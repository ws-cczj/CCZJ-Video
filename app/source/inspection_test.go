package source

import (
	"context"
	"errors"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestSourceInspectionCountsOnlyItsOwnCatalog(t *testing.T) {
	for _, key := range []string{"inspect_one", "inspect_two"} {
		if err := db.AddSource(&model.Source{SourceKey: key, Name: key, ApiUrl: "https://example.invalid/" + key}); err != nil {
			t.Fatal(err)
		}
		if err := db.UpsertCatalogItems(key, []*model.Video{{VodId: "v1", VodName: key, TypeId: "t1", TypeName: "Inspection"}}); err != nil {
			t.Fatal(err)
		}
	}
	detail, err := GetSourceDetail("inspect_one")
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range detail.Tables {
		if table.TableName == "source_videos" && table.RowCount != 1 {
			t.Fatalf("source inspection includes another source's catalog: %+v", table)
		}
	}
}

func TestSourceStatsCannotReportZeroWhenCatalogStorageFails(t *testing.T) {
	if err := db.AddSource(&model.Source{SourceKey: "inspect_failure", Name: "Failure", ApiUrl: "https://example.invalid/inspect_failure"}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB().Exec(`ALTER TABLE source_videos RENAME TO source_videos_probe`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.DB().Exec(`ALTER TABLE source_videos_probe RENAME TO source_videos`); err != nil {
			t.Fatal(err)
		}
	}()
	if _, err := GetSourceStats(); apperror.CodeOf(err) != apperror.Storage {
		t.Fatal("storage failure did not retain its storage classification", err)
	}
}

func TestCancelledSourceInspectionCannotPublishCounts(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetSourceDetailContext(ctx, "cancelled_inspection"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := GetSourceStatsContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
