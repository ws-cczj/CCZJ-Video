package files

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestFailedOrCancelledPublishKeepsPreviousFileAndRemovesOwnedTemporary(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		dir := t.TempDir()
		path := filepath.Join(dir, "backup.br")
		if err := os.WriteFile(path, []byte("previous complete backup"), 0600); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		failure := errors.New("writer failed")
		err := WriteAtomic(ctx, path, func(writer io.Writer) error {
			if _, err := writer.Write([]byte("incomplete")); err != nil {
				return err
			}
			if cancelled {
				cancel()
				return nil
			}
			return failure
		})
		cancel()
		if cancelled {
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		} else if !errors.Is(err, failure) {
			t.Fatal(err)
		}
		bytes, err := os.ReadFile(path)
		if err != nil || string(bytes) != "previous complete backup" {
			t.Fatal("previous file was lost", err)
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 1 {
			t.Fatal("owned temporary file remains", entries, err)
		}
	}
}
