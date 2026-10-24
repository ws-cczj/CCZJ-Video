package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	cacheservice "cczjVideo/app/cache"
	"cczjVideo/app/cast"
	collectionservice "cczjVideo/app/collection"
	"cczjVideo/app/danmaku"
	"cczjVideo/app/db"
	"cczjVideo/app/detail"
	"cczjVideo/app/diagnosticrecording"
	"cczjVideo/app/douban"
	downloadservice "cczjVideo/app/download"
	downloadengine "cczjVideo/app/download/engine"
	sourceservice "cczjVideo/app/source"

	"cczjVideo/app/handoff"
	"cczjVideo/app/library"
	"cczjVideo/app/lifecycle"
	mediaservice "cczjVideo/app/media"
	"cczjVideo/app/netbudget"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/netstats"
	"cczjVideo/app/operation"
	"cczjVideo/app/outbound"
	pluginservice "cczjVideo/app/plugin"
	proxyservice "cczjVideo/app/proxy"
	"cczjVideo/app/settings"
	"cczjVideo/app/storagepaths"
	updateservice "cczjVideo/app/update"
	"cczjVideo/app/updater"
	windowservice "cczjVideo/app/window"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

// App owns application dependencies, lifecycle, and the Wails-facing adapters.
type App struct {
	danmaku         *danmaku.Service
	danmakuHTTP     *http.Client
	modeMu          sync.RWMutex
	modeSwitchMu    sync.RWMutex
	mode            string
	modeTasks       *lifecycle.Group
	app             *application.App
	settings        *settings.Service
	plugins         *pluginservice.Service
	window          *windowservice.Service
	update          *updateservice.Service
	cache           *cacheservice.Service
	media           *mediaservice.Service
	details         *detail.Service
	releaseDetails  func()
	sources         *sourceservice.Service
	collection      *collectionservice.Service
	schedulers      *collectionservice.SchedulerService
	background      *lifecycle.Group
	networkBudget   *netbudget.Budget
	requestPolicy   *netpolicy.Manager
	outboundPool    *outbound.Pool
	operations      *operation.Metrics
	settingsWriteMu sync.Mutex
	stopComments    func(context.Context) error
	recording       *diagnosticrecording.Recorder
	// shutdownDone 在 ServiceShutdown 收尾时关闭。RestartApp 用它判断优雅退出
	// 是否真的走完，只有没走完才允许自己 os.Exit。
	shutdownDone chan struct{}
	shutdownOnce sync.Once
	shutdownMu   sync.Mutex
	databaseOpen bool
	logOpen      bool
	// relaunching 标记"这次退出其实是一次重启"。RestartApp 在请求退出之前已经把
	// 同一个 exe 又拉起一份，新进程立刻重新锁住文件——这时候兑现「退出时安装」
	// 换不动，还会留下一条用户没取消过的 swap_failed。关停流程读它。
	relaunching  atomic.Bool
	downloads    *downloadengine.Engine
	readingState *db.ReadingStateStore
	downloadDir  *downloadservice.Directory
	proxy        *proxyservice.Service
	hlsProxy     *proxyservice.HLSService
	mobileMedia  *proxyservice.MediaBridge
	// readingImages 服务小说/漫画的图片（漫画页、插图）。与 proxy 那个海报代理
	// 分开的原因写在 NewApp 里：节流策略、缓存目录与容量上限都不一样。
	readingImages *proxyservice.ImageProxy
	// library 是小说/漫画模式的资源载入服务。
	library *library.Service
	cast    *cast.Service
}

// ======================== 关闭行为 ========================

// GetCloseBehavior 返回关闭行为：true=缩小到托盘，false=直接退出
func (a *App) GetCloseBehavior() bool {
	return a.window.CloseBehavior()
}

// SetCloseBehavior 设置关闭行为并持久化
func (a *App) SetCloseBehavior(ctx context.Context, minimize bool) error {
	return a.window.SetCloseBehavior(ctx, minimize)
}

