package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/readingstate"
)

func TestBackupV2ExportsReadingAndArchiveRecoveryWithoutDroppingV1(t *testing.T) {
	resetUserTables(t)
	defer resetUserTables(t)
	for _, statement := range []string{`DELETE FROM reading_state`, `DELETE FROM reading_state_imports`} {
		if _, err := db.DB().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { db.DB().Exec(`DELETE FROM reading_state`); db.DB().Exec(`DELETE FROM reading_state_imports`) })
	ctx := context.Background()
	store := db.NewReadingStateStore(db.DB())
	preference := readingstate.Record{Bucket: "preferences", Key: "default", Value: `{"size":22,"theme":"paper"}`}
	if _, err := store.Load(ctx, "novel", []readingstate.Record{preference}); err != nil {
		t.Fatal(err)
	}
	s := NewService()
	payload, err := s.buildPayload(testDirs.data)
	if err != nil || payload.Version != 2 || payload.Reading == nil || len(payload.Reading.Records) != 1 {
		t.Fatalf("reading export = %+v %v", payload, err)
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportBytes("reading.json", raw, "test"); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "snapshot.db")
	if _, err := db.DB().Exec("VACUUM INTO '" + strings.ReplaceAll(filepath.ToSlash(archive), "'", "''") + "'"); err != nil {
		t.Fatal(err)
	}
	read, err := readArchive(archive)
	if err != nil || read.Reading == nil || len(read.Reading.Records) != 1 {
		t.Fatalf("archive omitted reading: %+v %v", read.Reading, err)
	}
	if _, err := s.Import(Payload{Kind: payloadKind, Version: 1}, "legacy-v1"); err != nil {
		t.Fatalf("old v1 backup rejected: %v", err)
	}
	if _, err := s.Import(Payload{Kind: payloadKind, Version: payloadVersion + 1}, "future"); !apperror.Is(err, apperror.Unsupported) {
		t.Fatalf("future backup = %v", err)
	}
}
