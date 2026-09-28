package service

import (
	"cczjVideo/app/applog"
	cacheservice "cczjVideo/app/cache"
	collectionservice "cczjVideo/app/collection"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	downloadservice "cczjVideo/app/download"
	"cczjVideo/app/handler"
	"cczjVideo/app/lifecycle"
	mediaservice "cczjVideo/app/media"
	proxyservice "cczjVideo/app/proxy"
	"cczjVideo/app/settings"
	updateservice "cczjVideo/app/update"
	"cczjVideo/app/updater"
	"cczjVideo/app/util"
	windowservice "cczjVideo/app/window"
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/bwmarrin/snowflake"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	maxSourceImportBytes       = 64 << 20
	maxSourceImportBase64Bytes = 86 << 20
)

// 图片代理限速：避免对同一CDN连续快速请求导致被限流
type App struct {
	app         *application.App
	collectMu   sync.Mutex
	forceQuit   atomic.Bool
	settings    *settings.Service
	window      *windowservice.Service
	update      *updateservice.Service
	cache       *cacheservice.Service
	media       *mediaservice.Service
	collection  *collectionservice.Service
	schedulers  *collectionservice.SchedulerService
	background  *lifecycle.Group
	downloads   *downloadservice.Registry[downloadTask]
	downloadDir *downloadservice.Directory
	proxy       *proxyservice.Service
	hlsProxy    *proxyservice.HLSService
}

// ======================== 关闭行为 ========================

func (a *App) shouldMinimizeToTray() bool {
	return a.window.CloseBehavior()
}

// GetCloseBehavior 返回关闭行为：true=缩小到托盘，false=直接退出
func (a *App) GetCloseBehavior() bool {
	return a.window.CloseBehavior()
}

// SetCloseBehavior 设置关闭行为并持久化
func (a *App) SetCloseBehavior(minimize bool) {
	a.window.SetCloseBehavior(minimize)
}

// RestartApp 重启应用
func (a *App) RestartApp() {
	exe, err := os.Executable()
	if err != nil {
		return
	}

	attr := &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
	}
	if goruntime.GOOS == "windows" {
		attr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}
	}

	// 带上自己的 PID：新进程会等这个 PID 消失再抢单实例锁，否则它一启动就被
	// 判成第二实例并静默退出。
	newArgs := []string{exe, relaunchArg(os.Getpid())}
	_, err = os.StartProcess(exe, newArgs, attr)
	if err != nil {
		cmd := exec.Command(exe, relaunchArg(os.Getpid()))
		_ = cmd.Start()
	}

	a.forceQuit.Store(true)

	time.Sleep(200 * time.Millisecond)

	go func() {
		os.Exit(0)
	}()

	a.app.Quit()
}

func NewApp() *App {
	a := &App{}
	a.settings = settings.NewService()
	a.window = windowservice.NewService(func() application.Window {
		if a.app == nil {
			return nil
		}
		return a.app.Window.Current()
	}, a.settings)
	a.update = updateservice.NewService(a.settings, func(name string, data any) {
		if a.app != nil {
			a.app.Event.Emit(name, data)
		}
	})
	a.cache = cacheservice.NewService(a.getDataDir)
	// 失效层自己不依赖 Wails，事件出口在这里接上：Go 清完自己的缓存后，前端靠这条事件
	// 清详情/海报/TS 片段那几份（见 frontend/src/stores/cacheInvalidate.ts）。
	cacheservice.SetEventPublisher(func(name string, data any) {
		if a.app != nil {
			a.app.Event.Emit(name, data)
		}
	})
	a.media = mediaservice.NewService()
	a.background = lifecycle.NewGroup()
	a.collection = collectionservice.NewService(func(name string, data any) {
		if a.app != nil {
			a.app.Event.Emit(name, data)
		}
	}, a.background.Go)
	// 这个间隔只作用于豆瓣补全调度（采集调度器另有生命周期管理）。
	// 一批 2+2 条在 60~150s 随机间隔下要约 7 分钟跑完：10 分钟一轮等于全天
	// 七成时间都在抓着豆瓣不放，容易被判定为异常流量；放宽到 30 分钟后
	// 每轮之间留出 20 分钟空闲，队列靠手动「补全此条」仍可即时推进。
	a.schedulers = collectionservice.NewSchedulerService(30 * time.Minute)
	a.downloads = downloadservice.NewRegistry[downloadTask]()
	a.downloadDir = downloadservice.NewDirectory(func(key, value string) error { return db.SetSetting(key, value) })
	a.proxy = proxyservice.NewService()
	a.hlsProxy = proxyservice.NewHLSService()
	return a
}

