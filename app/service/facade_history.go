package service

import (
	"cczjVideo/app/handler"
	"encoding/json"
)

// ======================== Watch History ========================

type HistoryReq struct {
	SourceKey string  `json:"source_key"`
	VodId     string  `json:"vod_id"`
	EpNum     int     `json:"ep_num"`
	Position  float64 `json:"position"`
}

func (r *HistoryReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
		EpNum     json.RawMessage `json:"ep_num"`
		Position  json.RawMessage `json:"position"`
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
	return nil
}

func (a *App) SaveWatchHistory(req HistoryReq) error {
	return a.media.SaveHistory(req.SourceKey, req.VodId, req.EpNum, req.Position)
}

func (a *App) GetRecentHistory(limit int) ([]*handler.HistoryItemWithVideo, error) {
	return a.media.RecentHistory(limit)
}

// GetHistoryPosition 返回某集上次播放到的位置（秒），无记录时为 0。
func (a *App) GetHistoryPosition(req HistoryReq) (float64, error) {
	return a.media.HistoryPosition(req.SourceKey, req.VodId, req.EpNum)
}

// DeleteHistoryItem 删除单条观看历史
func (a *App) DeleteHistoryItem(req HistoryReq) error {
	return a.media.DeleteHistoryItem(req.SourceKey, req.VodId, req.EpNum)
}

// DeleteHistoryByVideo 删除某个视频的全部观看历史
func (a *App) DeleteHistoryByVideo(req FavReq) error {
	return a.media.DeleteHistoryByVideo(req.SourceKey, req.VodId)
}

// ClearAllHistory 清空全部观看历史，返回删除的条数
func (a *App) ClearAllHistory() (int, error) {
	return a.media.ClearHistory()
}

// GetWatchedEpisodes 返回指定视频已观看的所有集数
func (a *App) GetWatchedEpisodes(req FavReq) ([]int, error) {
	return a.media.WatchedEpisodes(req.SourceKey, req.VodId)
}
