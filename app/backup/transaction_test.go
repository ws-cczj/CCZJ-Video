package backup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/readingstate"
	"github.com/jmoiron/sqlx"
)

func TestBackupStorageFailureRollsBackEveryCategory(t *testing.T) {
	resetUserTables(t)
	if _, err := db.DB().Exec(`DELETE FROM reading_state;CREATE TRIGGER reject_backup_favorite BEFORE INSERT ON favorites WHEN NEW.vod_id='atomic-1' BEGIN SELECT RAISE(ABORT,'reject backup favorite');END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = db.DB().Exec(`DROP TRIGGER reject_backup_favorite;DELETE FROM reading_state`) })
	input := Payload{Kind: payloadKind, Version: payloadVersion,
		Settings:  []db.BackupSetting{{Key: "backup_atomic_marker", Value: "new"}},
		Sources:   []*model.Source{{SourceKey: "atomic_source", Name: "Atomic", ApiUrl: "https://example.invalid"}},
		Videos:    []db.BackupVideoMeta{{VodName: "Atomic backup title"}},
		Favorites: []db.BackupFavorite{{VodName: "Atomic backup title", SourceKey: "atomic_source", VodID: "atomic-1"}},
		Reading:   &db.BackupReading{Records: []db.BackupReadingRecord{{Kind: "novel", Record: readingstate.Record{Bucket: "preferences", Key: "default", Value: `{"size":22,"theme":"paper"}`}}}},
	}
	if _, err := NewService().Import(input, "failure injection"); apperror.CodeOf(err) != apperror.Storage {
		t.Errorf("backup storage failure was not returned: %v", err)
	}
	for _, table := range []string{"settings", "sources", "global_video", "favorites", "reading_state"} {
		var count int
		if err := db.DB().Get(&count, `SELECT COUNT(*) FROM `+table); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("failed backup partially committed %s: %d rows", table, count)
		}
	}
}

func TestBackupCancellationRollsBackEarlierWrites(t *testing.T) {
	resetUserTables(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := db.WriteBackup(ctx, db.DB(), func(writer *db.BackupWriter) error {
		if err := writer.SetSetting("cancelled_restore", "must disappear"); err != nil {
			return err
		}
		cancel()
		return writer.AddSource(&model.Source{SourceKey: "cancelled_restore", Name: "Cancelled", ApiUrl: "https://example.invalid"})
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("missing cancellation: %v", err)
	}
	var count int
	if err := db.DB().Get(&count, `SELECT COUNT(*) FROM settings WHERE key='cancelled_restore'`); err != nil || count != 0 {
		t.Fatalf("cancelled restore committed previous write: count=%d err=%v", count, err)
	}
}

func TestConcurrentArchiveCopiesHaveIndependentCleanup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.db")
	fixture, err := sqlx.Connect("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.Exec(`CREATE TABLE marker(value TEXT);INSERT INTO marker VALUES('original')`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.Close(); err != nil {
		t.Fatal(err)
	}
	first, closeFirst, err := readOnlyCopy(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeFirst)
	second, closeSecond, err := readOnlyCopy(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeSecond)
	var firstPath, secondPath string
	if err := first.Get(&firstPath, `SELECT file FROM pragma_database_list WHERE name='main'`); err != nil {
		t.Fatal(err)
	}
	if err := second.Get(&secondPath, `SELECT file FROM pragma_database_list WHERE name='main'`); err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(firstPath) == filepath.Dir(secondPath) {
		t.Fatal("copies share a cleanup directory")
	}
	marker := filepath.Join(filepath.Dir(secondPath), "still-owned")
	if err := os.WriteFile(marker, []byte("second restore"), 0600); err != nil {
		t.Fatal(err)
	}
	closeFirst()
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("closing first restore deleted second resources: %v", err)
	}
	var value string
	if err := second.Get(&value, `SELECT value FROM marker`); err != nil || value != "original" {
		t.Fatalf("second copy no longer usable: %q %v", value, err)
	}
}

func TestExportsOrderUsesModificationTime(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, exportDirName), 0700); err != nil {
		t.Fatal(err)
	}
	older, newer := "backup-z.br", "backup-a.br"
	for index, name := range []string{older, newer} {
		path := filepath.Join(dir, exportDirName, name)
		if err := os.WriteFile(path, []byte("backup"), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Date(2026, 10, 24, 12, 0, 0, index*100_000_000, time.UTC)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := NewService().Exports(dir)
	if err != nil || len(rows) != 2 || rows[0].Name != newer {
		t.Fatalf("random suffix overrode actual export order: %#v %v", rows, err)
	}
}
