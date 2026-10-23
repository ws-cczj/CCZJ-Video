package danmaku

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// MatchSchema is applied by migration 17, separately from the original v15 schema.
var MatchSchema = []string{
	`CREATE TABLE IF NOT EXISTS danmaku_match_failures(local_key TEXT NOT NULL,provider TEXT NOT NULL,fingerprint TEXT NOT NULL,code TEXT NOT NULL,retry_at INTEGER NOT NULL,PRIMARY KEY(local_key,provider))`,
	`CREATE INDEX IF NOT EXISTS idx_danmaku_match_retry ON danmaku_match_failures(retry_at)`,
}

func (s *Store) matchFailure(ctx context.Context, key, provider, fingerprint string, now time.Time) (*MatchAttempt, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	value := &MatchAttempt{Provider: provider, Status: "cooling_down"}
	var storedFingerprint string
	err = db.QueryRowContext(ctx, `SELECT code,retry_at,fingerprint FROM danmaku_match_failures WHERE local_key=? AND provider=? AND retry_at>?`, key, provider, now.Unix()).Scan(&value.Code, &value.RetryAt, &storedFingerprint)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if storedFingerprint != fingerprint {
		// A changed matching policy can reevaluate a derived rejection using
		// cached metadata. It must not erase the old network/empty-pool cooldown.
		legacy := strings.TrimPrefix(fingerprint, matchPolicy+":")
		decision := strings.HasPrefix(value.Code, "WORK_") || strings.HasPrefix(value.Code, "EPISODE_") || strings.HasPrefix(value.Code, "AMBIGUOUS_") || value.Code == "NO_WORK_MATCH" || value.Code == "NO_EPISODE_MATCH" || value.Code == "NO_MATCH"
		if storedFingerprint != legacy || decision {
			return nil, nil
		}
	}
	return value, nil
}
func (s *Store) saveMatchFailure(ctx context.Context, key, provider, fingerprint, code string, now time.Time) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM danmaku_match_failures WHERE retry_at<=?`, now.Unix()); err != nil {
		return err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM danmaku_match_failures`).Scan(&count); err != nil {
		return err
	}
	if count >= 50000 {
		var exists int
		if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM danmaku_match_failures WHERE local_key=? AND provider=?`, key, provider).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return errors.New("danmaku failure ledger capacity reached")
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO danmaku_match_failures(local_key,provider,fingerprint,code,retry_at)VALUES(?,?,?,?,?) ON CONFLICT(local_key,provider) DO UPDATE SET fingerprint=excluded.fingerprint,code=excluded.code,retry_at=excluded.retry_at`, key, provider, fingerprint, code, now.Add(24*time.Hour).Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}
