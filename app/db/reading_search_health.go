package db

import (
	"context"
	"fmt"
	"time"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

func migrateReadingSearchHealth(tx *sqlx.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS reading_search_health (
	 kind TEXT NOT NULL CHECK(kind IN ('novel','comic')), source_id TEXT NOT NULL,
	 checked_at INTEGER NOT NULL, error_code TEXT NOT NULL DEFAULT '',
	 failures INTEGER NOT NULL DEFAULT 0, retry_after INTEGER NOT NULL DEFAULT 0,
	 PRIMARY KEY(kind,source_id))`)
	return err
}

// Search observations never replace chapter/page quality scores or advance the
// maintenance clock. SourceID includes the adapter configuration revision.
type ReadingSearchHealth struct {
	SourceID   string `db:"source_id"`
	CheckedAt  int64  `db:"checked_at"`
	ErrorCode  string `db:"error_code"`
	Failures   int    `db:"failures"`
	RetryAfter int64  `db:"retry_after"`
}

func ReadReadingSearchHealth(ctx context.Context, kind model.SourceKind) (map[string]ReadingSearchHealth, error) {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return nil, fmt.Errorf("invalid reading mode")
	}
	var rows []ReadingSearchHealth
	err := instance.SelectContext(ctx, &rows, `SELECT source_id,checked_at,error_code,failures,retry_after FROM reading_search_health WHERE kind=?`, kind)
	out := make(map[string]ReadingSearchHealth, len(rows))
	for _, row := range rows {
		out[row.SourceID] = row
	}
	return out, err
}

func RecordReadingSearchHealth(ctx context.Context, kind model.SourceKind, sourceID, code string) error {
	if (kind != model.SourceKindNovel && kind != model.SourceKindComic) || sourceID == "" {
		return fmt.Errorf("invalid reading search observation")
	}
	now := time.Now().Unix()
	_, err := instance.ExecContext(ctx, `INSERT INTO reading_search_health(kind,source_id,checked_at,error_code,failures,retry_after)
	 VALUES(?,?,?,?,CASE WHEN ?='' THEN 0 ELSE 1 END,CASE WHEN ?='' THEN 0 ELSE ?+60 END)
	 ON CONFLICT(kind,source_id) DO UPDATE SET checked_at=excluded.checked_at,error_code=excluded.error_code,
	 failures=CASE WHEN excluded.error_code='' THEN 0 ELSE MIN(reading_search_health.failures+1,100) END,
	 retry_after=CASE WHEN excluded.error_code='' THEN 0 ELSE excluded.checked_at+
	 CASE WHEN reading_search_health.failures=0 THEN 60 WHEN reading_search_health.failures=1 THEN 300
	 WHEN reading_search_health.failures=2 THEN 1800 ELSE 7200 END END`,
		kind, sourceID, now, code, code, code, now)
	return err
}
