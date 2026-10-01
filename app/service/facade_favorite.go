package service

import (
	"cczjVideo/app/db"
	"encoding/json"
)

// ======================== Favorites ========================

type FavReq struct {
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
	GlobalId  int    `json:"global_id"`
}

func (r *FavReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
		GlobalId  int             `json:"global_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.VodId = normalizeId(raw.VodId)
	r.GlobalId = raw.GlobalId
	return nil
}

func (a *App) AddFavorite(req FavReq) error {
	return a.media.AddFavorite(req.SourceKey, req.VodId)
}

func (a *App) RemoveFavorite(req FavReq) error {
	return a.media.RemoveFavorite(req.SourceKey, req.VodId)
}

func (a *App) IsFavorite(req FavReq) (bool, error) {
	return a.media.IsFavorite(req.SourceKey, req.VodId)
}

func (a *App) GetFavorites(page, pageSize int) ([]db.FavWithVideo, error) {
	return a.media.Favorites(page, pageSize)
}
