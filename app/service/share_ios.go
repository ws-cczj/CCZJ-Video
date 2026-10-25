//go:build ios

package service

import (
	"cczjVideo/app/apperror"
	fileservice "cczjVideo/app/files"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Validate the resolved file as well as the caller's path before passing it to
// UIKit. Presenting the share sheet does not claim the user saved a copy.
func shareIOSFile(path string, roots ...string) error {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "分享文件不可用")
	}
	if !fileservice.IsWithin(resolved, roots...) {
		return apperror.New(apperror.PermissionDenied, "只能分享应用管理的文件")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "分享文件不可用")
	}
	if !info.Mode().IsRegular() {
		return apperror.New(apperror.Unsupported, "请选择具体文件进行分享")
	}
	payload, err := json.Marshal(map[string]string{"url": (&url.URL{Scheme: "file", Path: resolved}).String()})
	if err != nil {
		return apperror.Wrap(apperror.Internal, err, "无法准备分享文件")
	}
	application.Mobile.Share(string(payload))
	return nil
}
