package service

import (
	"cczjVideo/app/library"
	"cczjVideo/app/model"
	"context"
)

func (a *App) LibraryPreviewSources(ctx context.Context, kind, input, origin string) (*library.SourcePreview, error) {
	return runRequest(a, ctx, "reading.LibraryPreviewSources", func(ctx context.Context) (*library.SourcePreview, error) {
		return a.library.PreviewSources(ctx, model.SourceKind(kind), input, origin)
	})
}

func (a *App) LibraryCheckSourceCandidate(ctx context.Context, previewID, id, keyword string) (*library.SourceCandidate, error) {
	return runRequest(a, ctx, "reading.LibraryCheckSourceCandidate", func(ctx context.Context) (*library.SourceCandidate, error) {
		return a.library.CheckSourceCandidate(ctx, previewID, id, keyword)
	})
}

func (a *App) LibraryImportSources(ctx context.Context, previewID string, ids []string) ([]*library.SourceImportResult, error) {
	return runRequest(a, ctx, "reading.LibraryImportSources", func(ctx context.Context) ([]*library.SourceImportResult, error) {
		return a.library.ImportSourceCandidates(ctx, previewID, ids)
	})
}
