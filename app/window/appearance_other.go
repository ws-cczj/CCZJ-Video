//go:build !windows

package window

import "github.com/wailsapp/wails/v3/pkg/application"

// InstallRoundedCorners leaves non-Windows window geometry to its native host.
func InstallRoundedCorners(_ *application.WebviewWindow) {}
