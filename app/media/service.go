// Package media owns video identity, favorites, and watch-history behavior.
package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"database/sql"
	"errors"
)

// Service provides media-facing application operations.
type Service struct {
	lookup func(sourceKey, vodID string) (*model.Video, error)
}

// NewService constructs a media service.
func NewService(lookup func(sourceKey, vodID string) (*model.Video, error)) *Service {
	return &Service{lookup: lookup}
}

// Recommend returns recommended videos after normalizing optional arguments.
func (s *Service) Recommend(sourceKey string, limit int, excludeIDs []string) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 8
	}
	if excludeIDs == nil {
		excludeIDs = []string{}
	}
	return handler.GetRecommend(sourceKey, limit, excludeIDs)
}

// Similar returns videos sharing a type with the requested video.
func (s *Service) Similar(sourceKey, typeID string, limit int, excludeIDs []string) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 8
	}
	if excludeIDs == nil {
		excludeIDs = []string{}
	}
	return handler.GetSimilarVideos(sourceKey, typeID, limit, excludeIDs)
}

// AddFavorite creates a global favorite. The favorite is shared by all sources
// with the same global_id.
func (s *Service) AddFavorite(sourceKey, vodID string) error {
	globalID, err := s.catalogGlobalID(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.AddFavoriteByIdentity(globalID, sourceKey, vodID)
}

// RemoveFavorite removes the global favorite shared by matching sources.
func (s *Service) RemoveFavorite(sourceKey, vodID string) error {
	globalID, err := s.catalogGlobalID(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.RemoveFavoriteByGlobalID(int(globalID), sourceKey)
}

// IsFavorite reports whether the global video is favorited.
func (s *Service) IsFavorite(sourceKey, vodID string) (bool, error) {
	v, err := s.lookup(sourceKey, vodID)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, apperror.Wrap(apperror.Storage, err, "read favorite identity")
	}
	if v == nil || v.GlobalId <= 0 {
		return false, apperror.New(apperror.Internal, "video has no catalog global identity")
	}
	return db.IsFavoriteByGlobalID(int(v.GlobalId)), nil
}

// Favorites returns the requested page of global favorites.
func (s *Service) Favorites(page, pageSize int) ([]db.FavWithVideo, error) {
	return db.GetFavorites(page, pageSize)
}

// SaveHistory records progress for a source episode under its global video.
func (s *Service) SaveHistory(sourceKey, vodID string, epNum int, position float64) error {
	globalID, err := s.catalogGlobalID(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.SaveWatchHistoryByIdentity(globalID, sourceKey, vodID, epNum, position)
}

// HistoryPosition returns the last watched position (seconds) of one episode.
// A missing record is not an error: 0 means "no history yet".
func (s *Service) HistoryPosition(sourceKey, vodID string, epNum int) (float64, error) {
	globalID, err := s.catalogGlobalID(sourceKey, vodID)
	if err != nil {
		return 0, err
	}
	position, err := db.GetWatchHistoryByIdentity(globalID, sourceKey, vodID, epNum)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return position, nil
}

// RecentHistory returns hydrated recent watch-history records.
func (s *Service) RecentHistory(limit int) ([]*handler.HistoryItemWithVideo, error) {
	if limit <= 0 {
		limit = 20
	}
	raw, err := db.GetRecentHistory(limit)
	if err != nil {
		return nil, err
	}
	return handler.HydrateHistory("", raw), nil
}

// DeleteHistoryItem removes a watched episode from its global history.
func (s *Service) DeleteHistoryItem(sourceKey, vodID string, epNum int) error {
	globalID, err := s.catalogGlobalID(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.DeleteHistoryItemByIdentity(globalID, sourceKey, vodID, epNum)
}

// DeleteHistoryByVideo removes source-specific history for compatibility.
func (s *Service) DeleteHistoryByVideo(sourceKey, vodID string) error {
	return db.DeleteHistoryByVideo(sourceKey, vodID)
}

// ClearHistory removes every watch-history record.
func (s *Service) ClearHistory() (int, error) {
	n, err := db.ClearAllHistory()
	return int(n), err
}

// WatchedEpisodes returns watched episode numbers for the global video.
func (s *Service) WatchedEpisodes(sourceKey, vodID string) ([]int, error) {
	globalID, err := s.catalogGlobalID(sourceKey, vodID)
	if err != nil {
		return nil, err
	}
	return db.GetWatchedEpisodesByIdentity(globalID, sourceKey, vodID)
}

func (s *Service) catalogGlobalID(sourceKey, vodID string) (int64, error) {
	v, err := s.lookup(sourceKey, vodID)
	if err != nil {
		return 0, apperror.Wrap(apperror.Storage, err, "read video identity")
	}
	if v == nil || v.GlobalId <= 0 {
		return 0, apperror.New(apperror.Internal, "video has no catalog global identity")
	}
	return v.GlobalId, nil
}