// RestartApp 重启应用：先把新进程拉起来，再请求退出。
//
// 这里以前是 sleep(200ms) + os.Exit(0)：整个关停流程被跳过 —— 记录退出时刻
// （下次启动的补采窗口按它算）、取消并等待采集与下载收尾、关日志、关库一个都没执行。
// 留下的结果就是"重启一次，库就脏一点"。现在只请求退出，进程由 app.Run() 正常返回
// 来结束；硬超时兜底只在优雅退出真的卡死时才对进程动手。
func (a *App) RestartApp() error {
	if application.System.IsMobile() {
		return apperror.New(apperror.Unsupported, "当前平台不支持桌面进程重启")
	}
	if a.app == nil {
		return apperror.New(apperror.Unavailable, "应用尚未启动")
	}
	exe, err := os.Executable()
	if err != nil {
		return apperror.Wrap(apperror.Unavailable, err, "无法启动新的应用进程")
	}

	attr := &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
	}
	if goruntime.GOOS == "windows" {
		attr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}
	}

	// 带上自己的 PID：新进程会等这个 PID 消失再抢单实例锁，否则它一启动就被
	// 判成第二实例并静默退出。换 exe 的批处理脚本带的是同一个参数（见 app/handoff）。
	newArgs := []string{exe, handoff.Arg(os.Getpid())}
	if err := launchReplacement(exe, newArgs, attr); err != nil {
		return apperror.Wrap(apperror.Unavailable, err, "无法启动新的应用进程")
	}

	// 重启把同一个 exe 又拉起一份，新进程立刻重新锁住那个文件。这次退出不是
	// 「用户关掉应用」，所以「退出时安装」的安排留给下一次真正的退出兑现。
	a.relaunching.Store(true)

	a.quitGracefully()
	return nil
}

// launchReplacement returns only after a replacement has actually started.
// The handoff PID lets the child wait for this process to finish shutting down.
func launchReplacement(exe string, args []string, attr *os.ProcAttr) error {
	process, firstErr := os.StartProcess(exe, args, attr)
	if firstErr == nil {
		if err := process.Release(); err != nil {
			applog.Warn("release replacement process handle: %v", err)
		}
		return nil
	}
	cmd := exec.Command(exe, args[1:]...)
	if err := cmd.Start(); err != nil {
		return errors.Join(firstErr, err)
	}
	if err := cmd.Process.Release(); err != nil {
		applog.Warn("release replacement process handle: %v", err)
	}
	return nil
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
	updater.SetMobileUpdateDirectory(func() string { return filepath.Join(a.getDataDir(), "updates") })
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
	a.background = lifecycle.NewGroup()
	a.details = detail.NewWithContext(a.background.Context(), 90*time.Second)
	a.media = mediaservice.NewService(db.GetVideoByIdContext, db.NewMediaStore(db.DB), a.details)
	a.operations = &operation.Metrics{}
	a.networkBudget = netbudget.New(a.background.Context(), netbudget.DefaultPolicy())
	a.update = updateservice.NewService(a.settings, func(name string, data any) {
		if a.app != nil {
			a.app.Event.Emit(name, data)
		}
	}, updater.NewDownloader(a.networkBudget))
	jar, _ := cookiejar.New(nil)
	a.danmakuHTTP = &http.Client{Jar: jar, Transport: a.networkBudget.Wrap("danmaku", netpolicy.WrapTransport(string(netstats.CategoryDanmaku), netstats.WrapTransport(netstats.CategoryDanmaku, &http.Transport{DialContext: proxyservice.SafeDialContext, MaxIdleConns: 4, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second}))), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 3 || !danmaku.AllowedURL(req.URL) {
			return apperror.New(apperror.PermissionDenied, "弹幕接口重定向不可用")
		}
		return nil
	}}
	a.danmaku = danmaku.NewService(danmaku.NewStore(func() *sql.DB {
		if db.DB() == nil {
			return nil
		}
		return db.DB().DB
	}), danmaku.NewClient(a.danmakuHTTP), a.background.Context())

	a.cast = cast.New(a.background.Context())
	a.recording = diagnosticrecording.New(func() string { return filepath.Join(a.getDataDir(), "exports", "diagnostics") }, a.background)
	a.collection = collectionservice.NewService(func(name string, data any) {
		if a.app != nil {
			a.app.Event.Emit(name, data)
		}
	}, func(name string, fn func(context.Context)) bool { return a.modeWork().Go(name, fn) }, a.background.Context())
	a.collection.SetContextProvider(func() context.Context {
		if a.currentMode() != "video" {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}
		return a.modeWork().Context()
	})
	a.sources = sourceservice.NewServiceWithRuntime(a.collection, func(name string, data any) {
		if a.app != nil {
			a.app.Event.Emit(name, data)
		}
	})

	// 这个间隔只作用于豆瓣补全调度（采集调度器另有生命周期管理）。
	// 一批 2+2 条在 60~150s 随机间隔下要约 7 分钟跑完：10 分钟一轮等于全天
	// 七成时间都在抓着豆瓣不放，容易被判定为异常流量；放宽到 30 分钟后
	// 每轮之间留出 20 分钟空闲，队列靠手动「补全此条」仍可即时推进。
	a.schedulers = collectionservice.NewSchedulerService(30*time.Minute, a.collection)
	a.sources.SetChangeObserver(a.schedulers.SourceChanged)
	a.downloadDir = downloadservice.NewDirectoryWithDefault(func(key, value string) error { return db.SetSetting(key, value) }, func() string {
		if application.System.IsMobile() {
			return filepath.Join(a.getDataDir(), "downloads")
		}
		return ""
	})
	a.proxy = proxyservice.NewService(a.getDataDir)
	a.hlsProxy = proxyservice.NewHLSServiceWithBudget(a.networkBudget)
	// 阅读模式的图片代理与海报代理分开：海报那条有 300ms 全局节流（防豆瓣限流），
	// 一话几十页漫画会被它拖成十几秒。缓存目录、TTL 与容量上限也各自一份，
	// 因为淘汰是按 mtime 全局排序的，混在一个目录里几十张大图会把开屏要用的
	// 小海报全挤出去。见 app/proxy/imgproxy.go。
	a.readingImages = proxyservice.NewImageProxyWithRuntime(a.getDataDir, a.background.Context(), a.networkBudget)
	a.readingImages.SetHeaderResolver(readingSourceHeaders)
	a.readingImages.SetModeResolver(func(key string) string {
		source, err := db.GetSourceByKey(key)
		if err != nil {
			return ""
		}
		return string(source.GetKind())
	})
	a.mobileMedia = proxyservice.NewMediaBridgeWithImages(a.background.Context(), a.hlsProxy, a.readingImages)
	a.library = library.NewServiceWithRuntime(a.getDataDir, a.background.Context(), a.networkBudget)
	a.library.EnableBuiltinRouting()
	a.readingImages.ConfigureReadingRuntime(a.library.WrapReadingTransport, a.library.BeginReadingForeground)
	a.readingImages.SetEvidenceRecorder(a.library.RecordReadingImage)
	// 图片地址的换算注入到这里，library 包因此不需要知道 App 与代理实例的存在。
	a.library.SetImageURLBuilder(a.readingImages.URL)
	modeLauncher := func(name string, fn func(context.Context)) bool { return a.modeWork().Go(name, fn) }
	a.stopComments = douban.ConfigureCommentRuntime(a.background.Context(), modeLauncher)
	a.proxy.SetMaintenanceLauncher(modeLauncher)
	a.readingImages.SetMaintenanceLauncher(modeLauncher)
	a.library.SetMaintenanceLauncher(modeLauncher)
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

