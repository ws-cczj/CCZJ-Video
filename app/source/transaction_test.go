package source

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestCreateSourceRollsBackIfDefaultPreferenceCannotBeSaved(t *testing.T) {
	previous, _ := db.GetSetting("default_source_key")
	if err := db.SetSetting("default_source_key", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`CREATE TRIGGER reject_source_default BEFORE INSERT ON settings WHEN NEW.key='default_source_key' BEGIN SELECT RAISE(ABORT,'reject default');END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.DB().Exec(`DROP TRIGGER reject_source_default`)
		_ = db.SetSetting("default_source_key", previous)
	})
	row := &model.Source{SourceKey: "atomic_source_default", Name: "Atomic default", ApiUrl: "https://example.invalid/atomic_source_default", Enabled: 1}
	if err := NewService().Add(context.Background(), row); err == nil {
		t.Fatal("partial source creation reported success")
	}
	if _, err := db.GetSourceByKey(row.SourceKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("failed source creation left a row", err)
	}
}

func TestExportDoesNotOverwriteAndCancellationLeavesNoFile(t *testing.T) {
	key := "export_unique_cancel"
	if err := db.AddSource(&model.Source{SourceKey: key, Name: key, ApiUrl: "https://example.invalid/" + key}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	svc := NewService()
	first, err := svc.ExportContext(context.Background(), dir, key)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.ExportContext(context.Background(), dir, key)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("repeated export overwrote the previous file")
	}
	after, err := os.ReadFile(first)
	if err != nil || string(before) != string(after) {
		t.Fatal("previous export changed", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := svc.ExportContext(ctx, dir, key); err == nil {
		t.Fatal("cancelled export succeeded")
	}
	entries, err := os.ReadDir(dir + "/exports")
	if err != nil || len(entries) != 2 {
		t.Fatal("cancelled export left a file", entries, err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".part") {
			t.Fatal("unfinished export remains")
		}
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, err = writeExportPayloadContext(ctx, cancellingExportWriter{cancel}, &model.Source{SourceKey: key}, nil, key)
	if !errors.Is(err, context.Canceled) {
		t.Fatal("streaming export ignored cancellation", err)
	}
}

type cancellingExportWriter struct{ cancel context.CancelFunc }

func (w cancellingExportWriter) Write(p []byte) (int, error) { w.cancel(); return io.Discard.Write(p) }

func TestCancelledSourceMutationCannotCreateOrDelete(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	row := &model.Source{SourceKey: "cancel_source_write", Name: "Cancelled", ApiUrl: "https://example.invalid", Enabled: 1}
	s := NewService()
	if err := s.Add(ctx, row); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := db.GetSourceByKey(row.SourceKey); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("cancelled create wrote a source", err)
	}
}

func TestImportRejectsInvalidCatalogBeforeWritingSource(t *testing.T) {
	for _, tc := range []struct {
		key    string
		kind   string
		videos []*CatalogVideo
	}{
		{"import_null_video", "video", []*CatalogVideo{nil}},
		{"import_novel_video", "novel", []*CatalogVideo{{SourceVodID: "v1", VodName: "Wrong mode"}}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			_, err := NewService().ImportContext(context.Background(), Payload{Source: &model.Source{SourceKey: tc.key, Kind: tc.kind, Name: tc.key, ApiUrl: "https://example.invalid"}, Videos: tc.videos}, "test")
			if err == nil {
				t.Fatal("invalid catalog accepted")
			}
			if _, err := db.GetSourceByKey(tc.key); !errors.Is(err, sql.ErrNoRows) {
				t.Fatal("invalid import wrote a source", err)
			}
		})
	}
}

func TestImportRollsBackConfigurationAndTypesOnCatalogFailure(t *testing.T) {
	key := "import_atomic_bundle"
	if err := db.AddSource(&model.Source{SourceKey: key, Name: "Before", ApiUrl: "https://example.invalid/" + key, Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`CREATE TRIGGER reject_import_catalog BEFORE INSERT ON source_videos WHEN NEW.source_key='import_atomic_bundle' BEGIN SELECT RAISE(ABORT,'reject catalog');END`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DB().Exec(`DROP TRIGGER reject_import_catalog`) })
	_, err := NewService().ImportContext(context.Background(), Payload{Source: &model.Source{SourceKey: key, Name: "After", ApiUrl: "https://changed.invalid"}, Types: []TypePayload{{TypeID: "t1", TypeName: "Film"}}, Videos: []*CatalogVideo{{SourceVodID: "v1", SourceTypeID: "t1", TypeName: "Film", VodName: "Atomic"}}}, "test")
	if err == nil {
		t.Fatal("failed catalog import reported success")
	}
	src, err := db.GetSourceByKey(key)
	if err != nil || src.Name != "Before" || src.ApiUrl != "https://example.invalid/"+key {
		t.Fatalf("configuration partially committed: %+v %v", src, err)
	}
	types, err := db.ExportSourceTypes(key)
	if err != nil || len(types) != 0 {
		t.Fatalf("types partially committed: %+v %v", types, err)
	}
	if _, err := db.GetCatalogItem(key, "v1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatal("catalog partially committed", err)
	}
}
