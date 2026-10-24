package service

import (
	"cczjVideo/app/db"
	"context"
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

func (a *App) AddFavorite(ctx context.Context, req FavReq) error {
	return runRequestError(a, ctx, "video.AddFavorite", func(ctx context.Context) error { return a.media.AddFavorite(ctx, req.SourceKey, req.VodId) })
}

func (a *App) RemoveFavorite(ctx context.Context, req FavReq) error {
	return runRequestError(a, ctx, "video.RemoveFavorite", func(ctx context.Context) error { return a.media.RemoveFavorite(ctx, req.SourceKey, req.VodId) })
}

func (a *App) IsFavorite(ctx context.Context, req FavReq) (bool, error) {
	return runRequest(a, ctx, "video.IsFavorite", func(ctx context.Context) (bool, error) { return a.media.IsFavorite(ctx, req.SourceKey, req.VodId) })
}

func (a *App) GetFavorites(ctx context.Context, page, pageSize int) ([]db.FavWithVideo, error) {
	return runRequest(a, ctx, "video.GetFavorites", func(ctx context.Context) ([]db.FavWithVideo, error) { return a.media.Favorites(ctx, page, pageSize) })
}
