package db

import (
	"context"
	"testing"
)

func TestReadOnlyHistoryBackupWithoutDuration(t *testing.T) {
	database := openFreshSQLite(t, t.TempDir())
	_, err := database.Exec(`CREATE TABLE global_video(id INTEGER PRIMARY KEY,vod_name TEXT,year TEXT);
		INSERT INTO global_video VALUES (1,'Old saved work','2011');
		CREATE TABLE watch_history(id INTEGER PRIMARY KEY,global_id INTEGER,source_key TEXT,vod_id TEXT,ep_num INTEGER,position REAL,updated_at TEXT);
		INSERT INTO watch_history VALUES(1,1,'old_source','work',5,1671.566798,'2026-10-26 03:40:00');
		PRAGMA query_only=TRUE`)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := ReadHistoryContext(context.Background(), database)
	if err != nil {
		t.Fatalf("cannot restore pre-v22 snapshot: %v", err)
	}
	if len(rows) != 1 || rows[0].Position != 1671.566798 || rows[0].Duration != 0 || rows[0].EpNum != 5 {
		t.Fatalf("old history changed: %+v", rows)
	}
	var columns int
	if err := database.Get(&columns, `SELECT COUNT(*) FROM pragma_table_info('watch_history') WHERE name='duration'`); err != nil || columns != 0 {
		t.Fatalf("snapshot was modified: %d %v", columns, err)
	}
}
