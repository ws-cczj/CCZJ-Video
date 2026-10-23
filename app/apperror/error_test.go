package apperror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestCodeOf(t *testing.T) {
	err := New(DownloadDuplicate, "already queued")
	if got := CodeOf(err); got != DownloadDuplicate {
		t.Fatalf("CodeOf() = %q, want %q", got, DownloadDuplicate)
	}
	if !Is(err, DownloadDuplicate) {
		t.Fatal("Is() 应认出同一个码")
	}
	if CodeOf(errors.New("plain")) != "" {
		t.Fatal("普通错误不该有码")
	}
	if !errors.Is(err, err) {
		t.Fatal("coded error must remain a normal Go error")
	}
}

// Diagnostic text remains compatible while the public response is structured.
func TestRenderingKeepsCodeAndCause(t *testing.T) {
	cause := fmt.Errorf("open %s: permission denied", "/x/y")

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"只有码和消息", New(Validation, "参数不对"), "VALIDATION: 参数不对"},
		{"带上底层原因", Wrap(Storage, cause, "写不下去"), "STORAGE: 写不下去: open /x/y: permission denied"},
		{"没消息就报原因", Wrap(Timeout, cause, ""), "TIMEOUT: open /x/y: permission denied"},
		{"没码也能渲染", &Error{Message: "裸消息"}, "裸消息"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Fatalf("Error() = %q, want %q", got, tt.want)
			}
		})
	}

	var appError *Error
	if !errors.As(Wrap(Storage, cause, "写不下去"), &appError) {
		t.Fatal("errors.As 应能取到带码错误")
	}
	if !errors.Is(Wrap(Storage, cause, "写不下去"), cause) {
		t.Fatal("Cause 必须能被 errors.Is 看到，否则底层错误类型在这套类型里断了")
	}
}

type providerError struct{}

func (providerError) Error() string { return "diagnostic source details" }
func (providerError) PublicError() Response {
	return Response{Code: AuthenticationRequired, Message: "请先登录"}
}

func TestPublicProtocol(t *testing.T) {
	cause := errors.New("open C:/private/file: permission denied")
	err := Wrap(Storage, cause, "保存失败")
	var response map[string]any
	if decodeErr := json.Unmarshal(Marshal(err), &response); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if response["code"] != "STORAGE" || response["message"] != "保存失败" || response["logged"] != true || response["requestId"] == "" {
		t.Fatalf("public error leaks diagnostics or loses classification: %#v", response)
	}
	if !errors.Is(err, cause) {
		t.Fatal("diagnostic cause lost")
	}
	if got := Public(fmt.Errorf("request: %w", providerError{})); got.Code != AuthenticationRequired || got.Message != "请先登录" {
		t.Fatalf("domain error: %#v", got)
	}
	if got := Public(cause); got.Code != Internal || got.Message == cause.Error() {
		t.Fatalf("unclassified error leaked: %#v", got)
	}
}

func TestClassificationSurvivesWrapping(t *testing.T) {
	cause := &Error{Code: RateLimited, Message: "请求过快", RetryAfterSeconds: 12}
	err := Wrap(Unavailable, cause, "搜索失败")
	if got := Public(err); got.Code != RateLimited || got.RetryAfterSeconds != 12 {
		t.Fatalf("metadata lost: %#v", got)
	}
	if !errors.Is(err, cause) {
		t.Fatal("cause lost")
	}
	if Wrap(Storage, nil, "write") != nil {
		t.Fatal("nil cause created an error")
	}
	var typedNil *Error
	if CodeOf(typedNil) != "" {
		t.Fatal("nil error has a code")
	}
	if got := Public(fmt.Errorf("request: %w", context.DeadlineExceeded)); got.Code != Timeout {
		t.Fatalf("deadline: %#v", got)
	}
}