func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) (startupErr error) {
	committed := false
	defer func() {
		if !committed {
			startupErr = errors.Join(startupErr, a.rollbackStartup())
		}
	}()
	a.app = application.Get()

	dataDir := a.getDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return apperror.Wrap(apperror.Storage, err, "应用数据目录不可用")
	}
	if application.System.IsMobile() {
		if err := configureMobileTemporaryStorage(dataDir); err != nil {
			return apperror.Wrap(apperror.Storage, err, "应用临时存储不可用")
		}
	}

	// 初始化日志（data/applog 目录），按天滚动，超过保留天数自动清理
	logDir := filepath.Join(dataDir, "applog")
	if err := applog.Init(logDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to init applog: %v\n", err)
	} else {
		a.logOpen = true
	}
	applog.InfoFields("application starting", applog.Fields{"app_version": updater.EffectiveVersion(), "go_version": goruntime.Version(), "wails_version": wailsVersionFromBuildInfo(), "goos": goruntime.GOOS, "goarch": goruntime.GOARCH, "pid": os.Getpid()})

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
	legacyStep := migrateLegacyDatabase(legacyDataDir(), dataDir)
	if legacyStep == legacyBlocked {
		if _, err := os.Stat(filepath.Join(dataDir, "cczj_video.db")); err != nil {
			return apperror.Wrap(apperror.Storage, err, "旧数据库迁移未完成，已保留旧数据；请检查数据目录权限和磁盘空间后重试")
		}
	}

	if err := db.InitDB(dataDir); err != nil {
		return apperror.Wrap(apperror.Storage, err, "应用数据库无法打开")
	}
	a.databaseOpen = true
	a.releaseDetails = cacheservice.SetDetailService(a.details)
	rawPolicy, err := a.settings.Get(netpolicy.SettingsKey)
	if err != nil {
		return err
	}
	policySettings, err := netpolicy.DecodeSettings(rawPolicy)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "读取网络请求策略失败")
	}
	requestPolicy := netpolicy.New(netpolicy.NewStore(func() *sql.DB {
		if db.DB() == nil {
			return nil
		}
		return db.DB().DB
	}), a.background.Context(), policySettings)
	if err = requestPolicy.Initialize(ctx); err != nil {
		_ = requestPolicy.Close(ctx)
		return apperror.Wrap(apperror.Storage, err, "恢复网络请求冷却记录失败")
	}
	a.requestPolicy = requestPolicy
	netpolicy.Install(requestPolicy)
	if err := a.startOutboundPool(); err != nil {
		return err
	}

	a.readingState = db.NewReadingStateStore(db.DB())
	a.downloads = downloadengine.New(a.background.Context(), downloadengine.Options{
		DataDirectory: a.getDataDir, Directory: a.downloadDir,
		Store: db.NewDownloadTaskStore(db.DB()), Budget: a.networkBudget,
		Progress: func(status downloadengine.Status) {
			if a.app != nil {
				a.app.Event.Emit("download:progress", status)
			}
		},
	})
	if err := a.downloads.Restore(ctx); err != nil {
		applog.Error("恢复下载任务失败: %v", err)
	}

	// 迁移结论记在当前库里：份旧数据被搬走过一次，就不该再提示用户去合并它。
	if legacyStep == legacyMigrated {
		recordLegacyDataStatus(legacyStatusMigrated)
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

	// Reading modes do not start video collection, Douban polling or chart fetches.
	mode, _ := a.settings.Get(applicationModeKey)
	a.startMode(normalizeMode(mode))

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
		"schema_version":   schemaVersion,
		"mode":             a.currentMode(),
		"background_tasks": len(a.background.ActiveTasks()),
	})

	// 加载关闭行为设置：只有在数据库中明确存在 "0" 时才禁用；首次运行保持默认开启
	a.window.LoadCloseBehavior()

	// 应用窗口设置（尺寸、是否可调整大小）
	if err := a.window.ApplySettings(ctx); err != nil {
		return err
	}

	// 启动时自动检查更新（每天一次）
	if application.System.IsDesktop() {
		a.background.Go("startupCheckUpdate", func(taskCtx context.Context) {
			if taskCtx.Err() == nil {
				a.startupCheckUpdate(taskCtx)
			}
		})
	}

	committed = true
	return nil
}

