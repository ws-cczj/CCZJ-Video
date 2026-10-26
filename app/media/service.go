// Package media owns video browsing, search, detail, line selection, favorites
// and watch history. Bindings supply request contexts; detail work has a joined
// application owner for shared fetches and stale refreshes.
package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/detail"
	"cczjVideo/app/model"
	"context"
	"database/sql"
	"errors"
)

// Service provides media-facing application operations.
type Service struct {
	store   *db.MediaStore
	details *detail.Service
	lookup  func(context.Context, string, string) (*model.Video, error)
}

// NewService constructs a media service.
func NewService(lookup func(context.Context, string, string) (*model.Video, error), store *db.MediaStore, details *detail.Service) *Service {
	return &Service{lookup: lookup, store: store, details: details}
}

// Recommend returns recommended videos after normalizing optional arguments.
func (s *Service) Recommend(ctx context.Context, sourceKey string, limit int, excludeIDs []string) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 8
	}
	if excludeIDs == nil {
		excludeIDs = []string{}
	}
	return db.GetCatalogRecommendContext(ctx, sourceKey, limit, excludeIDs)
}

// ShortDramaRecommendations owns the feed batch size; ordinary recommendations
// keep their existing behavior. Source and visibility rules remain in the catalog.
func (s *Service) ShortDramaRecommendations(ctx context.Context, sourceKey string, excludeIDs []string) ([]*model.Video, error) {
	return db.GetShortDramaRecommendContext(ctx, sourceKey, 5, excludeIDs)
}

// Similar returns videos sharing a type with the requested video.
func (s *Service) Similar(ctx context.Context, sourceKey, typeID string, limit int, excludeIDs []string) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 8
	}
	if excludeIDs == nil {
		excludeIDs = []string{}
	}
	page, err := db.GetCatalogVideoPageContext(ctx, sourceKey, db.FilterParams{TypeId: typeID, Page: 1, PageSize: limit})
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	for _, id := range excludeIDs {
		excluded[id] = true
	}
	out := page.Videos[:0]
	for _, v := range page.Videos {
		if !excluded[v.VodId.String()] {
			out = append(out, v)
		}
	}
	return out, nil
}

// AddFavorite creates a global favorite. The favorite is shared by all sources
// with the same global_id.
func (s *Service) AddFavorite(ctx context.Context, sourceKey, vodID string) error {
	globalID, err := s.catalogGlobalID(ctx, sourceKey, vodID)
	if err != nil {
		return err
	}
	return s.store.AddFavoriteByIdentity(ctx, globalID, sourceKey, vodID)
}

// RemoveFavorite removes the global favorite shared by matching sources.
func (s *Service) RemoveFavorite(ctx context.Context, sourceKey, vodID string) error {
	globalID, err := s.catalogGlobalID(ctx, sourceKey, vodID)
	if err != nil {
		return err
	}
	return s.store.RemoveFavoriteByGlobalID(ctx, int(globalID), sourceKey)
}

// IsFavorite reports whether the global video is favorited.
func (s *Service) IsFavorite(ctx context.Context, sourceKey, vodID string) (bool, error) {
	v, err := s.lookup(ctx, sourceKey, vodID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, apperror.Wrap(apperror.Storage, err, "read favorite identity")
	}
	if v == nil || v.GlobalId <= 0 {
		return false, apperror.New(apperror.Internal, "video has no catalog global identity")
	}
	value, err := s.store.IsFavoriteByGlobalID(ctx, int(v.GlobalId))
	if err != nil {
		return false, apperror.Wrap(apperror.Storage, err, "read favorite")
	}
	return value, nil
}

// Favorites returns the requested page of global favorites.
func (s *Service) Favorites(ctx context.Context, page, pageSize int) ([]db.FavWithVideo, error) {
	return s.store.GetFavorites(ctx, page, pageSize)
}

// SaveHistory records progress for a source episode under its global video.
func (s *Service) SaveHistory(ctx context.Context, sourceKey, vodID string, epNum int, position float64, duration ...float64) error {
	globalID, err := s.catalogGlobalID(ctx, sourceKey, vodID)
	if err != nil {
		return err
	}
	return s.store.SaveWatchHistoryByIdentity(ctx, globalID, sourceKey, vodID, epNum, position, duration...)
}

// HistoryPosition returns the last watched position (seconds) of one episode.
// A missing record is not an error: 0 means "no history yet".
func (s *Service) HistoryPosition(ctx context.Context, sourceKey, vodID string, epNum int) (float64, error) {
	globalID, err := s.catalogGlobalID(ctx, sourceKey, vodID)
	if err != nil {
		return 0, err
	}
	position, err := s.store.GetWatchHistoryByIdentity(ctx, globalID, sourceKey, vodID, epNum)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return position, nil
}

// RecentHistory returns hydrated recent watch-history records.
func (s *Service) RecentHistory(ctx context.Context, limit int) ([]*HistoryItemWithVideo, error) {
	if limit <= 0 {
		limit = 20
	}
	raw, err := s.store.GetRecentHistory(ctx, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*HistoryItemWithVideo, 0, len(raw))
	for _, r := range raw {
		out = append(out, &HistoryItemWithVideo{GlobalID: r.GlobalID, SourceKey: r.SourceKey, VodId: r.VodId, EpNum: r.EpNum, Position: r.Position, Duration: r.Duration, UpdatedAt: r.UpdatedAt, VodName: r.VodName, VodPic: r.VodPic, VodRemarks: r.VodRemarks})
	}
	return out, nil
}

// DeleteHistoryItem removes a watched episode from its global history.
func (s *Service) DeleteHistoryItem(ctx context.Context, sourceKey, vodID string, epNum int) error {
	globalID, err := s.catalogGlobalID(ctx, sourceKey, vodID)
	if err != nil {
		return err
	}
	return s.store.DeleteHistoryItemByIdentity(ctx, globalID, sourceKey, vodID, epNum)
}

// DeleteHistoryByVideo removes source-specific history for compatibility.
func (s *Service) DeleteHistoryByVideo(ctx context.Context, sourceKey, vodID string) error {
	return s.store.DeleteHistoryByVideo(ctx, sourceKey, vodID)
}

// ClearHistory removes every watch-history record.
func (s *Service) ClearHistory(ctx context.Context) (int, error) {
	n, err := s.store.ClearAllHistory(ctx)
	return int(n), err
}

// WatchedEpisodes returns watched episode numbers for the global video.
func (s *Service) WatchedEpisodes(ctx context.Context, sourceKey, vodID string) ([]int, error) {
	globalID, err := s.catalogGlobalID(ctx, sourceKey, vodID)
	if err != nil {
		return nil, err
	}
	return s.store.GetWatchedEpisodesByIdentity(ctx, globalID, sourceKey, vodID)
}

func (s *Service) catalogGlobalID(ctx context.Context, sourceKey, vodID string) (int64, error) {
	v, err := s.lookup(ctx, sourceKey, vodID)
	if err != nil {
		return 0, apperror.Wrap(apperror.Storage, err, "read video identity")
	}
	if v == nil || v.GlobalId <= 0 {
		return 0, apperror.New(apperror.Internal, "video has no catalog global identity")
	}
	return v.GlobalId, nil
}

func (s *Service) Close(ctx context.Context) error {
	if s.details == nil {
		return nil
	}
	return s.details.Close(ctx)
}
