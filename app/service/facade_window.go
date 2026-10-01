package service

// ======================== Window / Title Bar ========================
//
// 拖动逻辑由 Wails 自身的 CSS 自定义属性机制实现：
//   · 在 <header class="titlebar"> 上设置 `--wails-draggable: drag`
//   · 在按钮区域上设置 `--wails-draggable: no-drag`
// 所以这里不再需要 Win32 手动调用 SendMessageW。
// 保留以下两个方法给前端用作"切换最大化状态 / 读取最大化状态"的辅助接口。

func (a *App) WindowToggleMax() bool {
	return a.window.ToggleMax()
}

func (a *App) WindowIsMax() bool {
	return a.window.IsMax()
}

// WindowSetFullscreen 切换"系统级全屏"（覆盖任务栏，移除窗口边框）
//
//	enter=true  → 进入全屏
//	enter=false → 退出全屏
//
// Wails 的 WindowFullscreen 在 Windows 上会自动移除标题栏并覆盖任务栏。
func (a *App) WindowSetFullscreen(enter bool) {
	a.window.SetFullscreen(enter)
}

func (a *App) WindowIsFs() bool {
	return a.window.IsFullscreen()
}

// SetTitleBarTheme 切换标题栏主题（"dark" 或 "light"）
// Wails 在启动时已设置 CustomTheme；这里通过 ExecJS 让系统重新应用。
func (a *App) SetTitleBarTheme(theme string) error {
	_ = theme
	a.window.ReloadTitleBar()
	return nil
}

// WindowSetResizable 设置窗口是否可拖动调整大小
func (a *App) WindowSetResizable(resizable bool) {
	a.window.SetResizable(resizable)
}

// WindowGetResizable 返回窗口是否可调整大小
func (a *App) WindowGetResizable() bool {
	return a.window.Resizable()
}

// WindowSetSize 设置窗口尺寸
func (a *App) WindowSetSize(width, height int) {
	a.window.SetSize(width, height)
}

// WindowSizeResp 窗口尺寸响应
type WindowSizeResp struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowGetSize 返回当前窗口尺寸
func (a *App) WindowGetSize() *WindowSizeResp {
	size := a.window.Size()
	return &WindowSizeResp{Width: size.Width, Height: size.Height}
}

// ApplyWindowSettings 从数据库加载并应用窗口设置（启动时调用）
func (a *App) ApplyWindowSettings() {
	a.window.ApplySettings()
}