// getDataDir 返回数据目录路径
func (a *App) getDataDir() string {
	if application.System.IsMobile() {
		if dir := application.Mobile.StoragePath(); dir != "" {
			return filepath.Join(dir, "CCZJ Video")
		}
	}
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return storagepaths.DesktopRoot(dir)
	}
	// Keep a deterministic fallback for unusual restricted environments.
	exe, err := os.Executable()
	if err == nil {
		return filepath.Join(filepath.Dir(exe), "data")
	}
	return filepath.Join(".", "data")
}

// legacyDataDir 返回 2.0.x 及更早版本的数据落点：<exe 所在目录>\data。2.1.0 起改读用户配置目录。
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
//
// 返回的结论决定界面接下来做什么：新落点已经有库时这里什么都不能动（猜错方向就会
// 覆盖掉用户这一侧的数据），那份旧库改由「旧数据找回」提示交给用户自己点。
func migrateLegacyDatabase(oldDir, newDir string) legacyMigration {
	if oldDir == "" || filepath.Clean(oldDir) == filepath.Clean(newDir) {
		return legacyNone
	}
	oldDB := filepath.Join(oldDir, "cczj_video.db")
	if _, err := os.Stat(oldDB); err != nil {
		return legacyNone // 没有旧库，正常的首次安装
	}
	newDB := filepath.Join(newDir, "cczj_video.db")
	if _, err := os.Stat(newDB); err == nil {
		// 两处都有库。这里必须什么都不做——猜错方向就会覆盖掉用户这一侧的数据。
		applog.Warn("[DataDir] 新旧两处都有数据库，保留新目录 %s，未改动 %s", newDir, oldDir)
		return legacyBlocked
	}
	if err := copyDatabaseFiles(oldDir, newDir); err != nil {
		applog.Error("[DataDir] 迁移旧数据库失败: %v（旧目录: %s）", err, oldDir)
		return legacyBlocked
	}
	applog.Info("[DataDir] 已迁移旧数据库: %s -> %s", oldDir, newDir)
	return legacyMigrated
}

