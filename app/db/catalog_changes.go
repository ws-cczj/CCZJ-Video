package db

import (
	"context"
	"database/sql"
	"errors"

	"github.com/jmoiron/sqlx"
)

// Catalog retirements distinguish an explicitly purged coordinate from a
// legacy favorite/history entry which never had a local catalog projection.
// They retain no title, media URL or user progress. Reimport removes the marker.
const catalogRetirementsDDL = `CREATE TABLE IF NOT EXISTS catalog_retirements (
 source_key TEXT NOT NULL, source_vod_id TEXT NOT NULL,
 retired_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP,
 PRIMARY KEY(source_key, source_vod_id)
)`

func migrateCatalogRetirements(tx *sqlx.Tx) error {
	_, err := tx.Exec(catalogRetirementsDDL)
	return err
}

// Preserve coordinates before physical deletion so legacy user-record fallback
// does not mistake an explicitly removed projection for a never-collected item.
func rememberCatalogRetirements(ctx context.Context, tx *sqlx.Tx, where string, args ...any) error {
	_, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO catalog_retirements(source_key,source_vod_id) SELECT source_key,source_vod_id FROM source_videos WHERE `+where, args...)
	return err
}

// CatalogChange describes only committed mutations. The invalidation publisher
// consumes this result instead of querying a separate, racy pre-write snapshot.
type CatalogChange struct {
	Affected         int
	SourceKey, VodID string
	GlobalID         int64
	Sources          []string
}

func (s *MediaStore) changeCatalog(ctx context.Context, sourceKey, vodID string, action string) (change CatalogChange, err error) {
	database, err := s.open(ctx)
	if err != nil {
		return change, err
	}
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return change, err
	}
	defer tx.Rollback()
	var row struct {
		GlobalID int64  `db:"global_id"`
		State    string `db:"lifecycle_state"`
	}
	if err = tx.GetContext(ctx, &row, `SELECT global_id,lifecycle_state FROM source_videos WHERE source_key=? AND source_vod_id=?`, sourceKey, vodID); err != nil {
		return change, err
	}
	change = CatalogChange{SourceKey: sourceKey, VodID: vodID, GlobalID: row.GlobalID}
	if action == "delete" && row.State == "deleted" {
		return change, nil
	}
	if action != "delete" && row.State != "deleted" {
		return change, sql.ErrNoRows
	}
	var result sql.Result
	switch action {
	case "delete":
		result, err = tx.ExecContext(ctx, `UPDATE source_videos SET lifecycle_state='deleted',updated_at=CURRENT_TIMESTAMP WHERE source_key=? AND source_vod_id=? AND lifecycle_state='active'`, sourceKey, vodID)
	case "restore":
		result, err = tx.ExecContext(ctx, `UPDATE source_videos SET lifecycle_state='active',updated_at=CURRENT_TIMESTAMP WHERE source_key=? AND source_vod_id=? AND lifecycle_state='deleted'`, sourceKey, vodID)
	case "purge":
		if _, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO catalog_retirements(source_key,source_vod_id) VALUES(?,?)`, sourceKey, vodID); err != nil {
			return CatalogChange{}, err
		}
		result, err = tx.ExecContext(ctx, `DELETE FROM source_videos WHERE source_key=? AND source_vod_id=? AND lifecycle_state='deleted'`, sourceKey, vodID)
	default:
		return CatalogChange{}, errors.New("unknown catalog action")
	}
	if err != nil {
		return CatalogChange{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return CatalogChange{}, err
	}
	if n == 0 {
		return CatalogChange{}, sql.ErrNoRows
	}
	if err = tx.Commit(); err != nil {
		return CatalogChange{}, err
	}
	change.Affected = int(n)
	return change, nil
}

func (s *MediaStore) DeleteCatalog(ctx context.Context, key, vod string) (CatalogChange, error) {
	return s.changeCatalog(ctx, key, vod, "delete")
}
func (s *MediaStore) RestoreCatalog(ctx context.Context, key, vod string) (CatalogChange, error) {
	return s.changeCatalog(ctx, key, vod, "restore")
}
func (s *MediaStore) PurgeCatalog(ctx context.Context, key, vod string) (CatalogChange, error) {
	return s.changeCatalog(ctx, key, vod, "purge")
}

func (s *MediaStore) ClearCatalogRecycleBin(ctx context.Context, key string) (CatalogChange, error) {
	database, err := s.open(ctx)
	if err != nil {
		return CatalogChange{}, err
	}
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return CatalogChange{}, err
	}
	defer tx.Rollback()
	where, args := `lifecycle_state='deleted'`, []any{}
	if key != "" {
		where += ` AND source_key=?`
		args = append(args, key)
	}
	change := CatalogChange{}
	if err = tx.SelectContext(ctx, &change.Sources, `SELECT DISTINCT source_key FROM source_videos WHERE `+where, args...); err != nil {
		return CatalogChange{}, err
	}
	if err = rememberCatalogRetirements(ctx, tx, where, args...); err != nil {
		return CatalogChange{}, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM source_videos WHERE `+where, args...)
	if err != nil {
		return CatalogChange{}, err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return CatalogChange{}, err
	}
	if err = tx.Commit(); err != nil {
		return CatalogChange{}, err
	}
	change.Affected = int(n)
	return change, nil
}
