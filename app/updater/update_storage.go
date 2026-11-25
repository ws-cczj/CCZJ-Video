package updater

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/storagepaths"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
)

func updateDirectory() (string, error) {
	switch runtime.GOOS {
	case "android":
		if directory, ok := mobileUpdateDirectory.Load().(func() string); ok {
			if dir := directory(); filepath.IsAbs(dir) {
				return dir, nil
			}
		}
		return "", fmt.Errorf("Android update storage is unavailable")
	case "ios":
		return "", fmt.Errorf("iOS updates are handled outside the app")
	}
	exe, err := runningExePath()
	if err != nil {
		return "", err
	}
	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return storagepaths.DesktopUpdates(cache, exe)
}

const privateRollbackName = "previous.exe"

// A sibling copy permits a same-volume replacement when LocalAppData and the
// executable are on different drives.
func copyUpdateFile(source, dir, pattern string) (string, error) {
	input, err := os.Open(source)
	if err != nil {
		return "", err
	}
	defer input.Close()
	target, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(target, input)
	if copyErr == nil {
		copyErr = target.Sync()
	}
	if err := errors.Join(copyErr, target.Close()); err != nil {
		_ = os.Remove(target.Name())
		return "", err
	}
	return target.Name(), nil
}

// The versioned downloader uses a numeric CreateTemp suffix. Standalone
// release checksums and other loose files do not have this shape.
var orphanUpdateDigest = regexp.MustCompile(`(?i)^CCZJ-Video-Update\.\d+\.\d+\.\d+(?:-[a-z0-9_.-]+)?\.\d+\.[a-z0-9]{1,5}\.sha256$`)
var partialUpdateFile = regexp.MustCompile(`(?i)^CCZJ-Video-Update\.\d+\.\d+\.\d+(?:-[a-z0-9_.-]+)?\.\d+\.part$`)
var partialRollbackFile = regexp.MustCompile(`^previous\.exe(?:\.\d+)?\.part$`)
var partialReplacementSuffix = regexp.MustCompile(`^\d+\.part$`)

// CleanupWindowsUpdateFiles also handles the first launch after installing
// with an updater that stored its files next to the executable.
func CleanupWindowsUpdateFiles() {
	if runtime.GOOS != "windows" {
		return
	}
	exe, err := runningExePath()
	if err != nil {
		return
	}
	cleanUpdateFiles(filepath.Dir(exe), filepath.Base(exe), false)
	private, err := updateDirectory()
	if err != nil {
		return
	}
	cleanUpdateFiles(private, "", true)
	if err := archiveLegacyRollback(exe, private); err != nil {
		applog.Warn("[Updater] 迁移回滚副本失败: %v", err)
	}
}

func cleanUpdateFiles(dir, exeName string, private bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	stagePrefix := strings.ToLower(exeName + ".cczj-replace-")
	for _, entry := range entries {
		if !entry.Type().IsRegular() {
			continue
		}
		name := entry.Name()
		path := filepath.Join(dir, name)
		if orphanUpdateDigest.MatchString(name) {
			packagePath := strings.TrimSuffix(path, filepath.Ext(path))
			if _, err := os.Lstat(packagePath); errors.Is(err, os.ErrNotExist) {
				_ = os.Remove(path)
			}
			continue
		}
		stageSuffix, stagedReplacement := strings.CutPrefix(strings.ToLower(name), stagePrefix)
		if partialUpdateFile.MatchString(name) || (private && partialRollbackFile.MatchString(name)) ||
			(exeName != "" && stagedReplacement && partialReplacementSuffix.MatchString(stageSuffix)) {
			if info, err := entry.Info(); err == nil && time.Since(info.ModTime()) > 24*time.Hour {
				_ = os.Remove(path)
			}
		}
	}
}

func archiveLegacyRollback(exe, private string) error {
	legacy := exe + swapBackupSuffix
	info, err := os.Lstat(legacy)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return err
	}
	if err := os.MkdirAll(private, 0700); err != nil {
		return err
	}
	staged, err := copyUpdateFile(legacy, private, privateRollbackName+".*.part")
	if err != nil {
		return err
	}
	defer os.Remove(staged)
	if err := os.Rename(staged, filepath.Join(private, privateRollbackName)); err != nil {
		return err
	}
	return os.Remove(legacy)
}
