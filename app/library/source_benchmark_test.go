package library

import (
	"strings"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/library/quality"
)

func TestBenchmarkSummaryPersistsWithoutImportAndRanksAboveQuickProbe(t *testing.T) {
	s := setupLibraryTest(t)
	initial, err := db.GetSetting(sourceQualitySetting)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.SetSetting(sourceQualitySetting, initial); err != nil {
			t.Error(err)
		}
	})
	id := strings.Repeat("b", 64)
	report := &quality.Report{Version: quality.Version, CheckedAt: time.Now().Unix(), Source: quality.Source{ID: id, Kind: "novel"}, Summary: quality.Summary{Status: "partial", Score: 70, Confidence: "limited", Queries: 9, Matched: 3, Verified: 3, Attempts: 9, Readable: 6, Restricted: 3}}
	if err := s.saveBenchmarkQuality(id, report); err != nil {
		t.Fatal(err)
	}
	rows, err := s.loadSourceQualities()
	if err != nil {
		t.Fatal(err)
	}
	q := rows[id]
	if q == nil || q.Score != 70 || q.Successes != 6 || q.Samples != 9 || q.Evidence != "benchmark:"+quality.Version {
		t.Fatal(q)
	}
	quick := &SourceCandidate{Compatible: true, Name: "quick", Quality: &SourceQuality{Status: "healthy", Score: 100, CheckedAt: time.Now().Unix(), Evidence: "local"}}
	deep := &SourceCandidate{Compatible: true, Name: "benchmark", Quality: q}
	candidates := []*SourceCandidate{quick, deep}
	rankSourceCandidates(candidates)
	if candidates[0] != deep {
		t.Fatal("first-chapter score hid stronger evidence")
	}
	report.Summary.Status = "inconclusive"
	report.Summary.Score = 0
	if err = s.saveBenchmarkQuality(id, report); err != nil {
		t.Fatal(err)
	}
	rows, err = s.loadSourceQualities()
	if err != nil || rows[id].Score != 70 {
		t.Fatal("no-match run erased content evidence", rows[id], err)
	}
	deep.Quality.CheckedAt = time.Now().Add(-25 * time.Hour).Unix()
	rankSourceCandidates(candidates)
	if candidates[0] != quick {
		t.Fatal("stale benchmark promoted above fresh evidence")
	}
	if q, err = s.saveSourceQuality(id, &TestOutcome{OK: true}); err != nil || q.Evidence != "benchmark:"+quality.Version || q.Samples != 9 {
		t.Fatal("quick success erased richer evidence", q, err)
	}
	if q, err = s.saveSourceQuality(id, &TestOutcome{Stages: []*TestStage{{Name: StageSearch, Status: "failed"}}}); err != nil || q.Evidence != "local" || q.Samples != 1 || q.Successes != 0 {
		t.Fatal("mixed chapter counts with quick-run history", q, err)
	}
}
