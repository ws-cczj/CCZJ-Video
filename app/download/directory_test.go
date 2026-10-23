package download

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDirectorySetAndReset(t *testing.T) {
	temp := t.TempDir()
	var saved string
	directory := NewDirectory(func(_, value string) error {
		saved = value
		return nil
	})
	directory.homeDir = func() (string, error) { return temp, nil }

	configured := filepath.Join(temp, "custom")
	got, err := directory.Set(configured)
	if err != nil || got != configured || saved != configured {
		t.Fatalf("Set() = %q, %q, %v", got, saved, err)
	}
	if info, err := os.Stat(configured); err != nil || !info.IsDir() {
		t.Fatalf("expected created directory: %v", err)
	}
	got, err = directory.Set("")
	if err != nil || got != filepath.Join(temp, "Downloads") || saved != "" {
		t.Fatalf("reset = %q, %q, %v", got, saved, err)
	}
}

func TestDirectoryUsesPlatformStorageAfterReset(t *testing.T) {
	root := t.TempDir()
	writable := filepath.Join(root, "app-files", "downloads")
	directory := NewDirectoryWithDefault(nil, func() string { return writable })
	directory.homeDir = func() (string, error) { return filepath.Join(root, "unwritable-shared-storage"), nil }
	if got := directory.Get(); got != writable {
		t.Fatalf("platform default = %q, want %q", got, writable)
	}
	if _, err := directory.Set(filepath.Join(root, "custom")); err != nil {
		t.Fatal(err)
	}
	if got, err := directory.Set(""); err != nil || got != writable {
		t.Fatalf("reset = %q, %v", got, err)
	}
}
