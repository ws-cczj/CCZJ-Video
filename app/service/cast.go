package service

import (
	"cczjVideo/app/cast"
	"context"
)

func (a *App) DiscoverCastDevices(ctx context.Context) ([]cast.Device, error) {
	return runRequest(a, ctx, "cast.DiscoverCastDevices", func(ctx context.Context) ([]cast.Device, error) { return a.cast.DiscoverContext(ctx) })
}
func (a *App) PlayCastVideo(id, url, title string) error { return a.cast.Play(id, url, title) }
func (a *App) ControlCastVideo(id, action string, seconds float64) error {
	return a.cast.Control(id, action, seconds)
}
