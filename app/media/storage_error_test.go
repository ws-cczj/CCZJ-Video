package media

import (
	"context"
	"errors"
	"testing"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
)

func TestFavoriteStorageFailureDoesNotLookUnfavorited(t *testing.T) {
	source, vod, _ := seedCatalog(t, "storage_failure")
	if _, err := db.DB().Exec(`ALTER TABLE favorites RENAME TO favorites_unavailable`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.DB().Exec(`ALTER TABLE favorites_unavailable RENAME TO favorites`); err != nil {
			t.Error(err)
		}
	})
	s := newTestMedia()
	if _, err := s.IsFavorite(context.Background(), source, vod); !apperror.Is(err, apperror.Storage) {
		t.Fatalf("storage failure became a successful false result: %v", err)
	}
}

func TestCancelledVideoRequestsCannotWriteFavoritesOrHistory(t *testing.T) {
	source, vod, _ := seedCatalog(t, "cancelled")
	s := newTestMedia()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, err := range []error{s.AddFavorite(ctx, source, vod), s.SaveHistory(ctx, source, vod, 1, 10)} {
		if !errors.Is(err, context.Canceled) {
			t.Fatal("cancellation lost", err)
		}
	}
	if favorite, err := s.IsFavorite(context.Background(), source, vod); err != nil || favorite {
		t.Fatal("cancelled request wrote a favorite", err)
	}
	if position, err := s.HistoryPosition(context.Background(), source, vod, 1); err != nil || position != 0 {
		t.Fatal("cancelled request wrote history", err)
	}
	if _, err := s.List(ctx, VideoListReq{SourceKey: source}); !errors.Is(err, context.Canceled) {
		t.Fatal("catalog read ignored context", err)
	}
	if _, err := s.Types(ctx, GetTypesReq{SourceKey: source}); !errors.Is(err, context.Canceled) {
		t.Fatal("type read ignored context", err)
	}
	if err := s.Delete(ctx, DeleteVideoReq{SourceKey: source, VodId: vod}); !errors.Is(err, context.Canceled) {
		t.Fatal("delete ignored context", err)
	}
	if _, err := db.GetVideoById(source, vod); err != nil {
		t.Fatal("cancelled delete changed catalog", err)
	}
}