// systemTray 由 main 包在装配窗口时写入。它是包级变量而不是 App 的方法：
// Wails 会把服务结构体上的每个导出方法都生成成前端可调用的绑定，托盘引用
// 不该暴露给 WebView。
var systemTray *application.SystemTray

// SetSystemTray 保存托盘图标引用，ServiceShutdown 负责在退出前摘掉它。
func SetSystemTray(tray *application.SystemTray) {
	systemTray = tray
}

// ServeHLSProxy handles the same-origin HLS endpoint registered in main.go.
func (a *App) ServeHLSProxy(w http.ResponseWriter, r *http.Request) {
	if a.hlsProxy == nil {
		http.Error(w, "HLS proxy unavailable", http.StatusServiceUnavailable)
		return
	}
	a.hlsProxy.ServeHTTP(w, r)
}

func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	a.app = application.Get()

	dataDir := a.getDataDir()
	os.MkdirAll(dataDir, 0755)

	// 初始化日志（data/applog 目录），按天滚动，超过保留天数自动清理
	logDir := filepath.Join(dataDir, "applog")
	if err := applog.Init(logDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to init applog: %v\n", err)
	}

	// 把 db 层的日志桥接到 applog（避免 db 直接依赖 applog 产生循环）。
	// 必须排在 InitDB 之前：InitDB 里的顺序迁移就往这里写「已应用迁移 vN」「补列 …」，
	// 晚一行等于把这些证据丢掉——升级是否原地演进、有没有退回过去的删库重建，
	// 只能靠真机日志里这几行来确认。
	db.SetLogger(func(level, msg string) {
		switch level {
		case "ERROR":
			applog.Error("%s", msg)
		case "WARN":
			applog.Warn("%s", msg)
		default:
			applog.Info("%s", msg)
		}
	})

	if err := db.InitDB(dataDir); err != nil {
		panic(fmt.Sprintf("Failed to init database: %v", err))
	}

	// 应用日志级别：log_level 为权威来源，旧的 debug_mode 作为兼容回退。
	// 设置页可随时改写，无需重启。
	level := applog.LevelInfo
	if saved, err := db.GetSetting("log_level"); err == nil && strings.TrimSpace(saved) != "" {
		if parsed, parseErr := parseLogLevel(saved); parseErr == nil {
			level = parsed
		}
	} else if debugMode, err := db.GetSetting("debug_mode"); err == nil && debugMode == "1" {
		level = applog.LevelDebug
	}
	applog.SetMinLevel(level)
	if level <= applog.LevelDebug {
		applog.Info("日志级别为 DEBUG，将输出详细日志")
	}

	util.InitSnowFlake()
	_ = snowflake.Epoch

	// 恢复诊断页可改的持久化设置（豆瓣轮询间隔、日志保留天数），再启动调度器。
	a.applyPersistedDiagnosticsSettings()

	// 启动采集和豆瓣调度器。
	a.background.Go("schedulers", func(taskCtx context.Context) {
		a.schedulers.Start(taskCtx)
	})

	// 源巡检：每 6 小时探一次所有启用源的列表首页，把结果记成健康度样本。
	// 没被排上定时采集的源也需要有人发现"接口已经下线"，否则用户要等到亲手点开
	// 那个源、看到空列表，才知道它已经废了很久。
	a.background.Go("sourcePatrol", a.runSourcePatrol)

	// 预加载豆瓣热榜缓存（异步，不阻塞启动）
	// 注意：豆瓣热榜需要通过数据源匹配播放地址，无任何源时跳过，避免无意义请求
	a.background.Go("preloadDoubanChart", func(taskCtx context.Context) {
		if taskCtx.Err() != nil {
			return
		}
		if sources, err := db.GetAllSources(); err == nil && len(sources) == 0 {
			applog.Info("当前无任何数据源，跳过豆瓣热榜预加载")
			return
		}
		_, err := douban.FetchDoubanChart()
		if err != nil {
			applog.Debug("预加载豆瓣热榜失败: %v", err)
		} else {
			applog.Info("豆瓣热榜预加载完成")
		}
	})

	// 同步全局类型表
	a.background.Go("syncGlobalTypes", func(taskCtx context.Context) {
		if taskCtx.Err() != nil {
			return
		}
		count, err := db.SyncGlobalTypesFromSources()
		if err != nil {
			applog.Warn("同步全局类型失败: %v", err)
		} else {
			applog.Info("同步全局类型完成: %d 条", count)
		}
	})

	schemaVersion, schemaErr := db.SchemaVersion()
	if schemaErr != nil {
		schemaVersion = 0
	}
	a.app.Event.Emit("app:ready", map[string]string{
		"data_dir": dataDir,
		// 以前这里是写死的 "reset-generation-5"——那个"升级即删库"的年代留下的。
		// 迁移改成顺序演进之后，报真实版本才有诊断价值。
		"schema_version": strconv.Itoa(schemaVersion),
	})
	applog.InfoFields("application ready", applog.Fields{
		"data_dir":         dataDir,
		"background_tasks": len(a.background.ActiveTasks()),
	})

	// 加载上次未完成的下载任务
	a.loadPersistedTasks()

	// 加载关闭行为设置：只有在数据库中明确存在 "0" 时才禁用；首次运行保持默认开启
	a.window.LoadCloseBehavior()

	// 应用窗口设置（尺寸、是否可调整大小）
	a.window.ApplySettings()

	// 启动时自动检查更新（每天一次）
	a.background.Go("startupCheckUpdate", func(taskCtx context.Context) {
		if taskCtx.Err() == nil {
			a.startupCheckUpdate(taskCtx)
		}
	})

	return nil
}

