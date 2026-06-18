package main

import (
	"embed"

	"github.com/wailsapp/wails/v3/pkg/application"
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
		Name:             "main",
		Title:            "CCZJ Video",
		Width:            1280,
		Height:           800,
		MinWidth:         900,
		MinHeight:        600,
		Frameless:        true,
		BackgroundColour: application.RGBA{Red: 20, Green: 20, Blue: 40, Alpha: 255},
		Windows: application.WindowsWindow{
			Theme:                             application.Dark,
			ResizeDebounceMS:                  10,
			DisableFramelessWindowDecorations: false,
		},
	})

	// ========== 系统托盘 ==========
	systray := app.SystemTray.New()
	systray.SetIcon(appIcon) // 使用项目根目录的自定义 icon.png
	systray.SetTooltip("CCZJ Video")

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

	// 窗口关闭时，Wails 框架会自动调用 ServiceShutdown() 完成清理（停止调度器、关闭数据库等），
	// 无需在 WindowClosing 钩子中手动处理（之前在这里调用 app.Quit() 会导致递归，
	// 造成窗口关闭延迟）。

	err := app.Run()
	if err != nil {
		panic(err)
	}
}