package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// Configure before opening SQLite. Go's environment snapshot and modernc/libc's
// lazy environment are separate from Android's Java process environment.
func configureMobileTemporaryStorage(dataDir string) error {
	directory := filepath.Join(dataDir, "tmp")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return fmt.Errorf("create mobile temporary storage: %w", err)
	}
	for _, key := range []string{"TMPDIR", "SQLITE_TMPDIR"} {
		if err := os.Setenv(key, directory); err != nil {
			return fmt.Errorf("configure mobile temporary storage: %w", err)
		}
	}
	return nil
}