// getDataDir 返回数据目录路径
func (a *App) getDataDir() string {
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "CCZJ Video")
	}
	// Keep a deterministic fallback for unusual restricted environments.
	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "data")
	}
	return filepath.Join(".", "data")
}

// ======================== Video Download ========================

// VideoDownloadReq 前端发起的下载请求
type VideoDownloadReq struct {
	TaskId   string `json:"task_id"`
	Url      string `json:"url"`
	Filename string `json:"filename"` // 不含目录的文件名（含扩展名）
	SaveDir  string `json:"save_dir"` // 可选：自定义保存目录
	Force    bool   `json:"force"`    // 强制覆盖已有任务（用于重复下载确认后）
}

// VideoDownloadStatus 下载状态（用于 GetDownloadProgress 与事件推送）
type VideoDownloadStatus struct {
	TaskId     string          `json:"task_id"`
	Url        string          `json:"url"`
	Filename   string          `json:"filename"`
	SavePath   string          `json:"save_path"`
	Total      int64           `json:"total"`
	Downloaded int64           `json:"downloaded"`
	SpeedBps   float64         `json:"speed_bps"`
	EtaSec     float64         `json:"eta_sec"`
	Status     string          `json:"status"` // queued / downloading / done / error / cancelled
	Error      string          `json:"error,omitempty"`
	StartTime  int64           `json:"start_time"`
	EndTime    int64           `json:"end_time,omitempty"`
	Chunks     []ChunkProgress `json:"chunks,omitempty"` // 多连接并行下载的每个分块进度
}

// ChunkProgress 单个并发连接的分块进度
type ChunkProgress struct {
	ID    int   `json:"id"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"` // 本块已下载字节数
}

// downloadTask 内部状态
type downloadTask struct {
	mu         sync.Mutex
	status     VideoDownloadStatus
	cancel     context.CancelFunc
	httpClient *http.Client
	paused     bool     // 是否处于暂停状态
	segIndex   int      // 下一个要下载的分片索引（m3u8 断点恢复用）
	segments   []string // m3u8 的分片 URL 列表
	// playlist 是带密钥/初始化段/字节区间的分片列表，落盘顺序。segments 只剩 URL 的
	// 老任务读不出这些，所以两者并存：有 playlist 用它，否则退回 segments。
	playlist []downloadservice.Segment
	isM3u8   bool // 是否 m3u8 任务
	hasTotal bool // 是否已估算出 total（续传时不再重算）
}