// copyDatabaseFiles 连同 WAL/SHM 一起复制：只搬 .db 会把尚未 checkpoint 的已提交
// 事务留在旧目录里，用户拿到的就是一个少了最近一段的库。
func copyDatabaseFiles(fromDir, toDir string) error {
	// Stage the entire DB/WAL/SHM group before publishing any of it. A failed
	// WAL copy must not leave a main DB that blocks recovery on the next start.
	staging, err := os.MkdirTemp(toDir, ".legacy-db-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)
	var suffixes []string
	for _, suffix := range []string{".db", ".db-wal", ".db-shm"} {
		src := filepath.Join(fromDir, "cczj_video"+suffix)
		if err := copyFile(src, filepath.Join(staging, "cczj_video"+suffix)); err != nil {
			if suffix != ".db" && errors.Is(err, os.ErrNotExist) {
				continue // WAL/SHM 本来就可能不存在
			}
			return err
		}
		suffixes = append(suffixes, suffix)
	}
	// Publish the main DB last: its presence is the startup migration gate.
	var published []string
	for i := len(suffixes) - 1; i >= 0; i-- {
		name := "cczj_video" + suffixes[i]
		destination := filepath.Join(toDir, name)
		if err := os.Rename(filepath.Join(staging, name), destination); err != nil {
			for _, path := range published {
				_ = os.Remove(path)
			}
			return err
		}
		published = append(published, destination)
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

// VideoDownloadReq preserves the public binding shape while the download
// engine owns task admission, transfer state and file publication.
type VideoDownloadReq = downloadengine.Request
type VideoDownloadStatus = downloadengine.Status

// StartVideoDownload 启动一个下载任务（非阻塞）
// ======================== Migrate ========================

// ======================== 版本更新 ========================

// GetAppVersion 返回当前应用版本号
func (a *App) GetAppVersion() string {
	return a.update.Version()
}

// CheckUpdate 检查是否有新版本（每次调用都获取最新信息，无缓存）
func (a *App) CheckUpdate(ctx context.Context) (*updater.UpdateInfo, error) {
	return runRequest(a, ctx, "app.CheckUpdate", func(ctx context.Context) (*updater.UpdateInfo, error) { return a.update.CheckContext(ctx) })
}

// DownloadUpdate 下载更新包，通过事件向前端推送进度
func (a *App) DownloadUpdate(ctx context.Context, downloadURL string) (string, error) {
	return runHostRequest(a, ctx, "app.DownloadUpdate", func(ctx context.Context) (string, error) { return a.update.DownloadContext(ctx, downloadURL) })
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

// GetLastInstallReport 返回上一次热替换脚本的回执（连同此刻在跑的版本）。
// 换没换成这件事只有脚本自己知道，本进程拿不到它的退出码，所以界面只能从这里问。
func (a *App) GetLastInstallReport() updateservice.InstallReport {
	return a.update.LastInstallReport()
}

// GetSwapBackupInfo 报告此刻有没有一份能退回的上一版程序副本（exe 旁边的 .old）。
// 界面用它决定「退回上一版」这个按钮出不出现——没有副本时给一个按不动的按钮，
// 用户只会以为功能是坏的。
func (a *App) GetSwapBackupInfo() updateservice.RollbackInfo {
	return a.update.SwapBackupInfo()
}

// RollbackUpdate 用那份副本换回上一个程序，然后退出让脚本接管。
// 路径不接受前端传参：副本位置由当前 exe 推出来，这个入口不该能被指到别的文件上。
func (a *App) RollbackUpdate() error {
	return a.update.Rollback()
}

// ScheduleInstallOnExit 是这个包的第二档安装：只登记「下次退出时装它」，此刻不换 exe、
// 不关窗口。为什么分两档见 docs/adr/0010-install-timing-tiers.md。
func (a *App) ScheduleInstallOnExit(filePath string, version string) error {
	return a.update.ScheduleInstallOnExit(filePath, version)
}

// CancelInstallOnExit 撤掉那份安排。
func (a *App) CancelInstallOnExit() error {
	return a.update.CancelInstallOnExit()
}

// GetPendingInstall 报告当前有没有一份「退出时安装」的安排，界面据此决定按钮写什么。
func (a *App) GetPendingInstall() updateservice.PendingInstall {
	return a.update.PendingInstallInfo()
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
	a.shutdownMu.Lock()
	defer a.shutdownMu.Unlock()
	select {
	case <-a.shutdownDone:
		return nil
	default:
	}
	if systemTray != nil {
		systemTray.Destroy()
		systemTray = nil
	}
	if a.databaseOpen {
		collectionservice.TouchLastExit()
	}
	if err := a.releaseRuntime(); err != nil {
		applog.Error("应用关闭未完成: %v", err)
		return err
	}
	if a.update != nil {
		if err := a.update.InstallOnShutdown(a.relaunching.Load()); err != nil {
			applog.Warn("退出时安装未完成: %v", err)
		}
	}
	applog.Info("应用正常退出")
	if a.logOpen {
		applog.Default().Close()
		a.logOpen = false
	}
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
