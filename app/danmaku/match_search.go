package danmaku

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/netbudget"
)

const (
	searchFailureRetention = 24 * time.Hour
	searchFailureLimit     = 10000
	// Reserve the rest of the provider deadline for the episode and window reads.
	bilibiliTVIndexTimeout = matchProbeTimeout - 4*time.Second
	bilibiliKeywordDelay   = time.Second
)

var searchBackoff = [...]time.Duration{noMatchCooldown, 15 * time.Minute, 30 * time.Minute}

type workSearchResult struct {
	rows    []Candidate
	work    *Candidate
	attempt MatchAttempt
}

func bilibiliIndexYear(request MatchRequest) string {
	if mediaKind(request.Kind) == "tv" {
		return metadataYear(request.Year)
	}
	return ""
}

func (s *Service) matchSearch(ctx context.Context, request MatchRequest, provider string) (workSearchResult, error) {
	key := workIdentity(request)
	if provider == "bilibili" && bilibiliIndexYear(request) != "" {
		// The old search could record an index timeout as an empty result.
		key = "tv-index-v2:" + key
	} else if provider == "bilibili" && bilibiliSeriesQuery(request.Title) != strings.TrimSpace(request.Title) {
		// The previous search ledger covered only a verbatim season-title query.
		key = "series-search:" + key
	}
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

		result.rows, err = s.searchMatchCandidates(context.WithValue(ctx, matchTraceKey{}, trace), provider, request)
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

func (s *Service) searchMatchCandidates(ctx context.Context, provider string, request MatchRequest) ([]Candidate, error) {
	title := strings.TrimSpace(request.Title)
	source, err := s.provider(ctx, provider)
	if err != nil {
		return nil, err
	}
	bili, ok := source.(*bilibiliProvider)
	if !ok {
		return s.cachedSearch(ctx, provider, title, source)
	}
	keywords := func(ctx context.Context) ([]Candidate, error) {
		return s.searchBilibiliKeywords(ctx, bili, title, request)
	}
	year := bilibiliIndexYear(request)
	if year == "" {
		return keywords(ctx)
	}
	return s.searchBilibiliTV(ctx, bili, title, year, request, keywords)
}

type bilibiliSearchResult struct {
	rows []Candidate
	err  error
}

func bilibiliSearchStopsFallback(err error) bool {
	switch apperror.CodeOf(err) {
	case apperror.RateLimited, apperror.PermissionDenied, apperror.AuthenticationRequired:
		return true
	default:
		return false
	}
}

func (s *Service) searchBilibiliTV(ctx context.Context, bili *bilibiliProvider, title, year string, request MatchRequest, keywords func(context.Context) ([]Candidate, error)) ([]Candidate, error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()

	indexResults := make(chan bilibiliSearchResult, 1)
	workers.Go(func() {
		indexResults <- s.searchBilibiliTVIndex(ctx, bili, title, year, request)
	})
	timer := time.NewTimer(bilibiliKeywordDelay)
	defer timer.Stop()

	var keywordResults chan bilibiliSearchResult
	var index, keyword bilibiliSearchResult
	indexDone, keywordDone := false, false
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			matchLog(ctx, "danmaku keyword fallback started", applog.Fields{"provider": "bilibili", "delay_ms": bilibiliKeywordDelay.Milliseconds()})
			keywordResults = make(chan bilibiliSearchResult, 1)
			workers.Go(func() {
				rows, err := keywords(netbudget.WithoutQueue(ctx))
				keywordResults <- bilibiliSearchResult{rows: rows, err: err}
			})
		case index = <-indexResults:
			indexDone = true
			if len(index.rows) > 0 || bilibiliSearchStopsFallback(index.err) {
				return index.rows, index.err
			}
			if keywordResults == nil {
				keyword.rows, keyword.err = keywords(ctx)
				keywordDone = true
			}
		case keyword = <-keywordResults:
			keywordDone = true
			if bilibiliSearchStopsFallback(keyword.err) {
				return nil, keyword.err
			}
			// An early keyword result needs an exact, unambiguous season.
			if work := matchingWork(keyword.rows, request); keyword.err == nil && work != nil &&
				candidateTitleSimilarity(*work, title) == 1000 {
				localYear, workYear := metadataYear(request.Year), metadataYear(work.Year)
				if workYear == "" || workYear == localYear {
					return keyword.rows, nil
				}
			}
		}
		if indexDone && keywordDone {
			if errors.Is(keyword.err, netbudget.ErrNoSpareCapacity) {
				matchLog(ctx, "danmaku keyword fallback queued after index", applog.Fields{"provider": "bilibili"})
				keyword.rows, keyword.err = keywords(ctx)
			}
			if keyword.err == nil && index.err != nil && matchingWork(keyword.rows, request) == nil {
				return nil, index.err
			}
			return keyword.rows, keyword.err
		}
	}
}

func (s *Service) searchBilibiliTVIndex(ctx context.Context, bili *bilibiliProvider, title, year string, request MatchRequest) bilibiliSearchResult {
	started := time.Now()
	indexCtx, cancel := context.WithTimeout(ctx, bilibiliTVIndexTimeout)
	defer cancel()
	rows, err := cachedDiscovery(indexCtx, s, "bilibili", "tv-premiere-index", year, 50, func(ctx context.Context) ([]Candidate, error) {
		return bili.tvIndex(ctx, year)
	})
	fields := applog.Fields{"provider": "bilibili", "duration_ms": time.Since(started).Milliseconds()}
	if err != nil {
		fields["code"] = apperror.CodeOf(err)
		matchLog(ctx, "danmaku official TV index unavailable", fields)
		return bilibiliSearchResult{err: err}
	}
	fields["count"] = len(rows)
	if work := matchingWork(rows, request); work != nil && candidateTitleSimilarity(*work, title) == 1000 {
		fields["ref"] = work.Ref
		matchLog(ctx, "danmaku official TV index matched", fields)
		return bilibiliSearchResult{rows: []Candidate{*work}}
	}
	matchLog(ctx, "danmaku official TV index had no exact match", fields)
	return bilibiliSearchResult{}
}

func (s *Service) searchBilibiliKeywords(ctx context.Context, bili *bilibiliProvider, title string, request MatchRequest) ([]Candidate, error) {
	search := func(query string) ([]Candidate, error) {
		return cachedDiscovery(ctx, s, "bilibili", "official-search", query, 200, func(ctx context.Context) ([]Candidate, error) {
			return bili.searchClasses(ctx, query, "media_ft", "media_bangumi")
		})
	}
	base := bilibiliSeriesQuery(title)
	rows, err := search(base)
	if err != nil || base == title {
		return rows, err
	}
	for _, row := range rows {
		if workMatchScore(row, request) >= 0 {
			return rows, nil
		}
	}
	specific, err := search(title)
	return append(rows, specific...), err
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
