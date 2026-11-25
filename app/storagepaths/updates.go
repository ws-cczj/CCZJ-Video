package storagepaths

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

// DesktopUpdates scopes staging and rollback to one installation.
func DesktopUpdates(cacheDir, exe string) (string, error) {
	if !filepath.IsAbs(cacheDir) || !filepath.IsAbs(exe) {
		return "", fmt.Errorf("update storage requires absolute paths")
	}
	exe = filepath.Clean(exe)
	if runtime.GOOS == "windows" {
		exe = strings.ToLower(exe)
	}
	identity := sha256.Sum256([]byte(exe))
	return filepath.Join(DesktopRoot(cacheDir), "Updates", hex.EncodeToString(identity[:8])), nil
}

// AndroidUpdates matches the root verified by CczjUpdateInstaller before handoff.
func AndroidUpdates(filesDir string) string {
	if !filepath.IsAbs(filesDir) {
		return ""
	}
	return filepath.Join(DesktopRoot(filesDir), "updates")
}