// persistedTask 磁盘持久化格式
type persistedTask struct {
	TaskId     string   `json:"task_id"`
	Url        string   `json:"url"`
	Filename   string   `json:"filename"`
	SavePath   string   `json:"save_path"`
	Total      int64    `json:"total"`
	Downloaded int64    `json:"downloaded"`
	Status     string   `json:"status"`
	SegIndex   int      `json:"seg_index"`
	IsM3u8     bool     `json:"is_m3u8"`
	Segments   []string `json:"segments,omitempty"`
	// SegmentDetails 与 Segments 同序，但带 EXT-X-KEY / EXT-X-MAP / BYTERANGE。
	// 旧版本写不出这个字段，恢复时按纯 URL 处理。
	SegmentDetails []downloadservice.Segment `json:"segment_details,omitempty"`
	StartTime      int64                     `json:"start_time"`
	ErrorMsg       string                    `json:"error_msg,omitempty"`
	HasTotal       bool                      `json:"has_total"`
}

func sanitizeFilename(name string) string {
	if name == "" {
		name = "video"
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		`"`, "_", "<", "_", ">", "_", "|", "_", "\n", "_", "\r", "_", "\t", "_",
	)
	out := replacer.Replace(name)
	// 去除首尾空白与点（Windows 不允许以点结尾的目录）
	out = strings.TrimSpace(out)
	out = strings.Trim(out, ".")
	// Windows rejects paths longer than this in practice, so cap the byte length
	// while keeping multibyte titles intact.
	const maxFilenameBytes = 180
	if len(out) > maxFilenameBytes {
		ext := filepath.Ext(out)
		// A name like "a." + 200 chars yields an extension longer than the cap;
		// keeping it would push the base slice below zero.
		if len(ext) >= maxFilenameBytes/2 {
			ext = ""
		}
		out = truncateUTF8(strings.TrimSuffix(out, ext), maxFilenameBytes-len(ext)) + ext
		// Truncation can re-expose a trailing dot or space, both illegal on Windows.
		out = strings.TrimRight(out, " .")
	}
	if out == "" {
		out = "video"
	}
	return out
}

// truncateUTF8 cuts to maxBytes on a rune boundary so a Chinese title is never
// left with half a character.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}

// ensureFilenameExt 根据 URL 的扩展名补齐 filename（若 filename 没有扩展名）
func ensureFilenameExt(filename, urlStr string) string {
	if strings.Contains(filepath.Base(filename), ".") {
		// 如果扩展名是 .m3u8，替换为 .ts（分片合并后的容器）
		if strings.EqualFold(filepath.Ext(filename), ".m3u8") {
			return strings.TrimSuffix(filename, filepath.Ext(filename)) + ".ts"
		}
		return filename
	}
	// 去掉 query / fragment
	u := urlStr
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	base := filepath.Base(u)
	ext := filepath.Ext(base)
	if strings.EqualFold(ext, ".m3u8") {
		// m3u8 的分片拼接后仍是有效的 MPEG-TS，保存为 .ts 即可
		ext = ".ts"
	}
	if ext == "" || len(ext) > 8 {
		ext = ".mp4"
	}
	return filename + ext
}

// StartVideoDownload 启动一个下载任务（非阻塞）
// ======================== Migrate ========================

// ======================== 版本更新 ========================

// GetAppVersion 返回当前应用版本号
func (a *App) GetAppVersion() string {
	return a.update.Version()
}

// CheckUpdate 检查是否有新版本（每次调用都获取最新信息，无缓存）
func (a *App) CheckUpdate() (*updater.UpdateInfo, error) {
	return a.update.Check()
}

// DownloadUpdate 下载更新包，通过事件向前端推送进度
func (a *App) DownloadUpdate(downloadURL string) (string, error) {
	return a.update.Download(downloadURL)
}

// InstallUpdate 安装更新并退出当前程序
func (a *App) InstallUpdate(filePath string) error {
	return a.update.Install(filePath)
}

// IgnoreVersion 忽略指定版本（存入数据库，下次不再提示）
func (a *App) IgnoreVersion(version string) error {
	return a.update.IgnoreVersion(version)
}

// GetIgnoredVersion 返回被忽略的版本号
func (a *App) GetIgnoredVersion() string {
	return a.update.IgnoredVersion()
}

// GetLastStartVersion 返回上次启动时的版本号（参考 lx-music-desktop 的 getLastStartInfo）
// 用于检测版本升级，决定是否展示 changelog
func (a *App) GetLastStartVersion() string {
	return a.update.LastStartVersion()
}

// SaveLastStartVersion 保存当前版本号（启动时调用）
func (a *App) SaveLastStartVersion() {
	a.update.SaveCurrentVersion()
}

