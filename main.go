package main

import (
	"embed"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed icon.png
var appIcon []byte

func main() {
	myApp := NewApp()

	app := application.New(application.Options{
		Name: "CCZJ Video",
		Services: []application.Service{
			application.NewService(myApp),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
	})

	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
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

	// ========== 系统托盘 ==========
	systray := app.SystemTray.New()
	systray.SetIcon(appIcon) // 使用项目根目录的自定义 icon.png
	systray.SetTooltip("CCZJ Video")
	myApp.systray = systray // 保存引用，以便在关闭时清理

	// 托盘菜单
	trayMenu := app.Menu.New()
	trayMenu.Add("显示主窗口").OnClick(func(ctx *application.Context) {
		mainWindow.Show()
		mainWindow.Focus()
	})
	trayMenu.AddSeparator()
	trayMenu.Add("退出").OnClick(func(ctx *application.Context) {
		app.Quit()
	})

	// 把窗口关联到托盘图标，并设置菜单（官方推荐方式）
	systray.AttachWindow(mainWindow).WindowOffset(5).SetMenu(trayMenu)

	// 左键单击：总是显示并聚焦窗口（不再切换隐藏）
	systray.OnClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})

	// 左键双击：确保窗口显示并获得焦点
	systray.OnDoubleClick(func() {
		mainWindow.Show()
		mainWindow.Focus()
	})

	err := app.Run()
	if err != nil {
		panic(err)
	}
}
