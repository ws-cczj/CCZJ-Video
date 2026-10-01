package apperror

import (
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

// 前端按「CODE: 消息」拆码，日志按整行找原因，所以渲染格式和 Cause 的可见性都是契约。
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
