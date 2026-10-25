package service

import (
	"cczjVideo/app/applog"
	fileservice "cczjVideo/app/files"
	"cczjVideo/app/operation"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

// GetDownloadDir 返回默认下载目录
func (a *App) GetDownloadDir() string {
	return a.downloadDir.Get()
}

// SetDownloadDir 设置自定义下载目录（空字符串则重置为默认）
func (a *App) SetDownloadDir(dir string) (string, error) {
	ctx := operation.WithFields(context.Background(), applog.Fields{"directory_sha256": applog.Fingerprint([]byte(dir)), "reset": strings.TrimSpace(dir) == ""})
	return runRequest(a, ctx, "download.SetDownloadDir", func(context.Context) (string, error) { return a.downloadDir.Set(dir) })
}

// OpenFileInExplorer 在系统文件管理器中打开该文件（定位到文件）
func (a *App) OpenFileInExplorer(path string) bool {
	if path == "" {
		return false
	}
	if !fileservice.IsWithin(path, a.getDataDir(), a.downloadDir.Get()) {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}
	if goruntime.GOOS == "ios" {
		return shareIOSFile(path, a.getDataDir(), a.downloadDir.Get()) == nil
	}

	// Windows: 使用 explorer /select, 定位到具体文件
	if goruntime.GOOS == "windows" {
		cmd := exec.Command("explorer", "/select,", path)
		if err := cmd.Start(); err == nil {
			return true
		}
		// 回退：打开目录
		dir := filepath.Dir(path)
		cmd = exec.Command("explorer", dir)
		if err := cmd.Start(); err == nil {
			return true
		}
		return false
	}

	// 其他平台：打开所在目录
	dir := filepath.Dir(path)
	cmd := exec.Command("xdg-open", dir)
	cmd.Start()
	return true
}
