package danmaku

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"cczjVideo/app/apperror"
)

const (
	noMatchCooldown        = 24 * time.Hour
	temporaryMatchCooldown = 30 * time.Second
)

func matchCooldown(code string, err error) time.Duration {
	if err != nil {
		if seconds := apperror.Public(err).RetryAfterSeconds; seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	switch code {
	case string(apperror.Timeout), string(apperror.Unavailable):
		return temporaryMatchCooldown
	case string(apperror.RateLimited):
		return time.Minute
	case "SEGMENT_NOT_FOUND", "AMBIGUOUS_WORK", "AMBIGUOUS_EPISODE", "EPISODE_NUMBER_UNKNOWN", "EPISODE_DURATION_MISMATCH", "EPISODE_VERSION_MISMATCH":
		return 5 * time.Minute
	default:
		return noMatchCooldown
	}
}

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
		legacy := strings.TrimPrefix(fingerprint, matchPolicy+":")
		if storedFingerprint == "episode-priority-v2:"+legacy {
			// The previous policy also cooled sources that had usable partial windows.
			if value.Code == "SEGMENT_NOT_FOUND" {
				return nil, nil
			}
			return value, nil
		}
		decision := strings.HasPrefix(value.Code, "WORK_") || strings.HasPrefix(value.Code, "EPISODE_") || strings.HasPrefix(value.Code, "AMBIGUOUS_") || value.Code == "NO_WORK_MATCH" || value.Code == "NO_EPISODE_MATCH" || value.Code == "NO_MATCH"
		if (storedFingerprint != legacy && storedFingerprint != "episode-priority-v1:"+legacy) || decision || value.Code == "SEGMENT_NOT_FOUND" {
			return nil, nil
		}
		if value.Code == string(apperror.Timeout) || value.Code == string(apperror.Unavailable) {
			// The old deadline was always recorded as exactly one day after failure.
			value.RetryAt = time.Unix(value.RetryAt, 0).Add(-noMatchCooldown + temporaryMatchCooldown).Unix()
			if value.RetryAt <= now.Unix() {
				return nil, nil
			}
		}
	}
	return value, nil
}
func (s *Store) saveMatchFailure(ctx context.Context, key, provider, fingerprint, code string, now, retryAt time.Time) error {
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
	_, err = tx.ExecContext(ctx, `INSERT INTO danmaku_match_failures(local_key,provider,fingerprint,code,retry_at)VALUES(?,?,?,?,?) ON CONFLICT(local_key,provider) DO UPDATE SET fingerprint=excluded.fingerprint,code=excluded.code,retry_at=excluded.retry_at`, key, provider, fingerprint, code, retryAt.Unix())
	if err != nil {
		return err
	}
	return tx.Commit()
}
