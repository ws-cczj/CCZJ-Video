package db

import (
	"context"
	"database/sql"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

// BackupSnapshot is one coherent read of portable records and their references.
type BackupSnapshot struct {
	Settings  []BackupSetting
	Sources   []*model.Source
	Videos    []BackupVideoMeta
	Favorites []BackupFavorite
	History   []BackupHistory
	Reading   *BackupReading
}

func ReadBackupSnapshot(ctx context.Context, database *sqlx.DB) (result BackupSnapshot, err error) {
	tx, err := database.BeginTxx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if result.Settings, err = ReadAllSettingsContext(ctx, tx); err != nil {
		return result, err
	}
	if result.Sources, err = ReadSourcesContext(ctx, tx); err != nil {
		return result, err
	}
	if result.Favorites, err = ReadFavoritesContext(ctx, tx); err != nil {
		return result, err
	}
	if result.History, err = ReadHistoryContext(ctx, tx); err != nil {
		return result, err
	}
	if result.Videos, err = ReadLinkedVideoMetaContext(ctx, tx); err != nil {
		return result, err
	}
	var readingTables int
	if err := tx.GetContext(ctx, &readingTables, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='reading_state'`); err != nil {
		return result, err
	}
	if readingTables > 0 {
		if result.Reading, err = readReadingBackup(ctx, tx); err != nil {
			return result, err
		}
	}
	return result, tx.Commit()
}
