package cache

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLogCacheUsesApplicationLogDirectory(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "applog")
	if err := os.Mkdir(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "cczj-test.log"), []byte("log"), 0644); err != nil {
		t.Fatal(err)
	}
	service := NewService(func() string { return dir })
	info, err := service.GetInfo()
	if err != nil {
		t.Fatal(err)
	}
	if info.LogFilePath != logDir || info.LogFileBytes != 3 {
		t.Fatalf("log info = %+v", info)
	}
}
