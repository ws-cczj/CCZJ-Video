package library

import (
	"context"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/model"
)

func TestChapterMatchPersistsMappingWithoutMergingSourceIdentity(t *testing.T) {
	s, _ := managedFixture(t, 2, false)
	ctx := context.Background()
	sources, err := db.GetSourcesByKind(model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	var items, chapters []int
	for i, source := range sources {
		ids, err := db.UpsertLibraryItems(model.SourceKindNovel, source.SourceKey, []*model.LibraryItem{{RemoteID: "same", Title: "同一本书", Author: "同一作者"}}, true)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, ids[0])
		title := "第一章 起风"
		if i == 1 {
			title = "第1章 起风"
		}
		if _, _, _, err := db.ReplaceLibraryChapters(ids[0], []*model.LibraryChapter{{RemoteID: "c", Title: title, Ord: 0}}); err != nil {
			t.Fatal(err)
		}
		c, err := db.ListLibraryChapters(ids[0], false)
		if err != nil {
			t.Fatal(err)
		}
		chapters = append(chapters, c[0].Id)
	}
	r, err := s.MatchChapter(ctx, model.SourceKindNovel, items[0], chapters[0], items[1])
	if err != nil || r.Status != "matched" || len(r.Targets) != 1 || r.Targets[0].ID != chapters[1] {
		t.Fatal(r, err)
	}
	var n int
	if err := db.DB().Get(&n, `SELECT COUNT(*) FROM reading_chapter_alignments WHERE kind='novel' AND from_item=? AND to_item=?`, items[0], items[1]); err != nil || n != 1 {
		t.Fatal(n, err)
	}
	if _, err := s.MatchChapter(ctx, model.SourceKindComic, items[0], chapters[0], items[1]); err == nil {
		t.Fatal("cross-mode mapping accepted")
	}
	if _, err := s.MatchChapter(ctx, model.SourceKindNovel, items[0], chapters[1], items[1]); err == nil {
		t.Fatal("foreign original chapter accepted")
	}
	unknown, err := s.MatchChapter(ctx, model.SourceKindNovel, items[0], 0, items[1])
	if err != nil || unknown.Status != "unmatched" || len(unknown.Targets) != 0 {
		t.Fatal("unknown original position became an automatic target", unknown, err)
	}
}

func TestRepeatedProbeFailuresCoolDownWithoutDisablingSource(t *testing.T) {
	_, _ = managedFixture(t, 1, false)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		if err := db.SaveReadingSourceCheck(ctx, db.ReadingSourceCheck{Kind: "novel", SourceID: "cooling", CheckedAt: time.Now().Unix() + int64(i)*86400, State: "network_unknown", Score: -1, Confidence: "insufficient"}); err != nil {
			t.Fatal(err)
		}
	}
	checks, err := db.ReadReadingSourceChecks(ctx, model.SourceKindNovel)
	if err != nil || checks["cooling"].Failures != 3 || checks["cooling"].RetryAfter <= time.Now().Unix() {
		t.Fatal(checks, err)
	}
	sources, err := db.GetSourcesByKind(model.SourceKindNovel)
	if err != nil || len(sources) != 1 || sources[0].Enabled != 1 {
		t.Fatal("probe disabled a source", sources, err)
	}
}

func TestPageListsDoNotProveImagesAndDoNotEraseValidatedImageEvidence(t *testing.T) {
	s := setupLibraryTest(t)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	ctx := context.Background()
	ids, err := db.UpsertLibraryItems(model.SourceKindComic, "evidence_comic", []*model.LibraryItem{{RemoteID: "one", Title: "漫画校准", Author: "作者"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.ReplaceLibraryChapters(ids[0], []*model.LibraryChapter{{RemoteID: "ep", RemoteURL: "https://example.org/episode/1", Title: "第1话 星光"}}); err != nil {
		t.Fatal(err)
	}
	chapters, err := db.ListLibraryChapters(ids[0], false)
	if err != nil {
		t.Fatal(err)
	}
	id := chapters[0].Id
	if err := db.RecordReadingWorks(ctx, model.SourceKindComic, "evidence_comic", "rev", []db.ReadingWorkEvidence{{ItemID: ids[0], TitleKey: matching.Normalize("漫画校准")}}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordReadingChapter(ctx, model.SourceKindComic, id, "rev", "page_list"); err != nil {
		t.Fatal(err)
	}
	known, err := db.KnownReadingRoutes(ctx, model.SourceKindComic, matching.Normalize("漫画校准"))
	if err != nil || len(known) != 1 || known[0].Readable != 0 {
		t.Fatal("page list promoted to image proof", known, err)
	}
	if err := db.RecordReadingImage(ctx, "evidence_comic", "rev", "https://example.org/episode/1"); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordReadingChapter(ctx, model.SourceKindComic, id, "rev", "page_list"); err != nil {
		t.Fatal(err)
	}
	known, err = db.KnownReadingRoutes(ctx, model.SourceKindComic, matching.Normalize("漫画校准"))
	if err != nil || known[0].Readable != 1 {
		t.Fatal("page refresh lost image proof", known, err)
	}
	if err := db.RecordReadingWorks(ctx, model.SourceKindComic, "evidence_comic", "new-rev", []db.ReadingWorkEvidence{{ItemID: ids[0], TitleKey: matching.Normalize("漫画校准")}}); err != nil {
		t.Fatal(err)
	}
	known, err = db.KnownReadingRoutes(ctx, model.SourceKindComic, matching.Normalize("漫画校准"))
	if err != nil || known[0].Readable != 0 {
		t.Fatal("old revision image proof reused", known, err)
	}
}
