package library

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/library/comic"
	"cczjVideo/app/library/novel"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
)

// RunSourceMaintenance belongs to the active mode lifecycle. It yields to
// foreground acquisition, resumes from persisted per-source clocks, and never
// deletes user data. Only one maintenance loop is owned.
func (s *Service) RunSourceMaintenance(ctx context.Context, mode string) {
	if !s.routingEnabled.Load() || !s.auditRunning.CompareAndSwap(false, true) {
		return
	}
	defer s.auditRunning.Store(false)
	if !waitReading(ctx, 45*time.Second) {
		return
	}
	kind := model.SourceKind(mode)
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return
	}
	for ctx.Err() == nil {
		if kind == model.SourceKindNovel {
			call, release, err := s.priority.background(ctx)
			if err != nil {
				return
			}
			err = s.refreshNovelSubscription(call)
			release()
			if err != nil && ctx.Err() == nil {
				applog.Warn("[novel] 更新订阅规则: %v", err)
			}
		}
		if err := s.auditSources(ctx, kind); err != nil && ctx.Err() == nil {
			applog.Warn("[%s] 内置源巡检: %v", kind, err)
		}
		if !waitReading(ctx, 30*time.Minute) {
			return
		}
	}
}
func waitReading(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Service) auditSources(ctx context.Context, kind model.SourceKind) error {
	rows, err := s.registryFor(kind)
	if err != nil {
		return err
	}
	checks, err := db.ReadReadingSourceChecks(ctx, kind)
	if err != nil {
		return err
	}
	corpus, err := quality.AuditCorpusContext(ctx, string(kind))
	if err != nil {
		return err
	}
	// The mode corpus includes online metadata and curated catalog anchors.
	index, err := quality.Compile(corpus)
	if err != nil {
		return err
	}
	engine, err := newBatchEngine(index, BatchOptions{ProbeTimeout: 2 * time.Second, OperationTimeout: 4 * time.Second, Queries: 3, Matches: 3, Delay: 350 * time.Millisecond, MaxDeepSources: len(rows)}, s.readingGate)
	if err != nil {
		return err
	}
	defer engine.Close()
	interval, deepInterval := novel.AuditInterval, novel.DeepInterval
	if kind == model.SourceKindComic {
		interval, deepInterval = comic.AuditInterval, comic.DeepInterval
	}
	ordered := append([]*BatchSource(nil), rows...)
	installed, err := db.GetSourcesByKindContext(ctx, kind)
	if err != nil {
		return err
	}
	disabled := map[string]bool{}
	for _, source := range installed {
		if source.Enabled != 1 {
			disabled[sourceQualityID(string(kind), source.ApiUrl, source.StrategyConfig)] = true
		}
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		// Metadata-only catalogs must not hold up checks for the adapters that
		// foreground searches can actually use.
		if ordered[i].CanRead != ordered[j].CanRead {
			return ordered[i].CanRead
		}
		return checks[ordered[i].ID].CheckedAt < checks[ordered[j].ID].CheckedAt
	})
	for _, source := range ordered {
		if kind == model.SourceKindNovel && !source.CanRead {
			continue
		}
		if disabled[source.ID] {
			continue
		}
		prior := checks[source.ID]
		if prior.RetryAfter > time.Now().Unix() {
			continue
		}
		if prior.CheckedAt > time.Now().Add(-interval).Unix() {
			continue
		}
		call, release, err := s.priority.background(ctx)
		if err != nil {
			return err
		}
		call, cancel := context.WithTimeout(call, 90*time.Second)
		result := engine.execute(call, source, prior.DeepAt <= time.Now().Add(-deepInterval).Unix())
		interrupted := errors.Is(call.Err(), context.Canceled)
		cancel()
		release()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if interrupted {
			if !waitReading(ctx, time.Second) {
				return ctx.Err()
			}
			continue
		}
		check := db.ReadingSourceCheck{Kind: string(kind), SourceID: source.ID, CheckedAt: time.Now().Unix(), State: result.State, Score: -1, Confidence: "insufficient"}
		if result.Discovery != nil && result.Discovery.Queries > 0 && len(result.Discovery.Failures) == result.Discovery.Queries && len(result.Discovery.MatchedIDs) == 0 {
			check.State = "search_unavailable"
			gone := kind == model.SourceKindNovel
			for _, failure := range result.Discovery.Failures {
				gone = gone && failure.Code == "SOURCE_GONE"
			}
			if gone {
				if err := db.RecordReadingSearchHealth(ctx, kind, source.ID, "SOURCE_GONE"); err != nil {
					return err
				}
				for _, row := range installed {
					if sourceQualityID(string(kind), row.ApiUrl, row.StrategyConfig) == source.ID {
						if err := db.PauseMissingReadingSource(ctx, row.SourceKey, row.StrategyConfig); err != nil {
							return err
						}
					}
				}
			}
		}
		if result.Quality != nil {
			raw, err := json.Marshal(result.Quality)
			if err != nil {
				return err
			}
			check.DeepAt = check.CheckedAt
			check.SummaryJSON = string(raw)
			check.Score = result.Quality.Summary.Score
			check.Confidence = result.Quality.Summary.Confidence
		}
		if err := db.SaveReadingSourceCheck(ctx, check); err != nil {
			return err
		}
		if !waitReading(ctx, 350*time.Millisecond) {
			return ctx.Err()
		}
	}
	return nil
}
