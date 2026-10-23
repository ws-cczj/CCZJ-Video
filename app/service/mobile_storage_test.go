package service

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMobileTemporaryStorageIsWritableAndInApplicationDirectory(t *testing.T) {
	t.Setenv("TMPDIR", "")
	t.Setenv("SQLITE_TMPDIR", "")
	root := t.TempDir()
	if err := configureMobileTemporaryStorage(root); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "tmp")
	if os.Getenv("SQLITE_TMPDIR") != want || os.Getenv("TMPDIR") != want {
		t.Fatal("temporary storage escaped application directory")
	}
	file, err := os.CreateTemp(want, "spill-*")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
}
