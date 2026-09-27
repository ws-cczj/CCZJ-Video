package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func TestResetDatabaseForUpgradeArchivesUnsupportedDatabase(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "cczj_video.db")
	database, err := sqlx.Connect("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); INSERT INTO settings VALUES ('database_reset_version', 'old')`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	cacheDir := filepath.Join(dir, "ts_cache")
	if err := os.Mkdir(cacheDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "cached.ts"), []byte("cache"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := ResetDatabaseForUpgrade(dir, dbPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Fatalf("database was not removed: %v", err)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatalf("disk cache was not removed: %v", err)
	}
	archives, err := filepath.Glob(filepath.Join(dir, "reset-archives", "*.db"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives = %v, %v; want exactly one", archives, err)
	}

	database, err = sqlx.Connect("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL); INSERT INTO settings VALUES ('database_reset_version', ?)`, databaseResetVersion); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ResetDatabaseForUpgrade(dir, dbPath); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("current database was unexpectedly reset: %v", err)
	}
	archives, err = filepath.Glob(filepath.Join(dir, "reset-archives", "*.db"))
	if err != nil || len(archives) != 1 {
		t.Fatalf("archives after current database check = %v, %v; want unchanged", archives, err)
	}
}
