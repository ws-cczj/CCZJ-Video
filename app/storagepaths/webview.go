// Package storagepaths owns application storage identities independently of release filenames.
package storagepaths

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func DesktopRoot(configDir string) string { return filepath.Join(configDir, "CCZJ Video") }

// Official desktop release names used before the WebView identity was pinned.
// Unrelated tools and diagnostics builds must not become a migration candidate.
var releaseName = regexp.MustCompile(`(?i)^cczj[- ]?video(?:-windows-(?:amd64|arm64))?(?:-v?\d+\.\d+\.\d+(?:[-.][a-z0-9]+)*)?\.exe$`)

// WindowsWebview pins one profile in the same configuration root as SQLite.
// Existing profiles are reused in place: copying a live Chromium/LevelDB tree
// is not a consistent snapshot, and moving it would strand older installations.
func WindowsWebview(configDir, exeName string) (string, error) {
	if !filepath.IsAbs(configDir) || filepath.Base(exeName) != exeName || exeName == "." {
		return "", fmt.Errorf("invalid WebView storage identity")
	}
	root := DesktopRoot(configDir)
	manifest := filepath.Join(root, "webview-profile.json")
	read := func() (string, error) {
		data, err := os.ReadFile(manifest)
		if err != nil {
			return "", err
		}
		var relative string
		if err := json.Unmarshal(data, &relative); err != nil {
			return "", fmt.Errorf("read WebView profile selection: %w", err)
		}
		if !filepath.IsLocal(relative) || strings.ContainsAny(relative, `:`) ||
			(relative != filepath.Join("CCZJ Video", "WebView2") &&
				(filepath.Base(relative) != relative || !strings.EqualFold(filepath.Ext(relative), ".exe"))) {
			return "", fmt.Errorf("invalid WebView profile selection")
		}
		path := filepath.Join(configDir, relative)
		info, err := os.Stat(path)
		if err != nil {
			return "", fmt.Errorf("selected WebView profile is unavailable: %w", err)
		}
		if !info.IsDir() {
			return "", fmt.Errorf("selected WebView profile is not a directory")
		}
		return path, nil
	}
	if selected, err := read(); err == nil || !errors.Is(err, os.ErrNotExist) {
		return selected, err
	} else if _, statErr := os.Stat(manifest); !errors.Is(statErr, os.ErrNotExist) {
		// A missing selected directory is not permission to silently start empty.
		return "", err
	}

	selected := filepath.Join(root, "WebView2")
	if _, err := os.Stat(selected); errors.Is(err, os.ErrNotExist) {
		entries, err := os.ReadDir(configDir)
		if err != nil {
			return "", err
		}
		var newest time.Time
		for _, entry := range entries {
			if !entry.IsDir() || !(strings.EqualFold(entry.Name(), exeName) || releaseName.MatchString(entry.Name())) {
				continue
			}
			candidate := filepath.Join(configDir, entry.Name())
			info, err := os.Stat(filepath.Join(candidate, "EBWebView", "Default", "Preferences"))
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if err != nil {
				return "", err
			}
			if info.IsDir() {
				continue
			}
			if info.ModTime().After(newest) {
				selected, newest = candidate, info.ModTime()
			}
		}
	} else if err != nil {
		return "", err
	}
	if err := os.MkdirAll(selected, 0700); err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return "", err
	}
	relative, err := filepath.Rel(configDir, selected)
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(relative)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(root, ".webview-profile-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, writeErr := tmp.Write(data)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if err := errors.Join(writeErr, syncErr, closeErr); err != nil {
		return "", err
	}
	// Publish a complete selection without overwriting another starting process.
	if err := os.Link(tmp.Name(), manifest); err != nil && !errors.Is(err, os.ErrExist) {
		return "", err
	}
	return read()
}
