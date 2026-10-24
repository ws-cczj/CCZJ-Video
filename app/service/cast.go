package service

import (
	"cczjVideo/app/cast"
	"context"
)

func (a *App) DiscoverCastDevices(ctx context.Context) ([]cast.Device, error) {
	return runRequest(a, ctx, "cast.DiscoverCastDevices", func(ctx context.Context) ([]cast.Device, error) { return a.cast.DiscoverContext(ctx) })
}
func (a *App) PlayCastVideo(ctx context.Context, id, url, title string) error {
	return runRequestError(a, ctx, "cast.PlayCastVideo", func(ctx context.Context) error { return a.cast.PlayContext(ctx, id, url, title) })
}
func (a *App) ControlCastVideo(ctx context.Context, id, action string, seconds float64) error {
	return runRequestError(a, ctx, "cast.ControlCastVideo", func(ctx context.Context) error { return a.cast.ControlContext(ctx, id, action, seconds) })
}
