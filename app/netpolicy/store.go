package netpolicy

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) loadActive(ctx context.Context, now time.Time) (map[string]time.Time, error) {
	database, err := s.db()
	if err != nil {
		return nil, err
	}
	rows, err := database.QueryContext(ctx, `SELECT request_key,next_at FROM network_request_limits WHERE next_at>? ORDER BY CASE WHEN request_key LIKE 'host:%' THEN 0 ELSE 1 END,next_at LIMIT ?`, now.UnixMilli(), maxRequestKeys+512)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]time.Time{}
	for rows.Next() {
		var key string
		var next int64
		if err = rows.Scan(&key, &next); err != nil {
			return nil, err
		}
		out[key] = time.UnixMilli(next)
	}
	return out, rows.Err()
}

// Schema is separate so migration v16 can update existing v15 stores.
var Schema = []string{
	`CREATE TABLE IF NOT EXISTS network_request_limits(request_key TEXT PRIMARY KEY,next_at INTEGER NOT NULL)`,
	`CREATE INDEX IF NOT EXISTS idx_network_request_next ON network_request_limits(next_at)`,
}

type Store struct{ database func() *sql.DB }

func NewStore(database func() *sql.DB) *Store { return &Store{database: database} }
func (s *Store) db() (*sql.DB, error) {
	database := s.database()
	if database == nil {
		return nil, sql.ErrConnDone
	}
	return database, nil
}

const maxRequestKeys = 4096

func deadlineMillis(next time.Time) int64 {
	value := next.UnixMilli()
	if !next.Equal(time.UnixMilli(value)) {
		value++
	}
	return value
}

// Reservation survives process restarts; a failed or cancelled attempt still
// consumes its slot because the upstream may already have received the request.
func (s *Store) reserveRequest(ctx context.Context, key, host string, now, next time.Time) (time.Time, bool, error) {
	database, err := s.db()
	if err != nil {
		return time.Time{}, false, err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, false, err
	}
	defer tx.Rollback()
	var until int64
	if err = tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(next_at),0) FROM network_request_limits WHERE request_key IN (?,?)`, key, host).Scan(&until); err != nil {
		return time.Time{}, false, err
	}
	if until > now.UnixMilli() {
		return time.UnixMilli(until), false, nil
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM network_request_limits WHERE next_at<=?`, now.UnixMilli()); err != nil {
		return time.Time{}, false, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM network_request_limits`).Scan(&count); err != nil {
		return time.Time{}, false, err
	}
	if count >= maxRequestKeys {
		if err = tx.QueryRowContext(ctx, `SELECT MIN(next_at) FROM network_request_limits`).Scan(&until); err != nil {
			return time.Time{}, false, err
		}
		return time.UnixMilli(until), false, nil
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO network_request_limits(request_key,next_at) VALUES(?,?)`, key, deadlineMillis(next)); err != nil {
		return time.Time{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return time.Time{}, false, err
	}
	return time.UnixMilli(deadlineMillis(next)), true, nil
}

func (s *Store) deferRequestHost(ctx context.Context, host string, now, next time.Time) error {
	database, err := s.db()
	if err != nil {
		return err
	}
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM network_request_limits WHERE next_at<=?`, now.UnixMilli()); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM network_request_limits WHERE request_key<>?`, host).Scan(&count); err != nil {
		return err
	}
	if count >= maxRequestKeys+512 {
		return fmt.Errorf("network cooldown ledger capacity reached")
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO network_request_limits(request_key,next_at) VALUES(?,?) ON CONFLICT(request_key) DO UPDATE SET next_at=MAX(next_at,excluded.next_at)`, host, deadlineMillis(next)); err != nil {
		return err
	}
	return tx.Commit()
}
