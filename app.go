package main

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/service"
	"embed"
	"net/http"
	"strings"
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
func buildApp() *application.App {
	myApp := service.NewApp()

	// 单实例回调要在窗口创建前声明，Options 只能用闭包引用它。
	var mainWindow *application.WebviewWindow

	app := application.New(application.Options{
		Name: "CCZJ Video",
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
			application.NewService(myApp),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
			Middleware: func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/__cczj/hls" {
						myApp.ServeHLSProxy(w, r)
						return
					}
					// Keep all application assets and Vite dev-server requests on Wails' default path.
					if strings.HasPrefix(r.URL.Path, "/__cczj/") {
						http.NotFound(w, r)
						return
					}
					next.ServeHTTP(w, r)
				})
			},
		},
	})

	mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:   "main",
		Title:  "CCZJ Video",
		Width:  1280,
		Height: 800,
		// Keep WebView content zoom independent from the monitor DPI. Without an
		// explicit value WebView2 can retain a 50% zoom after DPI transitions.
		Zoom:             1.0,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        true,
		BackgroundColour: application.RGBA{Red: 20, Green: 20, Blue: 40, Alpha: 255},
		Windows: application.WindowsWindow{
			Theme:                             application.Dark,
			DisableFramelessWindowDecorations: false,
		},
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

	return app
}
