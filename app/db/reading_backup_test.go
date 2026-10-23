package db

import (
	"context"
	"encoding/json"
	"testing"

	"cczjVideo/app/model"
	"cczjVideo/app/readingstate"
)

func TestReadingBackupRejectsAnotherBooksChapterBeforeWriting(t *testing.T) {
	conn, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	backup := &BackupReading{
		Items:    []*model.LibraryItem{{Id: 7, Kind: "novel", SourceKey: "source", RemoteID: "one", Title: "One"}, {Id: 8, Kind: "novel", SourceKey: "source", RemoteID: "two", Title: "Two"}},
		Chapters: []*model.LibraryChapter{{Id: 11, ItemID: 8, RemoteID: "chapter", Title: "Other book"}},
		Records:  []BackupReadingRecord{{Kind: "novel", Record: readingstate.Record{Bucket: "progress", Key: "novel\x00source\x00one", Value: `{"itemId":7,"chapterId":11,"chapterOrd":0,"ratio":0.5,"updatedAt":1}`}}},
	}
	if _, err := NewReadingStateStore(conn).MergeBackup(context.Background(), backup); err == nil {
		t.Fatal("cross-book chapter identity accepted")
	}
	var count int
	if err := conn.Get(&count, `SELECT COUNT(*) FROM library_items`); err != nil || count != 0 {
		t.Fatalf("invalid backup partially wrote catalog: %d %v", count, err)
	}
}

func TestReadingBackupRemapsCatalogAndChapterIDsAndKeepsLocalEdits(t *testing.T) {
	source, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	destination, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	for _, stmt := range []string{
		`INSERT INTO library_items(id,kind,source_key,remote_id,title) VALUES(7,'novel','source','one','Book')`,
		`INSERT INTO library_chapters(id,item_id,remote_id,title,ord) VALUES(8,7,'chapter','Chapter',0)`,
	} {
		if _, err := source.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	// Occupy the old numeric IDs with unrelated data on the destination.
	for _, stmt := range []string{
		`INSERT INTO library_items(id,kind,source_key,remote_id,title) VALUES(7,'novel','other','different','Other')`,
		`INSERT INTO library_chapters(id,item_id,remote_id,title,ord) VALUES(8,7,'other-chapter','Other chapter',0)`,
	} {
		if _, err := destination.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	store := NewReadingStateStore(source)
	progress := readingstate.Record{Bucket: "progress", Key: "novel\x00source\x00one", Value: `{"itemId":7,"title":"Book","chapterId":8,"chapterOrd":0,"chapterRemoteId":"chapter","ratio":0.5,"updatedAt":10}`}
	if _, err := store.Load(ctx, "novel", []readingstate.Record{readingFixture("novel", "one"), progress}); err != nil {
		t.Fatal(err)
	}
	backup, err := store.ReadBackup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	target := NewReadingStateStore(destination)
	count, err := target.MergeBackup(ctx, backup)
	if err != nil || count != 2 {
		t.Fatalf("merge = %d %v", count, err)
	}
	records, err := target.Load(ctx, "novel", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range records {
		var value map[string]any
		json.Unmarshal([]byte(record.Value), &value)
		if value["itemId"].(float64) == 7 {
			t.Fatal("backup preserved foreign local item ID")
		}
		if record.Bucket == "progress" {
			if value["chapterId"].(float64) == 8 {
				t.Fatal("backup preserved foreign chapter ID")
			}
			value["ratio"] = 0.9
			raw, _ := json.Marshal(value)
			if err := target.Apply(ctx, "novel", []readingstate.Change{{Record: readingstate.Record{Bucket: record.Bucket, Key: record.Key, Value: string(raw)}}}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if count, err := target.MergeBackup(ctx, backup); err != nil || count != 0 {
		t.Fatalf("repeated import = %d %v", count, err)
	}
	records, _ = target.Load(ctx, "novel", nil)
	for _, record := range records {
		if record.Bucket == "progress" {
			var value map[string]any
			json.Unmarshal([]byte(record.Value), &value)
			if value["ratio"] != 0.9 {
				t.Fatal("backup overwrote newer local progress")
			}
		}
	}
}

func TestReadingBackupPreservesOrphanSnapshotWithoutUnrelatedIdentity(t *testing.T) {
	source, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	if _, err := source.Exec(`INSERT INTO library_items(id,kind,source_key,remote_id,title) VALUES(7,'novel','other','different','Wrong book')`); err != nil {
		t.Fatal(err)
	}
	store := NewReadingStateStore(source)
	ctx := context.Background()
	if _, err := store.Load(ctx, "novel", []readingstate.Record{readingFixture("novel", "one")}); err != nil {
		t.Fatal(err)
	}
	backup, err := store.ReadBackup(ctx)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer destination.Close()
	target := NewReadingStateStore(destination)
	if count, err := target.MergeBackup(ctx, backup); err != nil || count != 1 {
		t.Fatalf("orphan merge = %d %v", count, err)
	}
	records, err := target.Load(ctx, "novel", nil)
	if err != nil || len(records) != 1 {
		t.Fatalf("orphan snapshot = %+v %v", records, err)
	}
	var value struct {
		ItemID int `json:"itemId"`
	}
	json.Unmarshal([]byte(records[0].Value), &value)
	var remote, title string
	if err := destination.QueryRow(`SELECT remote_id,title FROM library_items WHERE id=?`, value.ItemID).Scan(&remote, &title); err != nil || remote != "one" || title != "Offline title" {
		t.Fatalf("wrong identity = %s %s %v", remote, title, err)
	}
}
