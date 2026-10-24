package service

import (
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/apperror"
)

func TestReplacementLaunchReportsBothFailures(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-app.exe")
	err := launchReplacement(path, []string{path, "handoff"}, &os.ProcAttr{})
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok || len(joined.Unwrap()) != 2 {
		t.Fatalf("both failed starts must be returned: %v", err)
	}
}

func TestRestartWithoutRuntimeReturnsError(t *testing.T) {
	a := &App{}
	if err := a.RestartApp(); apperror.CodeOf(err) != apperror.Unavailable {
		t.Fatalf("unstarted application must remain running: %v", err)
	}
	if a.relaunching.Load() {
		t.Fatal("failed restart was published as a replacement")
	}
}
