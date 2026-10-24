//go:build windows

package updater

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestDownloadWithLockedPreviousArtifact(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "耿", "OneDrive", "Desktop", "CC video")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	previous := filepath.Join(dir, stagingPrefix+".exe")
	oldBytes := []byte("previous running update")
	if err := os.WriteFile(previous, oldBytes, 0644); err != nil {
		t.Fatal(err)
	}
	path, err := windows.UTF16PtrFromString(previous)
	if err != nil {
		t.Fatal(err)
	}
	handle, err := windows.CreateFile(path, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(handle)
	if err := os.Remove(previous); err == nil {
		t.Fatal("fixture must hold a real Windows file lock")
	}
	payload := strings.Repeat("new verified update", 128)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, payload) }))
	defer server.Close()
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(payload)))
	got, err := downloadFromSources(context.Background(), dir, "https://github.com/ws-cczj/CCZJ-Video/releases/download/v9/app.exe", digest,
		[]speedTestResult{{url: server.URL, source: "fixture"}}, server.Client(), nil)
	if err != nil {
		t.Fatalf("locked old update must not block a new download: %v", err)
	}
	if got == previous {
		t.Fatal("download reused the occupied filename")
	}
	if err := verifyRecordedArtifact(got); err != nil {
		t.Fatal(err)
	}
	unchanged, err := os.ReadFile(previous)
	if err != nil || string(unchanged) != string(oldBytes) {
		t.Fatalf("previous artifact changed: %q, %v", unchanged, err)
	}
}
