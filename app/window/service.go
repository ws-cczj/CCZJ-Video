// Package window contains desktop window state and persisted window settings.
package window

import (
	"context"
	"strconv"
	"sync"
	"sync/atomic"

	"cczjVideo/app/apperror"
	"cczjVideo/app/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Minimum client area shared by native creation and persisted size restoration.
const (
	MinimumWidth  = 960
	MinimumHeight = 600
)

// CurrentWindow returns the active Wails window, or nil before startup.
type CurrentWindow func() application.Window

// Size is the serializable desktop window size.
type Size struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Service owns close behavior and persisted window settings.
type Service struct {
	current        CurrentWindow
	settings       *settings.Service
	minimizeToTray atomic.Bool
	writeMu        sync.Mutex
}

// NewService creates a window service using the supplied active-window provider.
func NewService(current CurrentWindow, settingService *settings.Service) *Service {
	s := &Service{
		current:  current,
		settings: settingService,
	}
	s.minimizeToTray.Store(true)
	return s
}

// CloseBehavior returns whether closing the main window minimizes it to the tray.
func (s *Service) CloseBehavior() bool {
	return s.minimizeToTray.Load()
}

// SetCloseBehavior changes and persists the main-window close behavior.
func (s *Service) SetCloseBehavior(ctx context.Context, minimize bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	value := "0"
	if minimize {
		value = "1"
	}
	if err := s.settings.SetMany(ctx, map[string]string{"close_to_tray": value}); err != nil {
		return apperror.Wrap(apperror.Storage, err, "保存窗口关闭行为失败")
	}
	s.minimizeToTray.Store(minimize)
	return nil
}

// LoadCloseBehavior restores the persisted close behavior while preserving the default on first run.
func (s *Service) LoadCloseBehavior() {
	value, err := s.settings.Get("close_to_tray")
	if err != nil || value == "" {
		// 缺键在库层返回的是 ("", nil)，只看 err 会把「没存过」当成「存了 false」，
		// 于是每个新装用户第一次点关闭就直接退出程序，而默认值本来是缩到托盘。
		return
	}
	s.minimizeToTray.Store(value == "1")
}

// ToggleMax toggles the active window's maximized state and returns the resulting state.
func (s *Service) ToggleMax() bool {
	w := s.currentWindow()
	if w == nil {
		return false
	}
	if w.IsMaximised() {
		w.UnMaximise()
		return false
	}
	w.Maximise()
	return true
}

// IsMax reports whether the active window is maximized.
func (s *Service) IsMax() bool {
	w := s.currentWindow()
	return w != nil && w.IsMaximised()
}

// SetFullscreen enters or exits system fullscreen for the active window.
func (s *Service) SetFullscreen(enter bool) {
	w := s.currentWindow()
	if w == nil {
		return
	}
	if enter {
		w.Fullscreen()
		return
	}
	w.UnFullscreen()
}

// IsFullscreen reports whether the active window is in system fullscreen.
func (s *Service) IsFullscreen() bool {
	w := s.currentWindow()
	return w != nil && w.IsFullscreen()
}

// ReloadTitleBar asks WebView to reload after a title-bar theme change.
func (s *Service) ReloadTitleBar() {
	w := s.currentWindow()
	if w != nil {
		w.ExecJS("location.reload()")
	}
}

// SetResizable changes and persists the active window's resizable state.
func (s *Service) SetResizable(ctx context.Context, resizable bool) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	value := "1"
	if !resizable {
		value = "0"
	}
	if err := s.settings.SetMany(ctx, map[string]string{"window_resizable": value}); err != nil {
		return apperror.Wrap(apperror.Storage, err, "保存窗口调整设置失败")
	}
	if w := s.currentWindow(); w != nil {
		w.SetResizable(resizable)
	}
	return nil
}

// Resizable reports whether the active window can be resized.
func (s *Service) Resizable() bool {
	w := s.currentWindow()
	return w != nil && w.Resizable()
}

// SetSize clamps, applies, and persists the active window size.
func (s *Service) SetSize(ctx context.Context, width, height int) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if width < MinimumWidth {
		width = MinimumWidth
	}
	if height < MinimumHeight {
		height = MinimumHeight
	}
	if err := s.settings.SetMany(ctx, map[string]string{"window_width": strconv.Itoa(width), "window_height": strconv.Itoa(height)}); err != nil {
		return apperror.Wrap(apperror.Storage, err, "保存窗口尺寸失败")
	}
	if w := s.currentWindow(); w != nil {
		w.SetSize(width, height)
	}
	return nil
}

// Size returns the active window size, or zero values before a window exists.
func (s *Service) Size() Size {
	w := s.currentWindow()
	if w == nil {
		return Size{}
	}
	width, height := w.Size()
	return Size{Width: width, Height: height}
}

// ApplySettings restores persisted size and resizable state at application startup.
func (s *Service) ApplySettings(ctx context.Context) error {
	w := s.currentWindow()
	if w == nil {
		return nil
	}

	width, widthErr := s.settings.Get("window_width")
	height, heightErr := s.settings.Get("window_height")
	if widthErr == nil && heightErr == nil {
		parsedWidth, widthParseErr := strconv.Atoi(width)
		parsedHeight, heightParseErr := strconv.Atoi(height)
		if widthParseErr == nil && heightParseErr == nil && parsedWidth > 0 && parsedHeight > 0 {
			if parsedWidth < MinimumWidth || parsedHeight < MinimumHeight {
				if err := s.SetSize(ctx, parsedWidth, parsedHeight); err != nil {
					return err
				}
			} else {
				w.SetSize(parsedWidth, parsedHeight)
			}
		}
	}

	if value, err := s.settings.Get("window_resizable"); err == nil {
		w.SetResizable(value != "0")
	}
	return nil
}

func (s *Service) currentWindow() application.Window {
	if s.current == nil {
		return nil
	}
	return s.current()
}
