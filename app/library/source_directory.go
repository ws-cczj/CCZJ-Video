package library

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/library/comic"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/library/novel"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
)

type BuiltinSourceRow struct {
	ID         string `json:"id"`
	SourceKey  string `json:"sourceKey"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	Pool       string `json:"pool"`
	Score      int    `json:"score"`
	Confidence string `json:"confidence"`
	CheckedAt  int64  `json:"checkedAt"`
	DeepAt     int64  `json:"deepAt"`
	State      string `json:"state"`
}
type SourceDirectory struct {
	Kind            string              `json:"kind"`
	Registered      int                 `json:"registered"`
	AwaitingAdapter int                 `json:"awaitingAdapter"`
	Rows            []*BuiltinSourceRow `json:"rows"`
	AuditRunning    bool                `json:"auditRunning"`
}

func checkEvidence(check db.ReadingSourceCheck, kind, sourceID string) matching.SourceEvidence {
	e := matching.SourceEvidence{Score: -1, Confidence: "insufficient"}
	var report quality.Report
	if len(check.SummaryJSON) > 0 && len(check.SummaryJSON) <= 2<<20 && json.Unmarshal([]byte(check.SummaryJSON), &report) == nil && report.Version == quality.Version && report.Source.Kind == kind && report.Source.ID == sourceID && report.Summary.Score >= 0 && report.Summary.Score <= 100 {
		if report.Summary.Confidence != "insufficient" && report.Summary.Confidence != "limited" && report.Summary.Confidence != "substantial" {
			return e
		}
		if report.Summary.Attempts <= 0 || report.Summary.Readable < 0 || report.Summary.Attempts < report.Summary.Readable || report.Summary.Verified > report.Summary.Matched {
			return e
		}
		switch report.Summary.Status {
		case "healthy", "partial", "failed", "inconclusive":
		default:
			return e
		}
		e.Score = report.Summary.Score
		e.Confidence = report.Summary.Confidence
		e.Status = report.Summary.Status
		e.Readable = report.Summary.Readable > 0
		e.Fresh = check.DeepAt > time.Now().Add(-7*24*time.Hour).Unix()
	}
	return e
}

// SourceDirectory exposes only builtin adapted sources. Unsupported builtin
// metadata remains in the internal audit registry and is counted separately.
func (s *Service) SourceDirectory(ctx context.Context, kind model.SourceKind) (*SourceDirectory, error) {
	if kind != model.SourceKindNovel && kind != model.SourceKindComic {
		return nil, apperror.New(apperror.Validation, "未知阅读模式")
	}
	if err := s.ensureBuiltinSources(ctx, kind); err != nil {
		return nil, err
	}
	registry, err := s.registryFor(kind)
	if err != nil {
		return nil, err
	}
	installed, err := db.GetSourcesByKindContext(ctx, kind)
	if err != nil {
		return nil, err
	}
	checks, err := db.ReadReadingSourceChecks(ctx, kind)
	if err != nil {
		return nil, err
	}
	health, err := db.ReadReadingSearchHealth(ctx, kind)
	if err != nil {
		return nil, err
	}
	byID := map[string]*model.Source{}
	for _, source := range installed {
		byID[sourceQualityID(string(kind), source.ApiUrl, source.StrategyConfig)] = source
	}
	out := &SourceDirectory{Kind: string(kind), Registered: len(registry), Rows: []*BuiltinSourceRow{}, AuditRunning: s.auditRunning.Load()}
	for _, row := range registry {
		if !row.CanRead {
			out.AwaitingAdapter++
			continue
		}
		installed := byID[row.ID]
		if installed == nil {
			continue
		}
		check := checks[row.ID]
		e := checkEvidence(check, string(kind), row.ID)
		if check.RetryAfter > time.Now().Unix() {
			e.Status = "failed"
		}
		pool := novel.Pool(e)
		if kind == model.SourceKindComic {
			pool = comic.Pool(e)
		}
		label := map[int]string{0: "known", 1: "preferred", 2: "supplement", 3: "reserve"}[pool]
		state := check.State
		if state == "" {
			state = "unmeasured"
		}
		checkedAt := check.CheckedAt
		if observed, ok := health[row.ID]; ok && observed.CheckedAt > time.Now().Add(-24*time.Hour).Unix() {
			checkedAt = max(checkedAt, observed.CheckedAt)
			if observed.ErrorCode == "" {
				state = "search_ready"
			} else {
				state = "search_failed"
			}
			if observed.RetryAfter > time.Now().Unix() {
				state = "search_cooldown"
				label = "reserve"
			}
		}
		out.Rows = append(out.Rows, &BuiltinSourceRow{ID: row.ID, SourceKey: installed.SourceKey, Name: row.Name, Enabled: installed.Enabled == 1, Pool: label, Score: e.Score, Confidence: e.Confidence, CheckedAt: checkedAt, DeepAt: check.DeepAt, State: state})
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		searchRank := func(row *BuiltinSourceRow) int {
			if row.State == "search_cooldown" {
				return 2
			}
			if row.State == "search_ready" {
				return 0
			}
			return 1
		}
		if searchRank(a) != searchRank(b) {
			return searchRank(a) < searchRank(b)
		}
		rank := map[string]int{"known": 0, "preferred": 1, "supplement": 2, "reserve": 3}
		if rank[a.Pool] != rank[b.Pool] {
			return rank[a.Pool] < rank[b.Pool]
		}
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		return a.Name < b.Name
	})
	return out, nil
}
