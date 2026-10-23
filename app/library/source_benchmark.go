package library

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/provider"
)

// BenchmarkCandidate evaluates an existing preview selection without importing
// it. Persist is explicit so a research run does not modify app preferences.
func (s *Service) BenchmarkCandidate(ctx context.Context, previewID, id string, corpus quality.Corpus, options quality.Options, persist bool) (*quality.Report, error) {
	candidate, err := s.catalogCandidate(previewID, id)
	if err != nil {
		return nil, err
	}
	if !candidate.Compatible {
		return nil, apperror.New(apperror.Unsupported, candidate.Reason)
	}
	select {
	case s.searchSlots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.searchSlots }()
	doc, err := provider.ParseDocument(candidate.document, candidate.Kind)
	if err != nil {
		return nil, err
	}
	reader := provider.NewPipeline(doc, s.fetcher.ScopeMode(candidate.ID, candidate.Kind), s.limits)
	probe, closeProbe := quality.NewImageProbeWithGate(doc.Headers, s.readingGate)
	defer closeProbe()
	report, err := quality.Evaluate(ctx, reader, quality.Source{ID: candidate.ID, Name: candidate.Name, Kind: candidate.Kind, URL: candidate.ApiURL}, corpus, options, probe)
	if err != nil {
		return nil, err
	}
	if persist {
		if err = s.saveBenchmarkQuality(candidate.ID, report); err != nil {
			return nil, err
		}
	}
	return report, nil
}

func (s *Service) saveBenchmarkQuality(id string, report *quality.Report) error {
	if report.Source.ID != id || len(id) != 64 || report.Version != quality.Version {
		return fmt.Errorf("invalid benchmark identity")
	}
	// Inconclusive means the source may simply not carry the benchmark's titles;
	// it must not overwrite a measured reading result with a fabricated failure.
	if report.Summary.Status == "inconclusive" {
		return nil
	}
	s.qualityMu.Lock()
	defer s.qualityMu.Unlock()
	rows, err := readSourceQualities()
	if err != nil {
		return err
	}
	sum := report.Summary
	stages := []*TestStage{{Name: StageConfig, Status: "ok", Message: report.Version + "；置信度：" + sum.Confidence + "；基准：" + report.CorpusHash},
		{Name: StageSearch, Status: "ok", Count: sum.Matched, Message: fmt.Sprintf("查询 %d，同版匹配 %d，身份已核实 %d；未收录不扣内容分", sum.Queries, sum.Matched, sum.Verified)}}
	name := StageContent
	if report.Source.Kind == "comic" {
		name = StagePages
	}
	reviews := map[string][]string{}
	addIssue := func(stage, title string, issues []quality.Issue) {
		for _, i := range issues {
			if i.Severity != "info" && len(reviews[stage]) < 5 {
				reviews[stage] = append(reviews[stage], title+"："+i.Code+" "+i.Evidence)
			}
		}
	}
	for _, w := range report.Works {
		for _, f := range w.Failures {
			if len(reviews[f.Stage]) < 5 {
				reviews[f.Stage] = append(reviews[f.Stage], w.Title+"："+f.Code+" "+f.Message)
			}
		}
		addIssue(StageDetail, w.Title, w.Issues)
		if w.Catalog != nil {
			addIssue(StageToc, w.Title, w.Catalog.Issues)
		}
		for _, sample := range w.Samples {
			if sample.Failure != nil && len(reviews[name]) < 5 {
				reviews[name] = append(reviews[name], sample.Title+"："+sample.Failure.Code+" "+sample.Failure.Message)
			}
			if sample.Text != nil {
				addIssue(name, sample.Title, sample.Text.Issues)
			}
			for _, img := range sample.Images {
				if img.Failure != nil && len(reviews["image"]) < 5 {
					reviews["image"] = append(reviews["image"], sample.Title+"："+img.Failure.Code+" "+img.Failure.Message)
				}
			}
		}
	}
	message := func(parts ...string) string {
		r := []rune(strings.Join(parts, "；"))
		if len(r) > 1600 {
			r = r[:1600]
		}
		return string(r)
	}
	if len(reviews[StageDetail]) > 0 {
		stages = append(stages, &TestStage{Name: StageDetail, Status: "failed", Message: message(reviews[StageDetail]...)})
	}
	tocStatus := "ok"
	if sum.CatalogCompared == 0 {
		tocStatus = "skipped"
	}
	if len(reviews[StageToc]) > 0 {
		tocStatus = "failed"
	}
	stages = append(stages, &TestStage{Name: StageToc, Status: tocStatus, Count: sum.CatalogCompared, Message: message(append(reviews[StageToc], strings.Join(sum.Notes, "；"))...)})
	status := "ok"
	if sum.Readable != sum.Attempts || sum.Attempts == 0 || len(reviews[name]) > 0 {
		status = "failed"
	}
	stages = append(stages, &TestStage{Name: name, Status: status, Count: sum.Readable, Millis: report.Millis, Message: message(append([]string{fmt.Sprintf("首中末抽样：成功 %d/%d；受限 %d；质量 %d/100", sum.Readable, sum.Attempts, sum.Restricted, sum.Score)}, reviews[name]...)...)})
	if len(reviews["image"]) > 0 {
		stages = append(stages, &TestStage{Name: "image", Status: "failed", Message: message(reviews["image"]...)})
	}
	rows[id] = &SourceQuality{Status: sum.Status, Score: sum.Score, CheckedAt: report.CheckedAt, Millis: report.Millis, Samples: max(1, sum.Attempts), Successes: sum.Readable, Stages: stages, Evidence: "benchmark:" + report.Version}
	return writeSourceQualities(rows, id)
}

// writeSourceQualities is called under qualityMu. Keep the
// same bounded SQLite setting so existing catalog/ranking clients stay usable.
func writeSourceQualities(rows map[string]*SourceQuality, currentID string) error {
	for len(rows) > 512 {
		oldest := ""
		stamp := time.Now().Unix() + 1
		for id, row := range rows {
			if id != currentID && row.CheckedAt < stamp {
				oldest, stamp = id, row.CheckedAt
			}
		}
		if oldest == "" {
			break
		}
		delete(rows, oldest)
	}
	encoded, err := json.Marshal(rows)
	if err != nil {
		return err
	}
	if err = db.SetSetting(sourceQualitySetting, string(encoded)); err != nil {
		return apperror.Wrap(apperror.Storage, err, "保存源质量评估失败")
	}
	return nil
}
