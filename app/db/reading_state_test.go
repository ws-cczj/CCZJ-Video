package db

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"cczjVideo/app/readingstate"
)

func TestReadingProgressWaitsForConcurrentCatalogWriter(t *testing.T) {
	conn, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewReadingStateStore(conn)
	ctx := context.Background()
	if _, err := store.Load(ctx, "novel", nil); err != nil {
		t.Fatal(err)
	}
	writer, err := conn.BeginTxx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Rollback()
	if _, err := writer.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('catalog-writing','busy')`); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- store.Apply(ctx, "novel", []readingstate.Change{{Record: readingFixture("novel", "concurrent")}})
	}()
	select {
	case err := <-done:
		t.Fatalf("progress must wait for the writer instead of failing immediately: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := writer.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	rows, err := store.Load(ctx, "novel", nil)
	if err != nil || len(rows) != 1 {
		t.Fatalf("progress was lost: %v %v", rows, err)
	}
}

func readingFixture(kind, id string) readingstate.Record {
	key := kind + "\x00source\x00" + id
	value := map[string]any{"itemId": 7, "title": "Offline title", "addedAt": 5, "kind": kind, "sourceKey": "source", "remoteId": id}
	if kind == "novel" {
		value["key"] = key
		value["item"] = map[string]any{"id": 7, "kind": kind, "sourceKey": "source", "remoteId": id, "title": "Offline title"}
	}
	raw, _ := json.Marshal(value)
	return readingstate.Record{Bucket: "shelf", Key: key, Value: string(raw)}
}

func TestReadingImportAtomicOnceAndOfflineRecordsSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	store := NewReadingStateStore(conn)
	ctx := context.Background()
	record := readingFixture("novel", "one")
	bad := record
	bad.Key = "comic\x00source\x00bad"
	if _, err := store.Load(ctx, "novel", []readingstate.Record{record, bad}); err == nil {
		t.Fatal("invalid import accepted")
	}
	for _, table := range []string{"reading_state", "reading_state_imports"} {
		var count int
		if err := conn.Get(&count, "SELECT COUNT(*) FROM "+table); err != nil || count != 0 {
			t.Fatalf("partial import: %s %d %v", table, count, err)
		}
	}
	got, err := store.Load(ctx, "novel", []readingstate.Record{record})
	if err != nil || len(got) != 1 {
		t.Fatalf("load = %+v %v", got, err)
	}
	if err := store.Apply(ctx, "novel", []readingstate.Change{{Record: record, Delete: true}}); err != nil {
		t.Fatal(err)
	}
	conn.Close()
	conn, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store = NewReadingStateStore(conn)
	got, err = store.Load(ctx, "novel", []readingstate.Record{record, bad})
	if err != nil || len(got) != 0 {
		t.Fatalf("old browser data resurrected deleted state: %+v %v", got, err)
	}
	comic := readingFixture("comic", "one")
	got, err = store.Load(ctx, "comic", []readingstate.Record{comic})
	if err != nil || len(got) != 1 {
		t.Fatalf("mode import leaked: %+v %v", got, err)
	}
}

func TestReadingApplyValidatesBatchBeforeCommitAndLeavesOtherBooks(t *testing.T) {
	conn, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewReadingStateStore(conn)
	ctx := context.Background()
	first, other := readingFixture("novel", "one"), readingFixture("novel", "two")
	if err := store.Apply(ctx, "novel", []readingstate.Change{{Record: first}}); err == nil {
		t.Fatal("write before hydration accepted")
	}
	if _, err := store.Load(ctx, "novel", []readingstate.Record{first, other}); err != nil {
		t.Fatal(err)
	}
	broken := readingstate.Record{Bucket: "progress", Key: first.Key, Value: `{"itemId":7,"chapterId":1,"chapterOrd":0,"ratio":2,"updatedAt":7}`}
	if err := store.Apply(ctx, "novel", []readingstate.Change{{Record: first, Delete: true}, {Record: broken}}); err == nil {
		t.Fatal("invalid progress accepted")
	}
	got, err := store.Load(ctx, "novel", nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("partial write: %+v %v", got, err)
	}
	if err := store.Apply(ctx, "novel", []readingstate.Change{{Record: first, Delete: true}}); err != nil {
		t.Fatal(err)
	}
	got, err = store.Load(ctx, "novel", nil)
	if err != nil || len(got) != 1 || got[0].Key != other.Key {
		t.Fatalf("unrelated book overwritten: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.Apply(ctx, "novel", []readingstate.Change{{Record: other, Delete: true}}); err == nil {
		t.Fatal("cancelled write acknowledged")
	}
}

func TestReadingRejectsCorruptStoredPayloadAndClosedDatabase(t *testing.T) {
	conn, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewReadingStateStore(conn)
	ctx := context.Background()
	record := readingFixture("comic", "one")
	if _, err := store.Load(ctx, "comic", []readingstate.Record{record}); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`UPDATE reading_state SET value='{}' WHERE kind='comic'`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Load(ctx, "comic", nil); err == nil {
		t.Fatal("corrupt journal displayed as empty state")
	}
	conn.Close()
	if err := store.Apply(ctx, "comic", []readingstate.Change{{Record: record}}); err == nil {
		t.Fatal("closed DB write acknowledged")
	}
}

func TestReadingMigrationV12PreservesExistingSetting(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{`INSERT INTO settings(key,value) VALUES('reading-migration','kept')`, `DROP TABLE reading_state`, `DROP TABLE reading_state_imports`, `PRAGMA user_version=12`} {
		if _, err := conn.Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()
	conn, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	var value string
	if err := conn.Get(&value, `SELECT value FROM settings WHERE key='reading-migration'`); err != nil || value != "kept" {
		t.Fatalf("existing setting lost: %s %v", value, err)
	}
	if _, err := NewReadingStateStore(conn).Load(context.Background(), "comic", nil); err != nil {
		t.Fatal(err)
	}
	backups, _ := filepath.Glob(filepath.Join(dir, "schema-backups", "*.db"))
	if len(backups) < 2 {
		t.Fatal("migration had no pre-upgrade backup")
	}
}
