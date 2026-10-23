package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestNovelReaderWindowDoesNotReturnWholeCatalog(t *testing.T) {
	s := setupLibraryTest(t)
	server := fakeSource()
	defer server.Close()
	addTestSource(t, server, "reader_window", novelDocument())
	ids, err := db.UpsertLibraryItems(model.SourceKindNovel, "reader_window", []*model.LibraryItem{{RemoteID: "/long", Title: "长篇"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]*model.LibraryChapter, 10000)
	for i := range rows {
		rows[i] = &model.LibraryChapter{RemoteID: fmt.Sprint(i), Ord: i, Title: fmt.Sprintf("第%d章", i)}
	}
	rows[4999].IsVolume = true
	if _, _, _, err := db.ReplaceLibraryChapters(ids[0], rows); err != nil {
		t.Fatal(err)
	}
	chapter, err := db.GetLibraryChapterByOrd(ids[0], 5000)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	location := NovelReadingLocation{ItemID: ids[0], ChapterID: chapter.Id}
	view, err := s.NovelContext(ctx, location)
	if err != nil {
		t.Fatal(err)
	}
	if view.Count != 9999 || view.Index != 4999 || view.Previous.Ord != 4998 || view.Next.Ord != 5001 || view.CacheIdentity == "" {
		t.Fatalf("bad bounded window: %+v", view)
	}
	raw, _ := json.Marshal(view)
	if len(raw) > 4096 || strings.Contains(string(raw), `"chapters"`) {
		t.Fatalf("reader returned full directory (%d bytes)", len(raw))
	}
	// A stale numeric ID must not beat a restored stable identity.
	location.RemoteID = "5002"
	recovered, err := s.NovelContext(ctx, location)
	if err != nil || recovered.Chapter.Ord != 5002 {
		t.Fatal(recovered, err)
	}
	location.RemoteID = "gone"
	missing, err := s.NovelContext(ctx, location)
	if err != nil || missing.Chapter != nil {
		t.Fatal("missing identity silently restarted", missing, err)
	}
	// Retired rows remain addressable but are skipped as navigation neighbors.
	if _, err := db.DB().Exec(`UPDATE library_chapters SET lifecycle_state='retired' WHERE item_id=? AND ord IN (5000,5001)`, ids[0]); err != nil {
		t.Fatal(err)
	}
	retired, err := s.NovelContext(ctx, NovelReadingLocation{ItemID: ids[0], ChapterID: chapter.Id})
	if err != nil || retired.Chapter.Available || retired.Next.Ord != 5002 {
		t.Fatal(retired, err)
	}
	changed := strings.Replace(novelDocument(), `"attr": "text"`, `"attr": "ownText"`, 1)
	if err := s.UpdateSource("reader_window", "", "", changed); err != nil {
		t.Fatal(err)
	}
	updated, err := s.NovelContext(ctx, NovelReadingLocation{ItemID: ids[0], ChapterID: chapter.Id})
	if err != nil || updated.CacheIdentity == view.CacheIdentity {
		t.Fatal("source revision retained old hot cache", updated, err)
	}
	t.Logf("10000 directory rows -> %d reader JSON bytes", len(raw))
	if _, _, _, err := db.ReplaceLibraryChapters(ids[0], rows[:2]); err != nil {
		t.Fatal(err)
	}
	active, err := db.ListLibraryChapters(ids[0], false)
	if err != nil || len(active) != 2 {
		t.Fatal("batched retirement lost catalog consistency", len(active), err)
	}
}

func TestNovelWindowRefreshAndCancelledRead(t *testing.T) {
	s := setupLibraryTest(t)
	server := fakeSource()
	defer server.Close()
	addTestSource(t, server, "reader_bootstrap", novelDocument())
	found, err := s.Search(context.Background(), model.SourceKindNovel, "example", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	location := NovelReadingLocation{ItemID: found.Results[0].Items[0].ID, First: true}
	first, err := s.NovelContext(context.Background(), location)
	if err != nil || first.Chapter == nil || first.Index != 0 {
		t.Fatal(first, err)
	}
	next, err := s.RefreshNovelContext(context.Background(), location)
	if err != nil || next.Chapter.ID != first.Chapter.ID {
		t.Fatal(next, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.NovelContext(ctx, location); err == nil {
		t.Fatal("cancelled reader query continued")
	}
}

func TestComicReaderWindowSeparatesModesAndBoundsLongCatalog(t *testing.T) {
	s := setupLibraryTest(t)
	server := fakeSource()
	defer server.Close()
	doc := strings.Replace(novelDocument(), `"kind": "novel"`, `"kind": "comic"`, 1)
	doc = strings.Replace(doc, `"operations": {`, `"operations": {"pages":{"format":"html","list":{"query":"img"},"fields":{"image_url":{"attr":"src"}}},`, 1)
	if err := db.AddSource(&model.Source{SourceKey: "comic_window", Name: "漫画窗口", ApiUrl: server.URL, Kind: "comic", Enabled: 1, StrategyConfig: doc}); err != nil {
		t.Fatal(err)
	}
	ids, err := db.UpsertLibraryItems(model.SourceKindComic, "comic_window", []*model.LibraryItem{{RemoteID: "/series", Title: "长篇漫画"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]*model.LibraryChapter, 3000)
	for i := range rows {
		rows[i] = &model.LibraryChapter{RemoteID: fmt.Sprint(i), Ord: i, Title: fmt.Sprintf("第%d话", i)}
	}
	if _, _, _, err = db.ReplaceLibraryChapters(ids[0], rows); err != nil {
		t.Fatal(err)
	}
	location := ReadingLocation{ItemID: ids[0], RemoteID: "1500"}
	view, err := s.ComicContext(context.Background(), location)
	if err != nil || view.Chapter == nil || view.Chapter.Ord != 1500 || view.Previous.Ord != 1499 || view.Next.Ord != 1501 || view.Count != 3000 || view.CacheIdentity == "" {
		t.Fatalf("bad comic window: %+v %v", view, err)
	}
	raw, _ := json.Marshal(view)
	if len(raw) > 4096 || strings.Contains(string(raw), `"chapters"`) {
		t.Fatal("full catalog sent to comic reader", len(raw))
	}
	if _, err = s.NovelContext(context.Background(), NovelReadingLocation(location)); err == nil {
		t.Fatal("novel accepted comic identity")
	}
	location.RemoteID = "missing"
	if view, err = s.ComicContext(context.Background(), location); err != nil || view.Chapter != nil {
		t.Fatal("missing durable ID fell back to ordinal", view, err)
	}
	t.Logf("3000 episodes -> %d reader JSON bytes", len(raw))
}