// GetPendingUpdateInfo 返回启动时检测到的待处理更新信息
func (a *App) GetPendingUpdateInfo() *updater.UpdateInfo {
	return a.update.Pending()
}

// ClearPendingUpdateInfo 清除待处理的更新信息
func (a *App) ClearPendingUpdateInfo() {
	a.update.ClearPending()
}

// FileExists 检查文件是否存在（用于前端检查已下载的更新包）
func (a *App) FileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

// startupCheckUpdate 启动时自动检查更新（每次启动都检查，无缓存）
func (a *App) startupCheckUpdate(ctx context.Context) {
	a.update.StartupCheck(ctx)
}

func (a *App) ServiceShutdown() error {
	// 移除系统托盘图标（如果存在）
	if systemTray != nil {
		systemTray.Destroy()
	}

	// 记录退出时间（下次启动"补采"可使用）
	handler.TouchLastExit()
	// 停止调度器的后台循环
	a.background.Stop(5 * time.Second)

	applog.Info("应用正常退出")
	// 关闭日志文件
	applog.Default().Close()
	db.Close()

	return nil
}

// IsSchedulerRunning 检查是否有调度任务正在运行（采集调度或豆瓣调度）
func (a *App) IsSchedulerRunning() bool {
	return a.schedulers.IsRunning()
}

// ConfirmShutdown 确认退出：触发应用退出，清理由 ServiceShutdown 统一处理
func (a *App) ConfirmShutdown() {
	if a.app != nil {
		a.app.Quit()
	}
}

// GracefulShutdown 优雅关闭：等待所有采集任务完成当前页后退出
func (a *App) GracefulShutdown() {
	a.schedulers.Stop()
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ======================== Cache Management ========================

// CacheInfo 应用自己写到磁盘上的占用，外加进程内派生缓存的条目数。
// 浏览器侧的 localStorage / IndexedDB 由前端统计，Go 报不出真实数字，
// 以前那两只恒为 0 的字段已经删掉。
type CacheInfo struct {
	DatabaseBytes int64  `json:"database_bytes"`
	DatabasePath  string `json:"database_path"`
	LogFileBytes  int64  `json:"log_file_bytes"`
	LogFilePath   string `json:"log_file_path"`

	DetailEntries int   `json:"detail_entries"`
	DetailBytes   int64 `json:"detail_bytes"`
	ChartMatches  int   `json:"chart_matches"`
	CommentPages  int   `json:"comment_pages"`
}

// GetCacheInfo 获取缓存信息
func (a *App) GetCacheInfo() (*CacheInfo, error) {
	info, err := a.cache.GetInfo()
	if err != nil {
		return nil, err
	}
	stats := cacheservice.Stats()
	return &CacheInfo{
		DatabaseBytes: info.DatabaseBytes,
		DatabasePath:  info.DatabasePath,
		LogFileBytes:  info.LogFileBytes,
		LogFilePath:   info.LogFilePath,

		DetailEntries: stats.DetailEntries,
		DetailBytes:   stats.DetailBytes,
		ChartMatches:  stats.ChartMatches,
		CommentPages:  stats.CommentPages,
	}, nil
}

// ClearCacheReq 清除缓存请求
type ClearCacheReq struct {
	Type string `json:"type"` // "memory" | "logs" | "database" | "all"
}

// ClearCache 清除指定类型的缓存。
//
// "memory" 是进程内的派生缓存（详情 / 热榜匹配 / 评论），走统一失效层；TS 片段那一份活在
// 浏览器 IndexedDB 里，由前端自己的清除按钮负责，这里没有对应目录。
func (a *App) ClearCache(req ClearCacheReq) (bool, error) {
	switch req.Type {
	case "memory":
		cacheservice.InvalidateAll("用户清除缓存")
	case "logs":
		// 走 logger 自己的清理：正在写的日志文件在 Windows 上删不掉。
		applog.Default().Clear()
	case "all":
		cacheservice.InvalidateAll("用户清除缓存")
		applog.Default().Clear()
	default:
		if err := a.cache.Clear(req.Type); err != nil {
			return false, err
		}
	}
	return true, nil
}

// ProxyImage proxies a remote image through the constrained proxy service.
func (a *App) ProxyImage(urlStr string) (string, error) {
	return a.proxy.ImageContext(a.background.Context(), urlStr)
}
