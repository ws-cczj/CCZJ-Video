package service

import (
	"cczjVideo/app/library"
	"cczjVideo/app/model"
	"context"
)

func (a *App) NovelSourceDirectory(ctx context.Context) (*library.SourceDirectory, error) {
	return runRequest(a, ctx, "reading.NovelSourceDirectory", func(ctx context.Context) (*library.SourceDirectory, error) {
		return a.library.SourceDirectory(ctx, model.SourceKindNovel)
	})
}
func (a *App) ComicSourceDirectory(ctx context.Context) (*library.SourceDirectory, error) {
	return runRequest(a, ctx, "reading.ComicSourceDirectory", func(ctx context.Context) (*library.SourceDirectory, error) {
		return a.library.SourceDirectory(ctx, model.SourceKindComic)
	})
}
func (a *App) NovelSourceAlternatives(ctx context.Context, itemID int, cursor string) (*library.SearchOutcome, error) {
	return runRequest(a, ctx, "reading.NovelSourceAlternatives", func(ctx context.Context) (*library.SearchOutcome, error) {
		return a.library.SourceAlternatives(ctx, model.SourceKindNovel, itemID, cursor)
	})
}
func (a *App) ComicSourceAlternatives(ctx context.Context, itemID int, cursor string) (*library.SearchOutcome, error) {
	return runRequest(a, ctx, "reading.ComicSourceAlternatives", func(ctx context.Context) (*library.SearchOutcome, error) {
		return a.library.SourceAlternatives(ctx, model.SourceKindComic, itemID, cursor)
	})
}
func (a *App) NovelChapterMatch(ctx context.Context, itemID, chapterID, targetItemID int) (*library.ChapterMatch, error) {
	return runRequest(a, ctx, "reading.NovelChapterMatch", func(ctx context.Context) (*library.ChapterMatch, error) {
		return a.library.MatchChapter(ctx, model.SourceKindNovel, itemID, chapterID, targetItemID)
	})
}
func (a *App) ComicEpisodeMatch(ctx context.Context, itemID, chapterID, targetItemID int) (*library.ChapterMatch, error) {
	return runRequest(a, ctx, "reading.ComicEpisodeMatch", func(ctx context.Context) (*library.ChapterMatch, error) {
		return a.library.MatchChapter(ctx, model.SourceKindComic, itemID, chapterID, targetItemID)
	})
}
