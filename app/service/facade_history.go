package service

import (
	"cczjVideo/app/media"
	"context"
	"encoding/json"
)

// ======================== Watch History ========================

type HistoryReq struct {
	SourceKey string  `json:"source_key"`
	VodId     string  `json:"vod_id"`
	EpNum     int     `json:"ep_num"`
	Position  float64 `json:"position"`
	Duration  float64 `json:"duration"`
}

func (r *HistoryReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
		EpNum     json.RawMessage `json:"ep_num"`
		Position  json.RawMessage `json:"position"`
		Duration  json.RawMessage `json:"duration"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.VodId = normalizeId(raw.VodId)
	if v, err := parseInt(raw.EpNum, 1); err == nil {
		r.EpNum = v
	}
	if v, err := parseFloat(raw.Position, 0); err == nil {
		r.Position = v
	}
	if v, err := parseFloat(raw.Duration, 0); err == nil {
		r.Duration = v
	}
	return nil
}

func (a *App) SaveWatchHistory(ctx context.Context, req HistoryReq) error {
	return runRequestError(a, ctx, "video.SaveWatchHistory", func(ctx context.Context) error {
		return a.media.SaveHistory(ctx, req.SourceKey, req.VodId, req.EpNum, req.Position, req.Duration)
	})
}

func (a *App) GetRecentHistory(ctx context.Context, limit int) ([]*media.HistoryItemWithVideo, error) {
	return runRequest(a, ctx, "video.GetRecentHistory", func(ctx context.Context) ([]*media.HistoryItemWithVideo, error) {
		return a.media.RecentHistory(ctx, limit)
	})
}

// GetHistoryPosition 返回某集上次播放到的位置（秒），无记录时为 0。
func (a *App) GetHistoryPosition(ctx context.Context, req HistoryReq) (float64, error) {
	return runRequest(a, ctx, "video.GetHistoryPosition", func(ctx context.Context) (float64, error) {
		return a.media.HistoryPosition(ctx, req.SourceKey, req.VodId, req.EpNum)
	})
}

// DeleteHistoryItem 删除单条观看历史
func (a *App) DeleteHistoryItem(ctx context.Context, req HistoryReq) error {
	return runRequestError(a, ctx, "video.DeleteHistoryItem", func(ctx context.Context) error {
		return a.media.DeleteHistoryItem(ctx, req.SourceKey, req.VodId, req.EpNum)
	})
}

// DeleteHistoryByVideo 删除某个视频的全部观看历史
func (a *App) DeleteHistoryByVideo(ctx context.Context, req FavReq) error {
	return runRequestError(a, ctx, "video.DeleteHistoryByVideo", func(ctx context.Context) error { return a.media.DeleteHistoryByVideo(ctx, req.SourceKey, req.VodId) })
}

// ClearAllHistory 清空全部观看历史，返回删除的条数
func (a *App) ClearAllHistory(ctx context.Context) (int, error) {
	return runRequest(a, ctx, "video.ClearAllHistory", func(ctx context.Context) (int, error) { return a.media.ClearHistory(ctx) })
}

// GetWatchedEpisodes 返回指定视频已观看的所有集数
func (a *App) GetWatchedEpisodes(ctx context.Context, req FavReq) ([]int, error) {
	return runRequest(a, ctx, "video.GetWatchedEpisodes", func(ctx context.Context) ([]int, error) {
		return a.media.WatchedEpisodes(ctx, req.SourceKey, req.VodId)
	})
}
