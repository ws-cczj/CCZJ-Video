package db

import (
	"context"
	"database/sql"
	"errors"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

// BackupWriter owns one restore transaction, including identity and reading-state writes.
type BackupWriter struct {
	ctx context.Context
	tx  *sqlx.Tx
}

// WriteBackup serializes identity changes and commits every restored category together.
func WriteBackup(ctx context.Context, database *sqlx.DB, work func(*BackupWriter) error) error {
	release, err := ownCatalogIdentityWrite(ctx)
	if err != nil {
		return err
	}
	defer release()
	tx, err := database.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := work(&BackupWriter{ctx, tx}); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	catalogIdentityRevision.Add(1)
	return nil
}

func (w *BackupWriter) Source(key string) (*model.Source, error) {
	var row model.Source
	if err := w.tx.GetContext(w.ctx, &row, `SELECT * FROM sources WHERE source_key=?`, key); err != nil {
		return nil, err
	}
	return &row, nil
}
func (w *BackupWriter) AddSource(source *model.Source) error {
	return insertSourceContext(w.ctx, w.tx, source)
}
func (w *BackupWriter) Setting(key string) (string, error) {
	var value string
	err := w.tx.GetContext(w.ctx, &value, `SELECT value FROM settings WHERE key=?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}
func (w *BackupWriter) SetSetting(key, value string) error {
	_, err := w.tx.ExecContext(w.ctx, `INSERT INTO settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}
func (w *BackupWriter) ResolveIdentities(items []BackupIdentity) ([]int64, error) {
	if len(items) == 0 {
		return nil, nil
	}
	candidates, err := loadGlobalCandidates(w.tx)
	if err != nil {
		return nil, err
	}
	index := newGlobalVideoIndex(candidates)
	ids := make([]int64, len(items))
	for i, item := range items {
		if err := w.ctx.Err(); err != nil {
			return nil, err
		}
		id, _, err := resolveGlobalVideoID(w.tx, index, item.VodName, item.Year, 0)
		if err != nil {
			return nil, err
		}
		ids[i] = id
	}
	return ids, nil
}
func (w *BackupWriter) MergeMeta(id int64, meta BackupVideoMeta) (bool, error) {
	return mergeBackupMeta(w.ctx, w.tx, id, meta)
}
func (w *BackupWriter) MergeFavorite(id int64, key, vodID, createdAt string) (bool, error) {
	return mergeBackupFavorite(w.ctx, w.tx, id, key, vodID, createdAt)
}
func (w *BackupWriter) MergeHistory(id int64, key, vodID string, episode int, position float64, updatedAt string) (bool, error) {
	return mergeBackupHistory(w.ctx, w.tx, id, key, vodID, episode, position, updatedAt)
}
func (w *BackupWriter) MergeReading(backup *BackupReading) (int, error) {
	return mergeReadingBackup(w.ctx, w.tx, backup)
}

// SourceKeys captures the restore's broad invalidation scope before commit.
func (w *BackupWriter) SourceKeys() ([]string, error) {
	keys := []string{}
	err := w.tx.SelectContext(w.ctx, &keys, `SELECT source_key FROM sources WHERE source_key!='' UNION SELECT source_key FROM source_videos WHERE source_key!='' ORDER BY source_key`)
	return keys, err
}
