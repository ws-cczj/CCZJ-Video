package library

import (
	CCZJ "cczjVideo/app/cczj"
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strconv"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/library/comic"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/library/novel"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

type BuiltinSourceRow struct {
	ID           string                     `json:"id"`
	SourceKey    string                     `json:"sourceKey"`
	Name         string                     `json:"name"`
	Enabled      bool                       `json:"enabled"`
	Pool         string                     `json:"pool"`
	Score        int                        `json:"score"`
	Confidence   string                     `json:"confidence"`
	CheckedAt    int64                      `json:"checkedAt"`
	DeepAt       int64                      `json:"deepAt"`
	State        string                     `json:"state"`
	Supply       string                     `json:"supply"`
	Section      string                     `json:"section"`
	Site         string                     `json:"site"`
	Origin       string                     `json:"origin"`
	Project      string                     `json:"project"`
	Protocol     string                     `json:"protocol"`
	Revision     string                     `json:"revision"`
	Family       string                     `json:"family"`
	Language     string                     `json:"language"`
	Capabilities []string                   `json:"capabilities"`
	Operations   []*SourceOperationEvidence `json:"operations"`
	Reading      *ReadingEvidence           `json:"reading"`
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

// SourceDirectory retains installed custom sources as well as adapted builtins.
// Unsupported uninstalled metadata is counted separately, never as readable supply.
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
	evidence, err := s.sourceEvidence(ctx, kind)
	if err != nil {
		return nil, err
	}
	out := &SourceDirectory{Kind: string(kind), Registered: len(registry), Rows: []*BuiltinSourceRow{}, AuditRunning: s.auditRunning.Load()}
	var subscription *SourceOperationEvidence
	if kind == model.SourceKindNovel {
		raw, err := db.GetSetting(CCZJ.SettingNovelSubscriptionChecked)
		if err != nil {
			return nil, err
		}
		stamp, _ := strconv.ParseInt(raw, 10, 64)
		code, err := db.GetSetting(CCZJ.SettingNovelSubscriptionError)
		if err != nil {
			return nil, err
		}
		if stamp > 0 || code != "" {
			status := "ok"
			if code != "" {
				status = "failed"
				raw, err := db.GetSetting(CCZJ.SettingNovelSubscriptionAttempted)
				if err != nil {
					return nil, err
				}
				stamp, _ = strconv.ParseInt(raw, 10, 64)
			}
			subscription = &SourceOperationEvidence{Operation: "subscription", Status: status, CheckedAt: stamp, Code: code}
		}
	}
	byID := map[string]*BatchSource{}
	for _, row := range registry {
		byID[row.ID] = row
		if !row.CanRead {
			out.AwaitingAdapter++
		}
	}
	for _, installed := range installed {
		id := sourceQualityID(string(kind), installed.ApiUrl, installed.StrategyConfig)
		row := byID[id]
		check := evidence.checks[id]
		e := evidence.routing(string(kind), id)
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
		if observed, ok := evidence.health[id]; ok && observed.CheckedAt > time.Now().Add(-24*time.Hour).Unix() {
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
		entry := &BuiltinSourceRow{ID: id, SourceKey: installed.SourceKey, Name: installed.Name, Enabled: installed.Enabled == 1, Pool: label, Score: e.Score, Confidence: e.Confidence, CheckedAt: checkedAt, DeepAt: check.DeepAt, State: state,
			Supply: "custom", Section: "reserve", Site: installed.ApiUrl, Revision: sourceRevision(installed), Language: "unknown", Capabilities: []string{}, Operations: evidence.operations(id), Reading: readingEvidence(evidence.chapters[installed.SourceKey], sourceRevision(installed))}
		if kind == model.SourceKindNovel && !e.Fresh {
			entry.Score = -1
		}
		if u, err := url.Parse(installed.ApiUrl); err == nil {
			entry.Family = u.Hostname()
		}
		if row != nil {
			entry.Origin, entry.Project, entry.Language = row.Origin, row.Project, row.Language
			if kind == model.SourceKindNovel {
				entry.Language = "unknown"
			}
			entry.Supply = "supplement"
			if subscription != nil && row.Project == "Legado" {
				entry.Operations = append(entry.Operations, subscription)
			}
			if kind == model.SourceKindNovel && maintainedNovel(installed.ApiUrl) {
				entry.Supply = "maintained"
				entry.Language = "zh"
			}
		}
		if doc, err := provider.ParseDocument(installed.StrategyConfig, string(kind)); err == nil {
			entry.Protocol = doc.Adapter
			if entry.Protocol == "" {
				entry.Protocol = "declarative-v3"
			}
			entry.Capabilities = []string{"search", "detail", "toc"}
			if kind == model.SourceKindNovel {
				entry.Capabilities = append(entry.Capabilities, "content")
			} else {
				entry.Capabilities = append(entry.Capabilities, "pages")
			}
			if doc.Adapter == "fanqie-web" {
				entry.Capabilities = append(entry.Capabilities, "discover")
			}
		} else {
			entry.State = "invalid_config"
		}
		if kind == model.SourceKindNovel {
			// Keep independent operation results. A search success must not erase
			// restrictions or failed chapters from the reading state.
			r := entry.Reading
			switch {
			case entry.State == "invalid_config":
			case r.Restricted > 0:
				entry.State = "restricted"
			case r.Failed+r.Warnings > 0:
				entry.State = "reading_failed"
			case r.Readable > 0:
				entry.State = "chapter_verified"
			default:
				for _, operation := range entry.Operations {
					if operation.Operation == StageSearch && operation.Status == "failed" {
						entry.State = "search_failed"
					}
					if operation.Operation == StageContent && operation.Status == "failed" {
						entry.State = "reading_failed"
					}
					if operation.Operation == StageContent && operation.Status == "ok" && operation.Count > 0 {
						entry.State = "sample_verified"
					}
				}
				if !e.Fresh && entry.State == "unmeasured" && (e.Score >= 0 || r.Stale > 0) {
					entry.State = "evidence_stale"
				}
				if entry.State == "unmeasured" && evidence.quick[id] != nil && e.Fresh {
					entry.State = "sample_unconfirmed"
				}
			}
		}
		if entry.Enabled {
			entry.Section = "active"
		}
		if entry.State == "invalid_config" || entry.State == "reading_failed" || entry.State == "restricted" || entry.State == "search_cooldown" || entry.State == "search_failed" {
			entry.Section = "attention"
		}
		if evidence.health[id].RetryAfter > time.Now().Unix() {
			entry.Section = "attention"
		}
		if kind == model.SourceKindNovel && evidence.health[id].ErrorCode == "SOURCE_GONE" {
			if err := db.PauseMissingReadingSource(ctx, installed.SourceKey, installed.StrategyConfig); err != nil {
				return nil, err
			}
			entry.Enabled, entry.State, entry.Section = false, "source_unavailable", "attention"
		}
		out.Rows = append(out.Rows, entry)
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if kind == model.SourceKindNovel {
			rank := func(row *BuiltinSourceRow) int {
				if row.State == "source_unavailable" || row.State == "invalid_config" {
					return 3
				}
				if !row.Enabled {
					return 2
				}
				if row.Section == "attention" {
					return 1
				}
				return 0
			}
			if rank(a) != rank(b) {
				return rank(a) < rank(b)
			}
			if a.Score != b.Score {
				return a.Score > b.Score
			}
		}
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
