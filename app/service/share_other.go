//go:build !ios

package service

import "cczjVideo/app/apperror"

func shareIOSFile(string, ...string) error {
	return apperror.New(apperror.Unsupported, "当前平台不使用 iOS 分享")
}
