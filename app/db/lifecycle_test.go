package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func TestInitDBRetriesFailureAndReopensAfterClose(t *testing.T) {
	previous, previousDir := instance, dataDir
	instance, dataDir = nil, ""
	blocked := filepath.Join(t.TempDir(), "data")
	t.Cleanup(func() { Close(); instance, dataDir = previous, previousDir })
	if err := os.WriteFile(blocked, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := InitDB(blocked); err == nil || DB() != nil {
			t.Fatal("failed startup reported success or published a partial database")
		}
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(blocked); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(blocked); err != nil {
		t.Fatal(err)
	}
	if err := InitDB(t.TempDir()); err == nil {
		t.Fatal("active database silently switched directories")
	}
	Close()
	if DB() != nil {
		t.Fatal("closed default database remains published")
	}
	if err := InitDB(blocked); err != nil {
		t.Fatalf("reopen: %v", err)
	}
}

func TestOpenClosesConnectionWhenMigrationFails(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cczj_video.db")
	seed, err := sqlx.Connect("sqlite", sqliteDSN(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec("CREATE TABLE global_video (id INTEGER PRIMARY KEY)"); err != nil {
		_ = seed.Close()
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	opened, err := Open(dir)
	if err == nil || opened != nil {
		if opened != nil {
			_ = opened.Close()
		}
		t.Fatal("invalid schema published a connection")
	}
	// Windows will refuse this removal if failed Open still holds the database.
	if err := os.Remove(path); err != nil {
		t.Fatalf("failed initialization leaked its file handle: %v", err)
	}
}

func TestOpenDoesNotReplaceDefaultDatabase(t *testing.T) {
	previous, previousDir := DB(), DataDir()
	first, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = first.Close() })
	second, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if _, err := first.Exec("INSERT INTO settings (key, value) VALUES ('isolation', 'first')"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := second.Get(&count, "SELECT count(*) FROM settings WHERE key = 'isolation'"); err != nil || count != 0 {
		t.Fatalf("independent databases share state: %d, %v", count, err)
	}
	if DB() != previous || DataDir() != previousDir {
		t.Fatal("Open replaced the default connection")
	}
}
