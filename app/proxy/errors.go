package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// 取图失败的原因类型。重试只按这些类型判，不再 strings.Contains 猜文案 ——
// 文案是给界面看的，一改文案就悄悄改了重试策略，是这类判定最典型的坏法。
// 类型外面统一套 apperror 的码（见 service.go 各条返回路径），界面拿到的是稳定前缀。

// statusError 是一次非 2xx 的响应。
type statusError struct{ statusCode int }

func (e *statusError) Error() string { return fmt.Sprintf("HTTP %d", e.statusCode) }

// retryable 保留原有的重试口径：只重 4xx。5xx 在这条链路上通常是路径本身不对
// （图床对不存在的键回 500），重试只是让共用一把限速锁的其它海报多等一轮。
func (e *statusError) retryable() bool {
	return e.statusCode >= 400 && e.statusCode < 500
}

// notImageError 是「拿回来了但不是图」，最常见的样子是反爬挑战页（200 + text/html）。
type notImageError struct{ contentType string }

func (e *notImageError) Error() string { return fmt.Sprintf("not an image: %s", e.contentType) }

// retryable 只对 text/html 为真：挑战页可能是瞬时的；其它非图内容（JSON 错误体、
// 目录页）重试多少次都是同一份。
func (e *notImageError) retryable() bool {
	return strings.Contains(strings.ToLower(e.contentType), "text/html")
}

// fetchTimeout 判「是不是没时间回来」：客户端总超时以 *url.Error 的形状出现，
// 调用方的 ctx 到期则是 context.DeadlineExceeded。两条都是瞬时故障。
func fetchTimeout(err error) bool {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Timeout() {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded)
}

// shouldRetryFetch 汇总重试判据：超时、反爬 HTML 页、4xx。其余（私网被拦、URL 写错、
// 非图内容、体积超限）一次判死，免得拖住整条图片队列。
func shouldRetryFetch(err error) bool {
	if err == nil {
		return false
	}
	if fetchTimeout(err) {
		return true
	}
	var status *statusError
	if errors.As(err, &status) {
		return status.retryable()
	}
	var notImage *notImageError
	if errors.As(err, &notImage) {
		return notImage.retryable()
	}
	return false
}
