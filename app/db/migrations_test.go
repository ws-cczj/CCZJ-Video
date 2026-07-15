package db

import (
	"testing"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

func TestRunSchemaMigrationsIsIdempotent(t *testing.T) {
	database, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE sources (source_key TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sources (source_key) VALUES ('source_1')`); err != nil {
		t.Fatal(err)
	}

	backups := 0
	backup := func() error {
		backups++
		return nil
	}
	if err := runSchemaMigrations(database, backup); err != nil {
		t.Fatal(err)
	}
	if err := runSchemaMigrations(database, backup); err != nil {
		t.Fatal(err)
	}
	if backups != 1 {
		t.Fatalf("backup count = %d, want 1", backups)
	}
	var count int
	if err := database.Get(&count, `SELECT COUNT(*) FROM schema_migrations`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("migration count = %d, want 1", count)
	}
}

func TestRunSchemaMigrationsRejectsUnsafeSourceKey(t *testing.T) {
	database, err := sqlx.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.Exec(`CREATE TABLE sources (source_key TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sources (source_key) VALUES ('unsafe-key')`); err != nil {
		t.Fatal(err)
	}
	if err := runSchemaMigrations(database, nil); err == nil {
		t.Fatal("expected unsafe source key migration to fail")
	}
}
