// Package media owns video browsing, favorites, and watch-history operations.
package media

import (
	"cczjVideo/app/db"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"fmt"
)

// Service provides media-facing application operations.
type Service struct{}

// NewService constructs a media service.
func NewService() *Service {
	return &Service{}
}

// List returns a filtered page of videos.
func (s *Service) List(req handler.VideoListReq) (*handler.VideoListResp, error) {
	return handler.GetVideoList(req)
}

// Detail returns one video and its episodes.
func (s *Service) Detail(req handler.VideoDetailReq) (*handler.VideoDetailResp, error) {
	return handler.GetVideoDetail(req)
}

// Search returns videos matching a keyword within a source.
func (s *Service) Search(req handler.VideoSearchReq) (*handler.VideoListResp, error) {
	return handler.SearchVideos(req)
}

// GlobalID returns the global identity for a source video.
func (s *Service) GlobalID(sourceKey, vodID string) (int64, error) {
	return db.GetGlobalIdForVideo(sourceKey, vodID)
}

// SourcesByGlobalID returns every source which has the global video.
func (s *Service) SourcesByGlobalID(globalID int64) ([]db.SourceVideoRef, error) {
	return db.FindSourcesByGlobalId(globalID)
}

// Types returns the video types available from a source.
func (s *Service) Types(req handler.GetTypesReq) ([]*model.VType, error) {
	return handler.GetTypes(req)
}

// Delete removes a source video and its associated records.
func (s *Service) Delete(req handler.DeleteVideoReq) error {
	return handler.DeleteVideo(req)
}

// YearsAndAreas returns filter values available for a source.
func (s *Service) YearsAndAreas(sourceKey string) (*handler.YearsResp, error) {
	return handler.GetYearsAndAreas(sourceKey)
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
	vodName, err := s.vodName(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.AddFavorite(sourceKey, vodID, vodName)
}

// RemoveFavorite removes the global favorite shared by matching sources.
func (s *Service) RemoveFavorite(sourceKey, vodID string) error {
	vodName, err := s.vodName(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.RemoveFavorite(vodName, sourceKey)
}

// IsFavorite reports whether the global video is favorited.
func (s *Service) IsFavorite(sourceKey, vodID string) (bool, error) {
	v, err := db.GetVideoById(sourceKey, vodID)
	if err != nil || v.GlobalId <= 0 {
		return false, nil
	}
	return db.IsFavoriteByGlobalID(int(v.GlobalId)), nil
}

// Favorites returns the requested page of global favorites.
func (s *Service) Favorites(page, pageSize int) ([]db.FavWithVideo, error) {
	return db.GetFavorites(page, pageSize)
}

// SaveHistory records progress for a source episode under its global video.
func (s *Service) SaveHistory(sourceKey, vodID string, epNum int, position float64) error {
	vodName, err := s.vodName(sourceKey, vodID)
	if err != nil {
		return err
	}
	return db.SaveWatchHistory(sourceKey, vodID, vodName, epNum, position)
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
	vodName, err := s.vodName(sourceKey, vodID)
	if err != nil {
		return err
	}
	row, err := db.GetGlobalVideoByName(vodName)
	if err != nil {
		return err
	}
	return db.DeleteHistoryItem(row.Id, epNum)
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
	vodName, err := s.vodName(sourceKey, vodID)
	if err != nil {
		return nil, err
	}
	return db.GetWatchedEpisodes(vodName)
}

func (s *Service) vodName(sourceKey, vodID string) (string, error) {
	v, err := db.GetVideoById(sourceKey, vodID)
	if err != nil {
		return "", fmt.Errorf("video not found: %w", err)
	}
	return v.VodName, nil
}
