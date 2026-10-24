package service

import (
	"cczjVideo/app/applog"
	sourceservice "cczjVideo/app/source"

	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"context"
)

// ======================== Source ========================

func (a *App) GetAllSources(ctx context.Context) ([]*model.Source, error) {
	return runRequest(a, ctx, "source.GetAllSources", func(ctx context.Context) ([]*model.Source, error) {
		rows, err := sourceservice.GetAllSources()
		if err == nil {
			keys := make([]string, 0, len(rows))
			for _, row := range rows {
				if row != nil {
					keys = append(keys, row.SourceKey)
				}
			}
			operation.Stage(ctx, "sources_loaded", applog.Fields{"count": len(rows), "source_keys": keys})
		}
		return rows, err
	})
}

func (a *App) GetSourceStats(ctx context.Context) ([]model.SourceStat, error) {
	return runRequest(a, ctx, "source.GetSourceStats", sourceservice.GetSourceStatsContext)
}

func (a *App) AddSource(ctx context.Context, s *model.Source) error {
	_, err := runRequest(a, ctx, "source.AddSource", func(ctx context.Context) (bool, error) { return true, a.sources.Add(ctx, s) })
	return err
}

// CreateSource acknowledges the allocated identity and initial strategy in the
// same write, so callers never infer the created source from a later list read.
func (a *App) CreateSource(ctx context.Context, s *model.Source) (*model.Source, error) {
	return runRequest(a, ctx, "source.CreateSource", func(ctx context.Context) (*model.Source, error) {
		if err := a.sources.Add(ctx, s); err != nil {
			return nil, err
		}
		return s, nil
	})
}

func (a *App) UpdateSource(ctx context.Context, s *model.Source) error {
	_, err := runRequest(a, ctx, "source.UpdateSource", func(ctx context.Context) (bool, error) { return true, a.sources.Update(ctx, s) })
	return err
}

func (a *App) DeleteSource(ctx context.Context, key string) error {
	_, err := runRequest(a, ctx, "source.DeleteSource", func(ctx context.Context) (bool, error) { return true, a.sources.Delete(ctx, key) })
	return err
}
