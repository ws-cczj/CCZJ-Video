package proxy

import (
	"context"
	"errors"
	"net/url"
	"testing"

	"cczjVideo/app/apperror"
)

// 重试判据只看错误类型，不看文案：原来用 strings.Contains 认 "deadline"/"text/html"/"HTTP 4"，
// 谁改一句界面文案就会悄悄改掉重试策略。这里把三类该重试的和三类不该重试的都钉住。
func TestShouldRetryFetchClassifiesByType(t *testing.T) {
	timeout := &url.Error{Op: "Get", URL: "https://cdn.example/x.jpg", Err: context.DeadlineExceeded}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"客户端总超时", timeout, true},
		{"上下文到期", context.DeadlineExceeded, true},
		{"4xx", &statusError{statusCode: 404}, true},
		{"反爬 HTML 页", &notImageError{contentType: "text/html; charset=utf-8"}, true},
		{"5xx", &statusError{statusCode: 500}, false},
		{"非图内容", &notImageError{contentType: "application/json"}, false},
		{"文案里带 deadline 但不是超时", errors.New("dial: deadline format unsupported"), false},
		{"文案里带 HTTP 4 但不是状态码错误", errors.New("HTTP 4xx is fine here"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldRetryFetch(tt.err); got != tt.want {
				t.Fatalf("shouldRetryFetch(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

// 带码外层不能挡住判据：错误套上 apperror 之后仍然要认得出底层的类型。
func TestShouldRetryFetchSeesThroughCodedError(t *testing.T) {
	err := apperror.Wrap(apperror.Unavailable, &statusError{statusCode: 403}, "")
	if !shouldRetryFetch(err) {
		t.Fatalf("带码包装后重试判据失效: %v", err)
	}
	if apperror.CodeOf(err) != apperror.Unavailable {
		t.Fatalf("CodeOf() = %q, want %q", apperror.CodeOf(err), apperror.Unavailable)
	}
}
