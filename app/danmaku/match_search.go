package danmaku

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"cczjVideo/app/apperror"
)

const (
	searchFailureRetention = 24 * time.Hour
	searchFailureLimit     = 10000
)

var searchBackoff = [...]time.Duration{noMatchCooldown, 15 * time.Minute, 30 * time.Minute}

type workSearchResult struct {
	rows    []Candidate
	work    *Candidate
	attempt MatchAttempt
}

func (s *Service) matchSearch(ctx context.Context, request MatchRequest, provider string) (workSearchResult, error) {
	key := workIdentity(request)
	trace, _ := ctx.Value(matchTraceKey{}).(string)
	return s.searchMatches.Do(ctx, provider+":"+key, func(ctx context.Context) (result workSearchResult, err error) {
		prior, err := s.store.searchFailure(ctx, key, provider)
		if err != nil {
			return result, apperror.Wrap(apperror.Storage, err, "读取弹幕搜索冷却失败")
		}
		if prior.RetryAt > s.now().Unix() {
			prior.Status, prior.Stage = "cooling_down", "cooldown"
			result.attempt = prior
			return result, nil
		}

		result.rows, err = s.Search(context.WithValue(ctx, matchTraceKey{}, trace), provider, request.Title)
		if err != nil {
			return result, err
		}
		result.work = matchingWork(result.rows, request)
		if result.work != nil {
			if prior.RetryAt != 0 {
				if err = s.store.clearSearchFailure(ctx, key, provider); err != nil {
					return result, apperror.Wrap(apperror.Storage, err, "清除弹幕搜索冷却失败")
				}
			}
			return result, nil
		}

		code, detail := workMatchFailure(result.rows, request)
		if provider == "bilibili" && bilibiliOfficialMissing(result.rows) {
			code = "WORK_OFFICIAL_MISSING"
		}
		result.attempt = MatchAttempt{Provider: provider, Status: "no_match", Stage: "work", Code: code, Detail: detail}
		if code == "WORK_SEARCH_EMPTY" || code == "WORK_OFFICIAL_MISSING" {
			result.attempt.RetryAt, err = s.store.recordSearchFailure(ctx, key, provider, code, s.now())
			if err != nil {
				return result, apperror.Wrap(apperror.Storage, err, "保存弹幕搜索冷却失败")
			}
		}
		return result, nil
	})
}

func (s *Store) searchFailure(ctx context.Context, key, provider string) (MatchAttempt, error) {
	value := MatchAttempt{Provider: provider}
	db, err := s.db()
	if err != nil {
		return value, err
	}
	err = db.QueryRowContext(ctx, `SELECT code,retry_at FROM danmaku_search_failures WHERE work_key=? AND provider=?`, key, provider).Scan(&value.Code, &value.RetryAt)
	if errors.Is(err, sql.ErrNoRows) {
		return value, nil
	}
	return value, err
}

func (s *Store) clearSearchFailure(ctx context.Context, key, provider string) error {
	db, err := s.db()
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `DELETE FROM danmaku_search_failures WHERE work_key=? AND provider=?`, key, provider)
	return err
}

func (s *Store) recordSearchFailure(ctx context.Context, key, provider, code string, now time.Time) (int64, error) {
	db, err := s.db()
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `DELETE FROM danmaku_search_failures WHERE failed_at<=?`, now.Add(-searchFailureRetention).Unix()); err != nil {
		return 0, err
	}
	var failures int
	var retryAt int64
	err = tx.QueryRowContext(ctx, `SELECT failures,retry_at FROM danmaku_search_failures WHERE work_key=? AND provider=?`, key, provider).Scan(&failures, &retryAt)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if retryAt > now.Unix() {
		return retryAt, tx.Commit()
	}
	backoffIndex := min(max(failures, 0), len(searchBackoff)-1)
	failures = backoffIndex + 1
	retryAt = now.Add(searchBackoff[backoffIndex]).Unix()
	_, err = tx.ExecContext(ctx, `INSERT INTO danmaku_search_failures(work_key,provider,code,failures,retry_at,failed_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(work_key,provider) DO UPDATE SET
		code=excluded.code,failures=excluded.failures,retry_at=excluded.retry_at,failed_at=excluded.failed_at`, key, provider, code, failures, retryAt, now.Unix())
	if err != nil {
		return 0, err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM danmaku_search_failures`).Scan(&count); err != nil {
		return 0, err
	}
	if count > searchFailureLimit {
		_, err = tx.ExecContext(ctx, `DELETE FROM danmaku_search_failures WHERE rowid IN
			(SELECT rowid FROM danmaku_search_failures ORDER BY failed_at,rowid LIMIT ?)`, count-searchFailureLimit)
		if err != nil {
			return 0, err
		}
	}
	return retryAt, tx.Commit()
}
