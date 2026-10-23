package library

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

const (
	searchCursorTTL        = 15 * time.Minute
	maxSearchCursors       = 128
	maxSearchCursorHistory = 128
)

type sourceSearchState struct {
	Key, Version, URL string
	Page              int
	Seen              []string
}

type searchContinuation struct {
	Kind     model.SourceKind
	Keyword  string
	Page     int
	Sources  []sourceSearchState
	Failures []*SourceResult
	Created  time.Time
}

// Cursors contain no caller-supplied URLs. Immutable continuation snapshots are
// bounded and expire; reusing one retries the same page without advancing it.
type searchContinuations struct {
	mu      sync.Mutex
	entries map[string]searchContinuation
}

func (c *searchContinuations) put(state searchContinuation) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]searchContinuation)
	}
	var oldest string
	var oldestTime time.Time
	for key, entry := range c.entries {
		if time.Since(entry.Created) >= searchCursorTTL {
			delete(c.entries, key)
			continue
		}
		if oldestTime.IsZero() || entry.Created.Before(oldestTime) {
			oldest, oldestTime = key, entry.Created
		}
	}
	if len(c.entries) >= maxSearchCursors {
		delete(c.entries, oldest)
	}
	key := rand.Text()
	state.Created = time.Now()
	c.entries[key] = state
	return key
}

func (c *searchContinuations) get(key string, kind model.SourceKind, keyword string) (searchContinuation, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state, ok := c.entries[key]
	if !ok || time.Since(state.Created) >= searchCursorTTL || state.Kind != kind || state.Keyword != keyword {
		return searchContinuation{}, apperror.New(apperror.Conflict, "搜索分页已过期或不属于当前查询，请重新搜索")
	}
	return state, nil
}

func validateSearch(kind model.SourceKind, keyword string) error {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return apperror.New(apperror.Validation, "阅读搜索只支持小说或漫画")
	}
	if strings.TrimSpace(keyword) == "" {
		return apperror.New(apperror.Validation, "搜索词不能为空")
	}
	return nil
}

// SearchPage starts or continues a query using independent source cursors.
// Cursors are scoped to this Service instance; source/configuration changes do
// not silently redirect an old continuation into a different source.
func (s *Service) SearchPage(ctx context.Context, kind model.SourceKind, keyword, cursor string) (*SearchOutcome, error) {
	return s.SearchPageStream(ctx, kind, keyword, cursor, nil)
}

