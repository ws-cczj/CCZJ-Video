package window

import (
	"cczjVideo/app/applog"
	"sync"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

var (
	setDWMWindowAttribute = windows.NewLazySystemDLL("dwmapi.dll").NewProc("DwmSetWindowAttribute")
	cornerUser32          = windows.NewLazySystemDLL("user32.dll")
	cornerGDI32           = windows.NewLazySystemDLL("gdi32.dll")
	getCornerWindowRect   = cornerUser32.NewProc("GetWindowRect")
	getCornerDPI          = cornerUser32.NewProc("GetDpiForWindow")
	getCornerDC           = cornerUser32.NewProc("GetDC")
	releaseCornerDC       = cornerUser32.NewProc("ReleaseDC")
	getCornerDeviceCaps   = cornerGDI32.NewProc("GetDeviceCaps")
	setCornerRegion       = cornerUser32.NewProc("SetWindowRgn")
	createCornerRegion    = cornerGDI32.NewProc("CreateRoundRectRgn")
	deleteCornerRegion    = cornerGDI32.NewProc("DeleteObject")
)

// InstallRoundedCorners binds geometry updates to the main window's lifetime.
// DWM owns Windows 11 corners; older hosts use a DPI-scaled native region so
// backgrounds and fixed WebView overlays share the same outer boundary.
func InstallRoundedCorners(w *application.WebviewWindow) {
	var mu sync.Mutex
	var initialized, native, warned bool
	var last struct {
		width, height, dpi int
		square, valid      bool
	}
	apply := func(_ *application.WindowEvent) {
		handle := w.NativeWindow()
		if handle == nil {
			return
		}
		square := w.IsMaximised() || w.IsFullscreen()
		mu.Lock()
		defer mu.Unlock()
		if !initialized {
			initialized = true
			if setDWMWindowAttribute.Find() == nil {
				var rounded uint32 = 2
				result, _, _ := setDWMWindowAttribute.Call(uintptr(handle), 33, uintptr(unsafe.Pointer(&rounded)), unsafe.Sizeof(rounded))
				native = int32(result) >= 0
			}
		}
		if native {
			return
		}
		var rect struct{ left, top, right, bottom int32 }
		ok, _, _ := getCornerWindowRect.Call(uintptr(handle), uintptr(unsafe.Pointer(&rect)))
		if ok == 0 {
			return
		}
		width, height, dpi := int(rect.right-rect.left), int(rect.bottom-rect.top), 96
		if getCornerDPI.Find() == nil {
			if value, _, _ := getCornerDPI.Call(uintptr(handle)); value > 0 {
				dpi = int(value)
			}
		} else if dc, _, _ := getCornerDC.Call(uintptr(handle)); dc != 0 {
			// GetDpiForWindow is absent on legacy systems. The device context
			// supplies their system-DPI fallback and must be released immediately.
			value, _, _ := getCornerDeviceCaps.Call(dc, 88)
			releaseCornerDC.Call(uintptr(handle), dc)
			if value > 0 {
				dpi = int(value)
			}
		}
		if width <= 0 || height <= 0 || (last.valid && last.width == width && last.height == height && last.dpi == dpi && last.square == square) {
			return
		}
		region := uintptr(0)
		if !square {
			diameter := (16*dpi + 48) / 96
			region, _, _ = createCornerRegion.Call(0, 0, uintptr(width+1), uintptr(height+1), uintptr(diameter), uintptr(diameter))
			if region == 0 {
				return
			}
		}
		// Position messages from SetWindowRgn may return to this listener.
		// Identical geometry must not allocate another native region.
		last.width, last.height, last.dpi, last.square, last.valid = width, height, dpi, square, true
		result, _, err := setCornerRegion.Call(uintptr(handle), region, 1)
		if result == 0 {
			last.valid = false
			if region != 0 {
				deleteCornerRegion.Call(region)
			}
			if !warned {
				warned = true
				applog.Info("[Window] Native corner region unavailable: %v", err)
			}
		}
		// Windows owns successful regions and deletes them on replacement or
		// window destruction. Only failed transfers are released by this owner.
	}
	for _, event := range []events.WindowEventType{events.Common.WindowRuntimeReady, events.Common.WindowDidResize, events.Common.WindowDPIChanged, events.Common.WindowMaximise, events.Common.WindowUnMaximise, events.Common.WindowFullscreen, events.Common.WindowUnFullscreen, events.Common.WindowRestore} {
		w.OnWindowEvent(event, apply)
	}
}
