package library

import (
	"context"
	"net/url"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/model"
)

// ReadingEvidence reports recent observations at the current rule revision.
// Counts describe chapters actually attempted, not the unread directory.
type ReadingEvidence struct {
	Readable   int   `json:"readable"`
	Restricted int   `json:"restricted"`
	Failed     int   `json:"failed"`
	Warnings   int   `json:"warnings"`
	Stale      int   `json:"stale"`
	CheckedAt  int64 `json:"checkedAt"`
}

type SourceOperationEvidence struct {
	Operation string `json:"operation"`
	Status    string `json:"status"`
	CheckedAt int64  `json:"checkedAt"`
	Count     int    `json:"count"`
	Code      string `json:"code"`
	Millis    int64  `json:"millis"`
}

type sourceEvidenceSnapshot struct {
	checks   map[string]db.ReadingSourceCheck
	health   map[string]db.ReadingSearchHealth
	quick    map[string]*SourceQuality
	chapters map[string][]db.ReadingObservationCount
}

func (s *Service) sourceEvidence(ctx context.Context, kind model.SourceKind) (*sourceEvidenceSnapshot, error) {
	checks, err := db.ReadReadingSourceChecks(ctx, kind)
	if err != nil {
		return nil, err
	}
	health, err := db.ReadReadingSearchHealth(ctx, kind)
	if err != nil {
		return nil, err
	}
	quick, err := s.loadSourceQualities()
	if err != nil {
		return nil, err
	}
	e := &sourceEvidenceSnapshot{checks: checks, health: health, quick: quick, chapters: map[string][]db.ReadingObservationCount{}}
	if kind == model.SourceKindNovel {
		rows, err := db.ReadNovelObservations(ctx, 0)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			e.chapters[row.SourceKey] = append(e.chapters[row.SourceKey], row)
		}
	}
	return e, nil
}

func readingEvidence(rows []db.ReadingObservationCount, revision string) *ReadingEvidence {
	out := &ReadingEvidence{}
	for _, row := range rows {
		if row.Revision != revision || row.CheckedAt < time.Now().Add(-7*24*time.Hour).Unix() {
			out.Stale += row.Count
			continue
		}
		out.CheckedAt = max(out.CheckedAt, row.CheckedAt)
		switch row.Outcome {
		case "text":
			out.Readable += row.Count
		case "restricted":
			out.Restricted += row.Count
		case "warning":
			out.Warnings += row.Count
		default:
			out.Failed += row.Count
		}
	}
	return out
}

func (e *sourceEvidenceSnapshot) routing(kind, id string) matching.SourceEvidence {
	result := checkEvidence(e.checks[id], kind, id)
	if kind != "novel" {
		return result
	}
	// Quick samples retain their own timestamp and cannot freshen a deep report.
	// They only supply missing/older ranking evidence, never whole-work proof.
	if q := e.quick[id]; q != nil && q.CheckedAt > e.checks[id].DeepAt {
		result.Score, result.Status, result.Confidence = q.Score, q.Status, "limited"
		result.Fresh = q.CheckedAt > time.Now().Add(-7*24*time.Hour).Unix()
		result.Readable = false
		for _, stage := range q.Stages {
			if stage.Name == StageContent && stage.Status == "ok" && stage.Count > 0 {
				result.Readable = true
			}
		}
	}
	return result
}

func (e *sourceEvidenceSnapshot) operations(id string) []*SourceOperationEvidence {
	byOperation := map[string]*SourceOperationEvidence{}
	if q := e.quick[id]; q != nil {
		for _, stage := range q.Stages {
			if stage.Name == StageConfig {
				continue
			}
			byOperation[stage.Name] = &SourceOperationEvidence{Operation: stage.Name, Status: stage.Status, CheckedAt: q.CheckedAt, Count: stage.Count, Code: stage.Code, Millis: stage.Millis}
			if stage.Name == StageSearch && stage.Code == "NOT_FOUND" {
				byOperation[stage.Name].Status = "empty"
			}
		}
	}
	if h, ok := e.health[id]; ok && (byOperation[StageSearch] == nil || h.CheckedAt > byOperation[StageSearch].CheckedAt) {
		status := "ok"
		if h.ErrorCode != "" {
			status = "failed"
		}
		byOperation[StageSearch] = &SourceOperationEvidence{Operation: StageSearch, Status: status, CheckedAt: h.CheckedAt, Code: h.ErrorCode}
	}
	out := []*SourceOperationEvidence{}
	for _, operation := range []string{StageSearch, StageDetail, StageToc, StageContent, StagePages, "image"} {
		if entry := byOperation[operation]; entry != nil {
			if entry.CheckedAt < time.Now().Add(-7*24*time.Hour).Unix() {
				entry.Status = "stale"
			}
			out = append(out, entry)
		}
	}
	return out
}

// The maintained set is explicit. Executable community rules remain available
// for opt-in use; successful parsing alone never opts a new rule into searches.
func maintainedNovel(address string) bool {
	u, err := url.Parse(address)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "appi.kuwo.cn", "m.suixkan.com", "book.zhaicool.net", "fanqienovel.com":
		return true
	default:
		return false
	}
}
