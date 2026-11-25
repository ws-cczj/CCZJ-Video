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
	tvIndexFingerprintSuffix = ":tv-index"
	legacyDayCooldown        = 24 * time.Hour
	temporaryMatchCooldown   = 30 * time.Second
	noMatchCooldown          = 5 * time.Minute
	emptyPoolCooldown        = 30 * time.Minute
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
	case "EMPTY_POOL":
		return emptyPoolCooldown
	default:
		return noMatchCooldown
	}
}

// EpisodeMatchSchema is the failure ledger introduced by migration 17.
var EpisodeMatchSchema = []string{
	`CREATE TABLE IF NOT EXISTS danmaku_match_failures(local_key TEXT NOT NULL,provider TEXT NOT NULL,fingerprint TEXT NOT NULL,code TEXT NOT NULL,retry_at INTEGER NOT NULL,PRIMARY KEY(local_key,provider))`,
	`CREATE INDEX IF NOT EXISTS idx_danmaku_match_retry ON danmaku_match_failures(retry_at)`,
}

// SearchMatchSchema adds work-level search history in migration 27.
var SearchMatchSchema = []string{
	`CREATE TABLE IF NOT EXISTS danmaku_search_failures(work_key TEXT NOT NULL,provider TEXT NOT NULL,code TEXT NOT NULL,failures INTEGER NOT NULL,retry_at INTEGER NOT NULL,failed_at INTEGER NOT NULL,PRIMARY KEY(work_key,provider))`,
	`CREATE INDEX IF NOT EXISTS idx_danmaku_search_failed_at ON danmaku_search_failures(failed_at)`,
}

// MatchSchema initializes both matching ledgers in new and isolated stores.
var MatchSchema = append(append([]string{}, EpisodeMatchSchema...), SearchMatchSchema...)

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
	if storedFingerprint != fingerprint && strings.HasSuffix(fingerprint, tvIndexFingerprintSuffix) {
		// Only explicit empty keyword results need rechecking through the year
		// index. Older generic failures can still be explained from cached data.
		if value.Code == "WORK_OFFICIAL_MISSING" || value.Code == "WORK_SEARCH_EMPTY" {
			return nil, nil
		}
		fingerprint = strings.TrimSuffix(fingerprint, tvIndexFingerprintSuffix)
	}
	if storedFingerprint != fingerprint {
		legacy := strings.TrimPrefix(fingerprint, matchPolicy+":")
		switch storedFingerprint {
		case "episode-priority-v2:" + legacy, "episode-priority-v3:" + legacy:
			// The previous policy also cooled sources that had usable partial windows.
			if storedFingerprint == "episode-priority-v2:"+legacy && value.Code == "SEGMENT_NOT_FOUND" {
				return nil, nil
			}
			switch value.Code {
			case string(apperror.Timeout), string(apperror.Unavailable), string(apperror.RateLimited),
				"WORK_OFFICIAL_MISSING", "SEGMENT_NOT_FOUND", "AMBIGUOUS_WORK", "AMBIGUOUS_EPISODE",
				"EPISODE_NUMBER_UNKNOWN", "EPISODE_DURATION_MISMATCH", "EPISODE_VERSION_MISMATCH":
				// These reasons already had individual deadlines.
			default:
				value.RetryAt -= int64((legacyDayCooldown - matchCooldown(value.Code, nil)) / time.Second)
			}
		case legacy, "episode-priority-v1:" + legacy:
			decision := strings.HasPrefix(value.Code, "WORK_") || strings.HasPrefix(value.Code, "EPISODE_") || strings.HasPrefix(value.Code, "AMBIGUOUS_") || value.Code == "NO_WORK_MATCH" || value.Code == "NO_EPISODE_MATCH" || value.Code == "NO_MATCH"
			if decision || value.Code == "SEGMENT_NOT_FOUND" {
				return nil, nil
			}
			if value.Code != string(apperror.RateLimited) {
				value.RetryAt -= int64((legacyDayCooldown - matchCooldown(value.Code, nil)) / time.Second)
			}
		default:
			return nil, nil
		}
		if value.RetryAt <= now.Unix() {
			return nil, nil
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
