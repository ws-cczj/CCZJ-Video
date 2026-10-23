package db

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/download"
)

func TestDownloadMigrationPreservesV11Settings(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		`INSERT INTO settings(key,value) VALUES('download-upgrade-proof','kept')`,
		`DROP TABLE download_tasks`, `DROP TABLE download_task_imports`, `PRAGMA user_version=11`,
	} {
		if _, err := conn.Exec(statement); err != nil {
			conn.Close()
			t.Fatal(err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer upgraded.Close()
	var value string
	if err := upgraded.Get(&value, `SELECT value FROM settings WHERE key='download-upgrade-proof'`); err != nil || value != "kept" {
		t.Fatalf("existing settings = %q %v", value, err)
	}
	var version int
	if err := upgraded.Get(&version, `PRAGMA user_version`); err != nil || version != LatestSchemaVersion() {
		t.Fatalf("version = %d %v", version, err)
	}
	if _, err := NewDownloadTaskStore(upgraded).Restore(context.Background(), filepath.Join(dir, "absent.json")); err != nil {
		t.Fatal(err)
	}
}

func TestMigrationBackupsDoNotCollideAcrossRapidRetries(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	paths := make(map[string]bool)
	for range 3 {
		path, err := backupBeforeMigrations(conn, dir, 1)
		if err != nil {
			t.Fatalf("retry backup: %v", err)
		}
		if paths[path] {
			t.Fatal("backup retry reused an earlier snapshot")
		}
		paths[path] = true
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Fatalf("incomplete snapshot: %v", err)
		}
	}
}

func TestDownloadImportIsAtomicRetainsOriginalAndDoesNotResurrect(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewDownloadTaskStore(conn)
	ctx := context.Background()
	path := filepath.Join(dir, "downloads.json")
	valid := download.Checkpoint{TaskId: "legacy", Url: "https://example.test/a", SavePath: filepath.Join(dir, "a.bin"), Status: "cancelled", Downloaded: 3}
	invalid := valid
	invalid.TaskId, invalid.Status = "invalid", "unknown"
	raw, _ := json.Marshal([]download.Checkpoint{valid, invalid})
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore(ctx, path); err == nil {
		t.Fatal("malformed legacy record accepted")
	}
	for _, table := range []string{"download_tasks", "download_task_imports"} {
		var count int
		if err := conn.Get(&count, "SELECT COUNT(*) FROM "+table); err != nil || count != 0 {
			t.Fatalf("partial import in %s: %d %v", table, count, err)
		}
	}
	if after, _ := os.ReadFile(path); string(after) != string(raw) {
		t.Fatal("failed import changed recovery file")
	}
	raw, _ = json.Marshal([]download.Checkpoint{valid})
	os.WriteFile(path, raw, 0600)
	items, err := store.Restore(ctx, path)
	if err != nil || len(items) != 1 || items[0].Status != "cancelled" {
		t.Fatalf("restored = %+v %v", items, err)
	}
	if err := store.Delete(ctx, "legacy"); err != nil {
		t.Fatal(err)
	}
	// Reopen the actual database while leaving the original JSON in place.
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	items, err = NewDownloadTaskStore(reopened).Restore(ctx, path)
	if err != nil || len(items) != 0 {
		t.Fatalf("deleted record resurrected: %+v %v", items, err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(raw) {
		t.Fatal("successful import changed original JSON")
	}
}

func TestDownloadRestorePreservesTerminalsAndReconcilesPublication(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewDownloadTaskStore(conn)
	ctx := context.Background()
	tests := []struct{ state, file, want string }{
		{"queued", "none", "paused"}, {"downloading", "none", "paused"}, {"paused", "none", "paused"},
		{"error", "none", "error"}, {"cancelled", "part", "cancelled"}, {"done", "final", "done"},
		{"finalizing", "final", "done"}, {"finalizing", "part", "paused"}, {"finalizing", "none", "error"},
		{"finalizing", "wrong-size", "error"},
	}
	for index, test := range tests {
		id := string(rune('a' + index))
		path := filepath.Join(dir, id+".bin")
		if test.file == "part" {
			os.WriteFile(path+".part", []byte("ok"), 0600)
		}
		if test.file == "final" {
			os.WriteFile(path, []byte("ok"), 0600)
		}
		if test.file == "wrong-size" {
			os.WriteFile(path, []byte("wrong"), 0600)
		}
		cp := download.Checkpoint{TaskId: id, Url: "https://example.test/" + id, SavePath: path, Status: test.state, FinalSize: 2}
		if err := store.Save(ctx, cp); err != nil {
			t.Fatal(err)
		}
	}
	items, err := store.Restore(ctx, filepath.Join(dir, "missing.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != len(tests) {
		t.Fatalf("restored %d tasks", len(items))
	}
	for index, test := range tests {
		id := string(rune('a' + index))
		found := false
		for _, cp := range items {
			if cp.TaskId == id {
				found = true
				if cp.Status != test.want {
					t.Errorf("%s/%s = %s, want %s", test.state, test.file, cp.Status, test.want)
				}
			}
		}
		if !found {
			t.Errorf("missing %s", id)
		}
	}
}

func TestDownloadRestoreRejectsCorruptJournalWithoutPartialRecovery(t *testing.T) {
	dir := t.TempDir()
	conn, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	store := NewDownloadTaskStore(conn)
	ctx := context.Background()
	cp := download.Checkpoint{TaskId: "valid", Url: "https://example.test/a", SavePath: filepath.Join(dir, "a.bin"), Status: "downloading"}
	if err := store.Save(ctx, cp); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(`INSERT INTO download_tasks(task_id,status,payload_version,payload,updated_at) VALUES('broken','paused',1,'{',0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore(ctx, filepath.Join(dir, "absent.json")); err == nil {
		t.Fatal("corrupt journal accepted")
	}
	var state string
	if err := conn.Get(&state, `SELECT status FROM download_tasks WHERE task_id='valid'`); err != nil || state != "downloading" {
		t.Fatalf("partially recovered journal: %s %v", state, err)
	}
	conn.Close()
	if err := store.Save(ctx, cp); err == nil {
		t.Fatal("closed database reported a successful write")
	}
}
