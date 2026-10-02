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
	pluginservice "cczjVideo/app/plugin"
	proxyservice "cczjVideo/app/proxy"
	"cczjVideo/app/settings"
	updateservice "cczjVideo/app/update"
	"cczjVideo/app/updater"
	windowservice "cczjVideo/app/window"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	maxSourceImportBytes       = 64 << 20
	maxSourceImportBase64Bytes = 86 << 20
	// shutdownDrainTimeout 给后台任务留出收尾时间：采集引擎在上下文取消后最多还跑
	// 完当前这一页的提交，下载则立刻断开。真正的判据是"db.Close 必须落在这里之后"，
	// 否则正在提交的那一批会写到已经关掉的句柄上。
	shutdownDrainTimeout = 15 * time.Second
	// shutdownHardStop 只在优雅退出确实卡死时对进程动手。远大于 drain 时限，
	// 正常重启与装更新走到的都是 shutdownDone 那条分支。
	shutdownHardStop = 60 * time.Second
)

// 图片代理限速：避免对同一CDN连续快速请求导致被限流
type App struct {
	app        *application.App
	settings   *settings.Service
	plugins    *pluginservice.Service
	window     *windowservice.Service
	update     *updateservice.Service
	cache      *cacheservice.Service
	media      *mediaservice.Service
	collection *collectionservice.Service
	schedulers *collectionservice.SchedulerService
	background *lifecycle.Group
	// shutdownDone 在 ServiceShutdown 收尾时关闭。RestartApp 用它判断优雅退出
	// 是否真的走完，只有没走完才允许自己 os.Exit。
	shutdownDone chan struct{}
	shutdownOnce sync.Once
	downloads    *downloadservice.Registry[downloadTask]
	downloadDir  *downloadservice.Directory
	proxy        *proxyservice.Service
	hlsProxy     *proxyservice.HLSService
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

// RestartApp 重启应用：先把新进程拉起来，再请求退出。
//
// 这里以前是 sleep(200ms) + os.Exit(0)：整个关停流程被跳过 —— 记录退出时刻
// （下次启动的补采窗口按它算）、取消并等待采集与下载收尾、关日志、关库一个都没执行。
// 留下的结果就是"重启一次，库就脏一点"。现在只请求退出，进程由 app.Run() 正常返回
// 来结束；硬超时兜底只在优雅退出真的卡死时才对进程动手。
func (a *App) RestartApp() {
	exe, err := os.Executable()
	if err != nil || a.app == nil {
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

	a.quitGracefully()
}

// QuitApp 退出应用：与 RestartApp 的差别只在于不拉起新进程。
// 首启条款闸门的「不同意就走」走这里 —— 那条路径不该顺带把应用再开一次。
func (a *App) QuitApp() {
	a.quitGracefully()
}

// quitGracefully 请求应用退出，并只在优雅退出真的走不通时对进程动手。
//
// 退出必须由 app.Run() 正常返回来收尾：ServiceShutdown 会取消并等待后台任务，
// 然后才关日志和关库。重启和用户点装的更新包都从这里退 —— 调用方要的是"一定会退"，
// 不是"立刻退"，所以留一个远大于收尾时限的兜底，防止界面卡在退出中。
func (a *App) quitGracefully() {
	if a.app == nil {
		applog.Warn("应用尚未启动，退出请求直接结束进程")
		os.Exit(0)
		return
	}

	quitting := a.shutdownDone
	a.app.Quit()
	go func() {
		select {
		case <-quitting:
		case <-time.After(shutdownHardStop):
			applog.Warn("优雅退出在 %s 内没有完成，强制结束进程", shutdownHardStop)
			os.Exit(0)
		}
	}()
}

func NewApp() *App {
	a := &App{shutdownDone: make(chan struct{})}
	a.settings = settings.NewService()
	// 扩展包注册表只要一个「取数据目录」的函数：它在扫描时才被调用，
	// 所以构造它可以早于数据库打开（启用状态才需要读库）。
	a.plugins = pluginservice.NewService(a.getDataDir)
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
	// 装更新时 updater 要让当前进程退场，替换脚本才好覆盖 exe。退出通道在这里递进去：
	// 走应用的关停流程（取消并等待采集与下载、关日志、关库），而不是 updater 自己
	// os.Exit —— 那样整个收尾会被跳过，丢的是正在提交的那一批数据。
	updater.SetQuitHook(a.quitGracefully)
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
	}, a.background.Go, a.background.Context())
	// 这个间隔只作用于豆瓣补全调度（采集调度器另有生命周期管理）。
	// 一批 2+2 条在 60~150s 随机间隔下要约 7 分钟跑完：10 分钟一轮等于全天
	// 七成时间都在抓着豆瓣不放，容易被判定为异常流量；放宽到 30 分钟后
	// 每轮之间留出 20 分钟空闲，队列靠手动「补全此条」仍可即时推进。
	a.schedulers = collectionservice.NewSchedulerService(30 * time.Minute)
	a.downloads = downloadservice.NewRegistry[downloadTask]()
	a.downloadDir = downloadservice.NewDirectory(func(key, value string) error { return db.SetSetting(key, value) })
	a.proxy = proxyservice.NewService(a.getDataDir)
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

	// 必须排在 InitDB 之前：库一打开就定了归属，晚一步就已经对着空库建表了。
	migrateLegacyDatabase(legacyDataDir(), dataDir)

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

	// 把内置扩展包落进 <dataDir>/plugins：设置页的「日志」「诊断」两个分组由它们提供，
	// 用户删掉那个文件夹就是不想要这一项，下次启动不会复活它。必须排在任何扫描之前，
	// 否则首次启动的扩展包面板看不到这两项。失败不影响启动——面板会照常列出磁盘上的包。
	if err := a.plugins.SeedBuiltin(); err != nil {
		applog.Warn("内置扩展包落盘失败: %v", err)
	}

	// 恢复诊断页可改的持久化设置（豆瓣轮询间隔、日志保留天数），再启动调度器。
	a.applyPersistedDiagnosticsSettings()

	// 启动采集和豆瓣调度器。
	a.background.Go("schedulers", func(taskCtx context.Context) {
		a.schedulers.Start(taskCtx)
	})

	// 源探测不再常驻后台：只有停在「采集源」页时界面才会去戳源站，
	// 每个源最多 5 分钟一次（见 service.sourceProbeMinInterval）。
	// 代价是没人打开那一页时不会自动发现接口下线，换来的是不挂着后台给源站添流量。

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

// legacyDataDir 返回 2.1.0 及更早版本的数据落点：<exe 所在目录>\data。
func legacyDataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

// migrateLegacyDatabase 在「旧落点有库、新落点没库」时把数据库复制过来。
//
// 为什么需要它：数据目录从 exe 旁边改到了用户配置目录，而更新是原地热替换 exe。
// 替换成功后，同一个图标启动起来读的是另一个空目录，用户看到的就是「更新完数据没了」。
// 只复制不删除：旧目录留着当免费备份，也方便用户自己回去核对。
func migrateLegacyDatabase(oldDir, newDir string) {
	if oldDir == "" || filepath.Clean(oldDir) == filepath.Clean(newDir) {
		return
	}
	oldDB := filepath.Join(oldDir, "cczj_video.db")
	if _, err := os.Stat(oldDB); err != nil {
		return // 没有旧库，正常的首次安装
	}
	newDB := filepath.Join(newDir, "cczj_video.db")
	if _, err := os.Stat(newDB); err == nil {
		// 两处都有库。这里必须什么都不做——猜错方向就会覆盖掉用户这一侧的数据。
		applog.Warn("[DataDir] 新旧两处都有数据库，保留新目录 %s，未改动 %s", newDir, oldDir)
		return
	}
	if err := copyDatabaseFiles(oldDir, newDir); err != nil {
		applog.Error("[DataDir] 迁移旧数据库失败: %v（旧目录: %s）", err, oldDir)
		return
	}
	applog.Info("[DataDir] 已迁移旧数据库: %s -> %s", oldDir, newDir)
}

// copyDatabaseFiles 连同 WAL/SHM 一起复制：只搬 .db 会把尚未 checkpoint 的已提交
// 事务留在旧目录里，用户拿到的就是一个少了最近一段的库。
func copyDatabaseFiles(fromDir, toDir string) error {
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		src := filepath.Join(fromDir, "cczj_video"+suffix)
		if err := copyFile(src, filepath.Join(toDir, "cczj_video"+suffix)); err != nil {
			if suffix != ".db" && errors.Is(err, os.ErrNotExist) {
				continue // WAL/SHM 本来就可能不存在
			}
			return err
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
	// 取消并等待后台任务收尾：采集引擎收到取消后最多还跑完当前页的提交，下载立刻断开。
	// db.Close 必须排在这个等待之后 —— 否则正在提交的那一批会写到已经关掉的句柄上，
	// 日志里只是一行 "database is closed"，实际丢掉的是整页采集结果。
	a.background.Stop(shutdownDrainTimeout)

	applog.Info("应用正常退出")
	// 库先关、日志最后关：关库时 WAL 归并可能失败并要报出来，日志要是先关了，
	// 丢掉的恰好是唯一那条「这次退出没收干净」的记录。
	db.Close()
	applog.Default().Close()
	a.shutdownOnce.Do(func() { close(a.shutdownDone) })

	return nil
}

// IsSchedulerRunning 检查是否有调度任务正在运行（采集调度或豆瓣调度）
func (a *App) IsSchedulerRunning() bool {
	return a.schedulers.IsRunning()
}

// ConfirmShutdown 确认退出：触发应用退出，清理由 ServiceShutdown 统一处理
func (a *App) ConfirmShutdown() {
	a.quitGracefully()
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

// ProxyImage proxies a remote image through the constrained proxy service.
func (a *App) ProxyImage(urlStr string) (string, error) {
	return a.proxy.ImageContext(a.background.Context(), urlStr)
}
