package library

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/library/comic"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/library/novel"
	"cczjVideo/app/model"
)

type readingPriority struct {
	mu         sync.Mutex
	foreground int
	cancel     context.CancelFunc
}

func (p *readingPriority) begin() func() {
	p.mu.Lock()
	p.foreground++
	if p.cancel != nil {
		p.cancel()
	}
	p.mu.Unlock()
	return func() { p.mu.Lock(); p.foreground--; p.mu.Unlock() }
}
func (p *readingPriority) background(ctx context.Context) (context.Context, context.CancelFunc, error) {
	for {
		p.mu.Lock()
		if p.foreground == 0 {
			call, cancel := context.WithCancel(ctx)
			p.cancel = cancel
			p.mu.Unlock()
			return call, cancel, nil
		}
		p.mu.Unlock()
		timer := time.NewTimer(200 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// EnableBuiltinRouting is called by application assembly, after persistence is
// initialized. Legacy programmatic callers retain their explicit source set.
func (s *Service) EnableBuiltinRouting() { s.routingEnabled.Store(true) }
func (s *Service) WrapReadingTransport(t http.RoundTripper) http.RoundTripper {
	return s.readingGate.Wrap(t)
}
func (s *Service) BeginReadingForeground() func() { return s.priority.begin() }

func (s *Service) RecordReadingImage(ctx context.Context, sourceKey, referer, revision string) error {
	if len(revision) != 64 {
		return nil
	}
	row, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return err
	}
	if row.Kind != "comic" {
		return nil
	}
	if revision != sourceRevision(row) {
		return nil
	}
	return db.RecordReadingImage(ctx, sourceKey, revision, referer)
}

func (s *Service) registryFor(kind model.SourceKind) ([]*BatchSource, error) {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return nil, fmt.Errorf("invalid reading mode")
	}
	s.registryMu.Lock()
	defer s.registryMu.Unlock()
	if rows := s.registry[string(kind)]; rows != nil {
		return rows, nil
	}
	base, err := ParseBatchSources(bundledSourceCatalog, string(kind), "builtin")
	if err != nil {
		return nil, err
	}
	catalogs, err := auditCatalogs(string(kind))
	if err != nil {
		return nil, err
	}
	community := []*BatchSource{}
	for _, raw := range catalogs {
		parsed, err := ParseBatchSources(raw, string(kind), "builtin-catalog")
		if err != nil {
			return nil, err
		}
		community = append(community, parsed...)
	}
	seen := map[string]bool{}
	rows := make([]*BatchSource, 0, len(base)+len(community))
	for _, list := range [][]*BatchSource{base, community} {
		for _, row := range list {
			if !seen[row.ID] {
				seen[row.ID] = true
				rows = append(rows, row)
			}
		}
	}
	if s.registry == nil {
		s.registry = map[string][]*BatchSource{}
	}
	s.registry[string(kind)] = rows
	return rows, nil
}

func (s *Service) ensureBuiltinSources(ctx context.Context, kind model.SourceKind) error {
	if !s.routingEnabled.Load() {
		return nil
	}
	registry, err := s.registryFor(kind)
	if err != nil {
		return err
	}
	s.browseMu.Lock()
	defer s.browseMu.Unlock()
	rows, err := db.GetSourcesByKindContext(ctx, kind)
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, row := range rows {
		known[sourceQualityID(string(kind), row.ApiUrl, row.StrategyConfig)] = true
	}
	for _, candidate := range registry {
		if !candidate.CanRead || known[candidate.ID] {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := db.AddSource(&model.Source{SourceKey: candidate.ID, Name: candidate.Name, ApiUrl: candidate.URL, Kind: string(kind), StrategyConfig: candidate.document, Enabled: 1}); err != nil {
			return err
		}
		known[candidate.ID] = true
	}
	return nil
}

func (s *Service) orderSearchSources(ctx context.Context, kind model.SourceKind, keyword string, rows []*model.Source) error {
	checks, err := db.ReadReadingSourceChecks(ctx, kind)
	if err != nil {
		return err
	}
	health, err := db.ReadReadingSearchHealth(ctx, kind)
	if err != nil {
		return err
	}
	known, err := db.KnownReadingRoutes(ctx, kind, matching.Normalize(keyword))
	if err != nil {
		return err
	}
	coverage := map[string]db.ReadingRouteEvidence{}
	for _, row := range known {
		coverage[row.SourceKey+"\x00"+row.Revision] = row
	}
	pool := map[string]int{}
	scores := map[string]int{}
	searchRank := map[string]int{}
	maintained := map[string]bool{}
	now := time.Now().Unix()
	for _, row := range rows {
		id := sourceQualityID(string(kind), row.ApiUrl, row.StrategyConfig)
		maintained[row.SourceKey] = bundledAdapter(string(kind), row.ApiUrl) != nil
		check, exists := checks[id]
		e := checkEvidence(check, string(kind), id)
		e.Fresh = e.Fresh && exists
		if check.RetryAfter > time.Now().Unix() {
			e.Status = "failed"
		}
		observed, hit := coverage[row.SourceKey+"\x00"+sourceRevision(row)]
		e.KnownWork = observed.Readable > 0
		if kind == model.SourceKindNovel {
			pool[row.SourceKey] = novel.Pool(e)
		} else {
			pool[row.SourceKey] = comic.Pool(e)
		}
		scores[row.SourceKey] = e.Score
		searchRank[row.SourceKey] = 3
		last := health[id]
		switch {
		case last.RetryAfter > now || check.RetryAfter > now:
			searchRank[row.SourceKey] = 5
		case e.KnownWork:
			searchRank[row.SourceKey] = 0
		case hit:
			searchRank[row.SourceKey] = 1
		case last.CheckedAt > now-86400 && last.ErrorCode == "":
			searchRank[row.SourceKey] = 2
		case last.CheckedAt > now-86400 && last.ErrorCode != "":
			searchRank[row.SourceKey] = 4
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].SourceKey, rows[j].SourceKey
		if searchRank[a] != searchRank[b] {
			return searchRank[a] < searchRank[b]
		}
		if pool[a] != pool[b] {
			return pool[a] < pool[b]
		}
		if scores[a] != scores[b] {
			return scores[a] > scores[b]
		}
		if maintained[a] != maintained[b] {
			return maintained[a]
		}
		return rows[i].Id < rows[j].Id
	})
	return nil
}

// Search in ordered five-source rounds. Successful exact-title results
// can return early; unsearched plans survive in the opaque continuation.
func (s *Service) searchRoutedPlans(ctx context.Context, kind model.SourceKind, keyword string, page int, plans []sourceSearchState, sources map[string]*model.Source, onResult func(*SourceResult)) (*SearchOutcome, []sourceSearchState, []sourceSearchState, error) {
	out := &SearchOutcome{Keyword: keyword, Page: page, Results: []*SourceResult{}}
	start := time.Now()
	batch, budget := novel.SearchBatch, novel.SearchBudget
	deadline := novel.SearchDeadline
	if kind == model.SourceKindComic {
		batch, budget = comic.SearchBatch, comic.SearchBudget
		deadline = comic.SearchDeadline
	}
	count := 0
	for count < len(plans) {
		if count > 0 && time.Since(start)+deadline > budget {
			break
		}
		end := min(count+batch, len(plans))
		part, err := s.searchPlans(ctx, kind, keyword, page, plans[count:end], sources, onResult)
		if err != nil {
			return nil, nil, nil, err
		}
		out.Results = append(out.Results, part.Results...)
		out.Total += part.Total
		out.FailedCount += part.FailedCount
		count = end
		exact := false
		for _, r := range part.Results {
			for _, item := range r.Items {
				if matching.Normalize(item.Title) == matching.Normalize(keyword) {
					exact = true
				}
			}
		}
		if exact || time.Since(start) >= budget {
			break
		}
	}
	out.SearchedSources = count
	for _, plan := range plans[count:] {
		if plan.Page == 1 && plan.URL == "" {
			out.PendingSources++
		}
	}
	return out, plans[:count], append([]sourceSearchState(nil), plans[count:]...), nil
}
