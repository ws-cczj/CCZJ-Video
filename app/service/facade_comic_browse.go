package service

import (
	"cczjVideo/app/library"
	"cczjVideo/app/provider"
	"context"
)

func (a *App) ComicBrowseChannels() []provider.BrowseChannel { return a.library.ComicBrowseChannels() }

func (a *App) ComicBrowsePage(ctx context.Context, channel string, refresh bool) (*library.ComicBrowsePage, error) {
	return runRequest(a, ctx, "reading.ComicBrowsePage", func(ctx context.Context) (*library.ComicBrowsePage, error) {
		return a.library.BrowseComics(ctx, channel, refresh)
	})
}
