// Package window contains desktop window state and persisted window settings.
package window

import (
	"strconv"
	"sync/atomic"

	"cczjVideo/app/settings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	minimumWidth  = 800
	minimumHeight = 500
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
func (s *Service) SetCloseBehavior(minimize bool) {
	s.minimizeToTray.Store(minimize)
	value := "0"
	if minimize {
		value = "1"
	}
	_ = s.settings.Set("close_to_tray", value)
}

// LoadCloseBehavior restores the persisted close behavior while preserving the default on first run.
func (s *Service) LoadCloseBehavior() {
	value, err := s.settings.Get("close_to_tray")
	if err != nil {
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
func (s *Service) SetResizable(resizable bool) {
	if w := s.currentWindow(); w != nil {
		w.SetResizable(resizable)
	}
	value := "1"
	if !resizable {
		value = "0"
	}
	_ = s.settings.Set("window_resizable", value)
}

// Resizable reports whether the active window can be resized.
func (s *Service) Resizable() bool {
	w := s.currentWindow()
	return w != nil && w.Resizable()
}

// SetSize clamps, applies, and persists the active window size.
func (s *Service) SetSize(width, height int) {
	if width < minimumWidth {
		width = minimumWidth
	}
	if height < minimumHeight {
		height = minimumHeight
	}
	if w := s.currentWindow(); w != nil {
		w.SetSize(width, height)
	}
	_ = s.settings.Set("window_width", strconv.Itoa(width))
	_ = s.settings.Set("window_height", strconv.Itoa(height))
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
func (s *Service) ApplySettings() {
	w := s.currentWindow()
	if w == nil {
		return
	}

	width, widthErr := s.settings.Get("window_width")
	height, heightErr := s.settings.Get("window_height")
	if widthErr == nil && heightErr == nil {
		parsedWidth, widthParseErr := strconv.Atoi(width)
		parsedHeight, heightParseErr := strconv.Atoi(height)
		if widthParseErr == nil && heightParseErr == nil && parsedWidth >= minimumWidth && parsedHeight >= minimumHeight {
			w.SetSize(parsedWidth, parsedHeight)
		}
	}

	if value, err := s.settings.Get("window_resizable"); err == nil {
		w.SetResizable(value != "0")
	}
}

func (s *Service) currentWindow() application.Window {
	if s.current == nil {
		return nil
	}
	return s.current()
}
