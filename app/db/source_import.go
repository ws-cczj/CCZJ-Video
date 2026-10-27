package db

import (
	"context"
	"database/sql"
	"errors"

	"cczjVideo/app/model"
)

// ImportSourceBundle keeps configuration, category mappings and catalog rows
// in one transaction. The domain supplies only its pure configuration merge.
func ImportSourceBundle(ctx context.Context, key string, prepare func(*model.Source) (*model.Source, error), videos []*model.Video, types []SourceTypeExport) error {
	release, err := ownCatalogIdentityWrite(ctx)
	if err != nil {
		return err
	}
	defer release()
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing := &model.Source{}
	err = tx.GetContext(ctx, existing, `SELECT * FROM sources WHERE source_key=?`, key)
	missing := errors.Is(err, sql.ErrNoRows)
	if err != nil && !missing {
		return err
	}
	if missing {
		existing = nil
	}
	desired, err := prepare(existing)
	if err != nil {
		return err
	}
	if err := ensureUniqueVideoEndpoint(ctx, tx, desired, !missing); err != nil {
		return err
	}
	if missing {
		err = insertSourceContext(ctx, tx, desired)
	} else {
		err = updateSourceContext(ctx, tx, desired, false)
	}
	if err != nil {
		return err
	}
	if err := writeSourceTypes(ctx, tx, key, types); err != nil {
		return err
	}
	selection, err := selectSourceCollectVideos(ctx, tx, key, videos)
	if err != nil {
		return err
	}
	if len(selection.Videos) > 0 {
		resolver, err := newCatalogIdentityResolver(tx)
		if err != nil {
			return err
		}
		if err := writeCatalogProjection(ctx, tx, resolver, key, selection.Videos, true); err != nil {
			return err
		}
	}
	// Explicit mappings override automatically inferred mappings in the projection.
	if err := writeSourceTypes(ctx, tx, key, types); err != nil {
		return err
	}
	return tx.Commit()
}