func (s *Service) SearchPageStream(ctx context.Context, kind model.SourceKind, keyword, cursor string, onResult func(*SourceResult)) (*SearchOutcome, error) {
	if err := validateSearch(kind, keyword); err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	var resolveErr error
	keyword, resolveErr = resolveSearchKeyword(ctx, kind, keyword)
	if resolveErr != nil {
		return nil, resolveErr
	}
	release := s.priority.begin()
	defer release()
	if err := s.ensureBuiltinSources(ctx, kind); err != nil {
		return nil, err
	}
	rows, err := db.GetEnabledSourcesByKind(kind)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取源列表失败")
	}
	if s.routingEnabled.Load() {
		registry, err := s.registryFor(kind)
		if err != nil {
			return nil, err
		}
		unsearchable := map[string]bool{}
		eligible := map[string]bool{}
		for _, candidate := range registry {
			if !candidate.CanRead {
				unsearchable[candidate.ID] = true
			} else {
				eligible[candidate.ID] = true
			}
		}
		filtered := rows[:0]
		for _, source := range rows {
			// Old installed snapshots may contain a fixed author/book list.
			// Retain it for existing books, but do not query it as a search route.
			id := sourceQualityID(string(kind), source.ApiUrl, source.StrategyConfig)
			if source.SourceKey == id && !eligible[id] {
				if doc, err := provider.ParseDocument(source.StrategyConfig, string(kind)); err == nil && !legacyDocumentHasKeyword(doc) {
					continue
				}
			}
			if !unsearchable[id] {
				filtered = append(filtered, source)
			}
		}
		rows = filtered
	}
	byKey := make(map[string]*model.Source, len(rows))
	for _, row := range rows {
		byKey[row.SourceKey] = row
	}
	state := searchContinuation{Kind: kind, Keyword: keyword, Page: 1}
	if cursor != "" {
		state, err = s.continuations.get(cursor, kind, keyword)
		if err != nil {
			return nil, err
		}
	} else {
		if s.routingEnabled.Load() {
			if err := s.orderSearchSources(ctx, kind, keyword, rows); err != nil {
				return nil, err
			}
		}
		for _, row := range rows {
			state.Sources = append(state.Sources, sourceSearchState{Key: row.SourceKey, Version: sourceRevision(row), Page: 1})
		}
	}
	var out *SearchOutcome
	var pending []sourceSearchState
	if s.routingEnabled.Load() {
		out, state.Sources, pending, err = s.searchRoutedPlans(ctx, kind, keyword, state.Page, state.Sources, byKey, onResult)
	} else {
		out, err = s.searchPlans(ctx, kind, keyword, state.Page, state.Sources, byKey, onResult)
	}
	if err != nil {
		return nil, err
	}
	next := pending
	for i, result := range out.Results {
		if result.ErrorCode != "" || !result.HasNext || result.NextURL == "" {
			continue
		}
		previous := state.Sources[i]
		sum := sha256.Sum256([]byte(result.NextURL))
		fingerprint := hex.EncodeToString(sum[:])
		repeated := result.NextURL == previous.URL
		for _, seen := range previous.Seen {
			if seen == fingerprint {
				repeated = true
				break
			}
		}
		if repeated {
			result.HasNext, result.NextURL = false, ""
			continue
		}
		seen := append(append([]string(nil), previous.Seen...), fingerprint)
		if len(seen) > maxSearchCursorHistory {
			seen = append([]string(nil), seen[len(seen)-maxSearchCursorHistory:]...)
		}
		next = append(next, sourceSearchState{Key: previous.Key, Version: previous.Version, Page: previous.Page + 1, URL: result.NextURL, Seen: seen})
	}
	if len(next) > 0 {
		failures := append([]*SourceResult(nil), state.Failures...)
		for _, result := range out.Results {
			if result.ErrorCode != "" {
				failures = append(failures, result)
			}
		}
		out.NextCursor = s.continuations.put(searchContinuation{Kind: kind, Keyword: keyword, Page: state.Page + 1, Sources: next, Failures: failures})
	}
	// Keep failed sources visible without repeatedly issuing failed requests.
	out.Results = append(out.Results, state.Failures...)
	out.FailedCount += len(state.Failures)
	return out, nil
}

func (s *Service) searchPlans(ctx context.Context, kind model.SourceKind, keyword string, page int, plans []sourceSearchState, sources map[string]*model.Source, onResult func(*SourceResult)) (*SearchOutcome, error) {
	out := &SearchOutcome{Keyword: keyword, Page: page, Results: make([]*SourceResult, len(plans))}
	jobs := make(chan int)
	var group sync.WaitGroup
	for worker := 0; worker < min(searchConcurrency, len(plans)); worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				select {
				case s.searchSlots <- struct{}{}:
				case <-ctx.Done():
					return
				}
				if ctx.Err() != nil {
					<-s.searchSlots
					return
				}
				plan := plans[index]
				var result *SourceResult
				if source := sources[plan.Key]; source != nil {
					result = s.searchSourcePage(ctx, kind, source, keyword, plan)
				} else {
					result = &SourceResult{SourceKey: plan.Key, SourceName: plan.Key, Items: []*Item{}, ErrorCode: string(apperror.Unavailable), ErrorMessage: "来源已移除或停用，请重新搜索"}
				}
				<-s.searchSlots
				out.Results[index] = result
				if onResult != nil && ctx.Err() == nil {
					onResult(result)
				}
			}
		}()
	}
dispatch:
	for index := range plans {
		select {
		case jobs <- index:
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	group.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for _, result := range out.Results {
		out.Total += len(result.Items)
		if result.ErrorCode != "" {
			out.FailedCount++
		}
	}
	return out, nil
}
