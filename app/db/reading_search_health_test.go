package db

import (
	"context"
	"testing"
	"time"

	"cczjVideo/app/model"
)

func TestReadingSearchFeedbackSurvivesReopenWithoutReplacingDeepChecks(t *testing.T) {
	previous, previousDir := instance, dataDir
	instance, dataDir = nil, ""
	root := t.TempDir()
	t.Cleanup(func() { Close(); instance, dataDir = previous, previousDir })
	if err := InitDB(root); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	now := time.Now().Unix()
	deep := ReadingSourceCheck{Kind: "novel", SourceID: "revision", CheckedAt: now, DeepAt: now, State: "deep_checked", Score: 81, Confidence: "limited", SummaryJSON: `{"preserved":true}`}
	if err := SaveReadingSourceCheck(ctx, deep); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := RecordReadingSearchHealth(ctx, model.SourceKindNovel, "revision", "TIMEOUT"); err != nil {
			t.Fatal(err)
		}
	}
	Close()
	if err := InitDB(root); err != nil {
		t.Fatal(err)
	}
	rows, err := ReadReadingSearchHealth(ctx, model.SourceKindNovel)
	if err != nil || rows["revision"].Failures != 3 || rows["revision"].RetryAfter < now+1800 {
		t.Fatal(rows, err)
	}
	checks, err := ReadReadingSourceChecks(ctx, model.SourceKindNovel)
	if err != nil || checks["revision"].Score != 81 || checks["revision"].State != "deep_checked" || checks["revision"].DeepAt != now {
		t.Fatal(checks, err)
	}
	comic, err := ReadReadingSearchHealth(ctx, model.SourceKindComic)
	if err != nil || len(comic) != 0 {
		t.Fatal(comic, err)
	}
	if err := RecordReadingSearchHealth(ctx, model.SourceKindNovel, "revision", ""); err != nil {
		t.Fatal(err)
	}
	rows, err = ReadReadingSearchHealth(ctx, model.SourceKindNovel)
	if err != nil || rows["revision"].Failures != 0 || rows["revision"].RetryAfter != 0 {
		t.Fatal(rows, err)
	}
}
