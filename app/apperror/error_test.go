package apperror

import (
	"errors"
	"testing"
)

func TestCodeOf(t *testing.T) {
	err := New(DownloadDuplicate, "already queued")
	if got := CodeOf(err); got != DownloadDuplicate {
		t.Fatalf("CodeOf() = %q, want %q", got, DownloadDuplicate)
	}
	if !errors.Is(err, err) {
		t.Fatal("coded error must remain a normal Go error")
	}
}
