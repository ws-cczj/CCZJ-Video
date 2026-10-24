package media

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestExplicitImportRevivesPurgedProgressWithoutMutatingSearchHit(t *testing.T) {
	ctx := context.Background()
	s := newTestMedia()
	key := "media_explicit_import"
	if err := db.AddSource(&model.Source{SourceKey: key, Name: key, Kind: "video", ApiUrl: "https://example.invalid", Enabled: 0}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteSource(key) })
	value := &model.Video{VodId: "one", VodName: "Explicit catalog import", TypeId: "import", TypeName: "Import category", VodContent: "detail body", VodPlayUrl: "episode$https://example.invalid/media", VodPlayFrom: "line"}
	before := *value
	if count, err := s.Import(ctx, key, []*model.Video{value, value, {VodName: "missing ID"}}); err != nil || count != 1 {
		t.Fatal("import must count valid unique coordinates", count, err)
	}
	if !reflect.DeepEqual(before, *value) {
		t.Fatal("import modified caller-owned search result")
	}
	if err := s.AddFavorite(ctx, key, "one"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveHistory(ctx, key, "one", 1, 35); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, DeleteVideoReq{SourceKey: key, VodId: "one"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.PurgeVideo(ctx, RecycleReq{SourceKey: key, VodId: "one"}); err != nil {
		t.Fatal(err)
	}
	items, err := s.RecentHistory(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		if item.SourceKey == key {
			t.Fatal("purged history resurfaced")
		}
	}
	if count, err := s.Import(ctx, key, []*model.Video{value}); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	if ok, err := s.IsFavorite(ctx, key, "one"); err != nil || !ok {
		t.Fatal("explicit reimport lost preserved favorite", err)
	}
	if position, err := s.HistoryPosition(ctx, key, "one", 1); err != nil || position != 35 {
		t.Fatal("explicit reimport lost progress", position, err)
	}
}

func TestVideoImportRejectsReadingSource(t *testing.T) {
	key := "media_reading_import"
	if err := db.AddSource(&model.Source{SourceKey: key, Name: key, Kind: "novel", ApiUrl: "https://example.invalid", Enabled: 0}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteSource(key) })
	if _, err := newTestMedia().Import(context.Background(), key, []*model.Video{{VodId: "one", VodName: "Wrong mode"}}); !apperror.Is(err, apperror.Unsupported) {
		t.Fatal(err)
	}
}

func TestCancelledCatalogMutationsDoNotPublishOrChangeRows(t *testing.T) {
	key, vod, id := seedCatalog(t, "cancel_mutations")
	s := newTestMedia()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	events := 0
	cache.SetEventPublisher(func(string, any) { events++ })
	t.Cleanup(func() { cache.SetEventPublisher(nil) })
	_, purge := s.PurgeVideo(ctx, RecycleReq{SourceKey: key, VodId: vod})
	_, clear := s.ClearRecycleBin(ctx, RecycleListReq{SourceKey: key})
	_, merge := s.MergeIdentities(ctx, MergeIdentitiesReq{GlobalIDs: []int64{id, id + 1}})
	_, importErr := s.Import(ctx, key, []*model.Video{{VodId: "new", VodName: "Cancelled"}})
	for _, err := range []error{purge, clear, merge, importErr, s.Delete(ctx, DeleteVideoReq{SourceKey: key, VodId: vod}), s.RestoreVideo(ctx, RecycleReq{SourceKey: key, VodId: vod})} {
		if !errors.Is(err, context.Canceled) {
			t.Fatal("mutation ignored cancellation", err)
		}
	}
	if events != 0 {
		t.Fatal("cancelled mutation broadcast invalidation", events)
	}
	if _, err := db.GetVideoById(key, vod); err != nil {
		t.Fatal("cancelled mutation changed catalog", err)
	}
}
