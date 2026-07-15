package files

import (
	"path/filepath"
	"testing"
)

func TestIsWithin(t *testing.T) {
	root := t.TempDir()
	if !IsWithin(filepath.Join(root, "nested", "file.txt"), root) {
		t.Fatal("expected nested path to be allowed")
	}
	if IsWithin(filepath.Join(root, "..", "outside.txt"), root) {
		t.Fatal("expected sibling path to be rejected")
	}
}
