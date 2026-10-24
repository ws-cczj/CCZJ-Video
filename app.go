package main

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/service"
	"cczjVideo/app/storagepaths"
	appwindow "cczjVideo/app/window"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed icon.png
var appIcon []byte

// buildApp 装配 Wails 应用：注册 service.App 作为唯一绑定入口，挂上同域 HLS
// 代理中间件、无边框主窗口和系统托盘。业务逻辑一律不在这一层。
func buildApp() (*application.App, error) {
	windowsOptions := application.WindowsOptions{}
	if runtime.GOOS == "windows" {
		configDir, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("resolve application storage: %w", err)
		}
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		profile, err := storagepaths.WindowsWebview(configDir, filepath.Base(exe))
		if err != nil {
			return nil, fmt.Errorf("open browser storage: %w", err)
		}
		windowsOptions.WebviewUserDataPath = profile
	}
	myApp := service.NewApp()

	// 单实例回调要在窗口创建前声明，Options 只能用闭包引用它。
	var mainWindow *application.WebviewWindow

	options := application.Options{
		Windows:      windowsOptions,
		MarshalError: apperror.Marshal,
		Name:         "CCZJ Video",
		// 强制单实例：桌面上重复图标只会把已在跑的窗口叫到前台。两份进程会同时写
		// 同一份 SQLite、同一个缓存目录与同一个豆瓣限速闸门，那些状态都是按进程算的。
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.cczj.video",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if mainWindow == nil {
					return
				}
				applog.Info("[SingleInstance] 检测到重复启动，已激活现有窗口")
				mainWindow.Show()
				mainWindow.UnMinimise()
				mainWindow.Focus()
			},
		},
		Services: []application.Service{
			// beta.30's service registration uses its own marshaler fallback;
			// configure it explicitly so every binding carries the public envelope.
			application.NewServiceWithOptions(myApp, application.ServiceOptions{MarshalError: apperror.Marshal}),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: service.AssetMiddleware(myApp, application.System.IsMobile()),
		},
	}
	if application.System.IsMobile() {
		options.SingleInstance = nil
	}
	app := application.New(options)

	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "CCZJ Video",
		Width:  1280,
		Height: 800,
		// Keep WebView content zoom independent from the monitor DPI. Without an
		// explicit value WebView2 can retain a 50% zoom after DPI transitions.
		Zoom:      1.0,
		MinWidth:  appwindow.MinimumWidth,
		MinHeight: appwindow.MinimumHeight,
		Frameless: true,
		// 扩展包靠「从资源管理器拖一个文件夹进来」安装。不开这个开关，Wails 的运行时
		// 会对所有外部文件拖放强制 dropEffect=none——鼠标变成一个禁止符号，什么都进不来。
		EnableFileDrop:   true,
		BackgroundColour: application.RGBA{Red: 20, Green: 20, Blue: 40, Alpha: 255},
		Windows: application.WindowsWindow{
			Theme:                             application.Dark,
			DisableFramelessWindowDecorations: false,
		},
	})
	// Android owns its Activity, screen geometry and lifecycle. Desktop-only
	// tray, DPI and drag/drop handlers must not control that native host.
	if application.System.IsMobile() {
		return app, nil
	}
	mainWindow.RegisterHook(events.Common.WindowClosing, func(event *application.WindowEvent) {
		if app.Context().Err() != nil {
			return
		}
		if myApp.GetCloseBehavior() {
			event.Cancel()
			mainWindow.Hide()
			return
		}
		// Closing the main window is application exit; explicit Quit bypasses
		// this user-close hook and runs service shutdown for active tasks.
		event.Cancel()
		app.Quit()
	})

	// 原生拖放交给应用的只有被拖条目的绝对路径（WebView2 的 File 对象读不出目录内容），
	// 所以这里只把路径转给界面：装哪个、失败怎么报由扩展包面板决定，落盘闸门在 app/plugin。
	mainWindow.OnWindowEvent(events.Common.WindowFilesDropped, func(event *application.WindowEvent) {
		paths := event.Context().DroppedFiles()
		if len(paths) == 0 {
			return
		}
		app.Event.Emit("plugin:filedrop", paths)
	})

	// WebView2 may retain a stale zoom/raster scale after a frameless window is
	// repeatedly minimised and restored. Let the native DPI resync finish, then
	// normalise browser zoom and force the frontend to lay out at the final size.
	var scaleResetMu sync.Mutex
	var scaleResetTimer *time.Timer
	var scaleResetGeneration uint64
	resetWebviewScale := func(_ *application.WindowEvent) {
		scaleResetMu.Lock()
		scaleResetGeneration++
		generation := scaleResetGeneration
		if scaleResetTimer != nil {
			scaleResetTimer.Stop()
		}
		scaleResetTimer = time.AfterFunc(80*time.Millisecond, func() {
			scaleResetMu.Lock()
			if generation != scaleResetGeneration {
				scaleResetMu.Unlock()
				return
			}
			scaleResetTimer = nil
			scaleResetMu.Unlock()
			// SetZoom(1) is intentional here: ZoomReset may restore WebView2's
			// stale per-monitor value instead of CSS's 100% content scale.
			mainWindow.SetZoom(1.0)
			mainWindow.ExecJS("window.dispatchEvent(new Event('resize'))")
		})
		scaleResetMu.Unlock()
	}
	mainWindow.OnWindowEvent(events.Common.WindowRuntimeReady, resetWebviewScale)
	mainWindow.OnWindowEvent(events.Common.WindowUnMinimise, resetWebviewScale)
	mainWindow.OnWindowEvent(events.Common.WindowMaximise, resetWebviewScale)
	mainWindow.OnWindowEvent(events.Common.WindowUnMaximise, resetWebviewScale)
	mainWindow.OnWindowEvent(events.Common.WindowDPIChanged, resetWebviewScale)

	systray := app.SystemTray.New()
	systray.SetIcon(appIcon)
	systray.SetTooltip("CCZJ Video")
	service.SetSystemTray(systray)

	trayMenu := app.Menu.New()
	trayMenu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
		mainWindow.Show()
		mainWindow.Focus()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
		app.Quit()
	})

	systray.AttachWindow(mainWindow).WindowOffset(5).SetMenu(trayMenu)
	systray.OnClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})
	systray.OnDoubleClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})

	return app, nil
}
