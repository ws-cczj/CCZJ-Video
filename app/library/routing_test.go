package library

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
)

func managedFixture(t *testing.T, n int, allMatch bool, authorOnly ...bool) (*Service, *atomic.Int64) {
	t.Helper()
	s := setupLibraryTest(t)
	s.EnableBuiltinRouting()
	s.registry = map[string][]*BatchSource{"novel": {}, "comic": {}}
	t.Cleanup(func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	count := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		i, _ := strconv.Atoi(strings.Trim(r.URL.Path, "/"))
		body := `<ul class="results"></ul>`
		if len(authorOnly) > 0 && authorOnly[0] {
			body = `<ul class="results"><li><a href="/book/2">另一本书</a><span class="author">示例书</span></li></ul>`
		}
		if allMatch || i == n-1 {
			body = `<ul class="results"><li><a href="/book/1">示例书</a><span class="author">作者甲</span></li></ul>`
		}
		if _, err := fmt.Fprint(w, body); err != nil {
			t.Error(err)
		}
	}))
	t.Cleanup(server.Close)
	for i := 0; i < n; i++ {
		key := fmt.Sprintf("route_%057d", i)
		addTestSource(t, server, key, novelDocument())
		source, err := db.GetSourceByKey(key)
		if err != nil {
			t.Fatal(err)
		}
		source.ApiUrl = server.URL + "/" + strconv.Itoa(i)
		if err := db.UpdateSource(source); err != nil {
			t.Fatal(err)
		}
	}
	return s, count
}

func TestManagedSearchAuthorNameDoesNotHideTitleInLaterSources(t *testing.T) {
	s, count := managedFixture(t, 7, false, true)
	out, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例书", "")
	if err != nil || out.SearchedSources != 7 || count.Load() != 7 {
		t.Fatal("author-only match stopped title discovery", out, count.Load(), err)
	}
}

func TestManagedSearchExpandsWhenFirstFiveHaveNoWork(t *testing.T) {
	s, count := managedFixture(t, 7, false)
	out, err := s.SearchPageStream(context.Background(), model.SourceKindNovel, "示例书", "", nil)
	if err != nil || out.Total != 1 || out.SearchedSources != 7 || out.PendingSources != 0 || count.Load() != 7 {
		t.Fatal(out, count.Load(), err)
	}
}

func TestOldInstalledFixedBuiltinIsKeptForBooksButNotSearched(t *testing.T) {
	s, _ := managedFixture(t, 0, false)
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if _, err := fmt.Fprint(w, `<ul class="results"><li><a href="/book">固定作者书单</a></li></ul>`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	id := sourceQualityID("novel", server.URL, novelDocument())
	addTestSource(t, server, id, novelDocument())
	out, err := s.SearchPage(context.Background(), model.SourceKindNovel, "鬼吹灯", "")
	if err != nil || out.Total != 0 || hits.Load() != 0 {
		t.Fatal("old installed fixed list still participated in keyword search", out, hits.Load(), err)
	}
	if row, err := db.GetSourceByKey(id); err != nil || row.Enabled != 1 {
		t.Fatal("existing reading source was deleted or disabled", row, err)
	}
}

func TestManagedSearchLearnsExactHitsBeforeReading(t *testing.T) {
	s, _ := managedFixture(t, 2, false)
	ctx := context.Background()
	out, err := s.SearchPage(ctx, model.SourceKindNovel, "示例书", "")
	if err != nil || out.Total != 1 {
		t.Fatal(out, err)
	}
	rows, err := db.GetEnabledSourcesByKind(model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	hit := rows[1].SourceKey
	if err := s.orderSearchSources(ctx, model.SourceKindNovel, "示例书", rows); err != nil {
		t.Fatal(err)
	}
	if rows[0].SourceKey != hit {
		t.Fatal("an observed exact search hit was ignored until a chapter was read", rows[0].Name)
	}
}

func TestManagedSearchDefersFailedUnknownSourceWithoutDroppingIt(t *testing.T) {
	s, _ := managedFixture(t, 2, false)
	ctx := context.Background()
	rows, err := db.GetEnabledSourcesByKind(model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	failedKey := rows[0].SourceKey
	id := sourceQualityID("novel", rows[0].ApiUrl, rows[0].StrategyConfig)
	if err := db.RecordReadingSearchHealth(ctx, model.SourceKindNovel, id, "TIMEOUT"); err != nil {
		t.Fatal(err)
	}
	if err := s.orderSearchSources(ctx, model.SourceKindNovel, "unseen", rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[1].SourceKey != failedKey {
		t.Fatal("failed source was neither deferred nor retained", rows)
	}
	if err := db.RecordReadingSearchHealth(ctx, model.SourceKindNovel, id, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.orderSearchSources(ctx, model.SourceKindNovel, "unseen", rows); err != nil || rows[0].SourceKey != failedKey {
		t.Fatal("recovered source did not return", rows, err)
	}
}

func TestManagedCancelledSearchDoesNotCreateFailureEvidence(t *testing.T) {
	s, _ := managedFixture(t, 1, false)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-r.Context().Done() }))
	defer server.Close()
	sources, err := db.GetSourcesByKind(model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	sources[0].ApiUrl = server.URL
	if err := db.UpdateSource(sources[0]); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := s.SearchPage(ctx, model.SourceKindNovel, "示例书", ""); done <- err }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("search did not reach upstream")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled search succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("search did not cancel")
	}
	rows, err := db.ReadReadingSearchHealth(context.Background(), model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if _, ok := rows[sourceQualityID("novel", source.ApiUrl, source.StrategyConfig)]; ok {
			t.Fatal("user cancellation became a source failure")
		}
	}
}

func TestNoReadingAttemptsRemainUnmeasured(t *testing.T) {
	report := quality.Report{Version: quality.Version, Source: quality.Source{ID: "candidate", Kind: "novel"}, Summary: quality.Summary{Score: 0, Status: "inconclusive", Confidence: "insufficient"}}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	e := checkEvidence(db.ReadingSourceCheck{DeepAt: time.Now().Unix(), SummaryJSON: string(raw)}, "novel", "candidate")
	if e.Score != -1 {
		t.Fatal("no chapter/page was attempted, but the UI advertises a measured reading score", e)
	}
}

func TestMaintenanceChecksUsableAdaptersBeforeMetadataOnlyEntries(t *testing.T) {
	s := setupLibraryTest(t)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan string, 1)
	serve := func(label string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case first <- label:
			default:
			}
			if _, err := fmt.Fprint(w, `<ul class="results"></ul>`); err != nil {
				t.Error(err)
			}
			cancel()
		}))
	}
	server := serve("adapted")
	defer server.Close()
	metadata := serve("metadata")
	defer metadata.Close()
	raw, _ := json.Marshal([]map[string]any{{"name": "adapted", "apiUrl": server.URL, "kind": "novel", "document": json.RawMessage(novelDocument())}})
	adapted, err := ParseBatchSources(raw, "novel", "test")
	if err != nil || len(adapted) != 1 {
		t.Fatal(adapted, err)
	}
	s.registry = map[string][]*BatchSource{"novel": append([]*BatchSource{{ID: "metadata", Name: "metadata", Kind: "novel", URL: metadata.URL}}, adapted...)}
	_ = s.auditSources(ctx, model.SourceKindNovel)
	select {
	case path := <-first:
		if path != "adapted" {
			t.Fatal("metadata scan delayed the sources used by real search", path)
		}
	default:
		t.Fatal("no source checked")
	}
}
func TestManagedSearchContinuationRetainsUnsearchedSources(t *testing.T) {
	s, count := managedFixture(t, 12, true)
	first, err := s.SearchPageStream(context.Background(), model.SourceKindNovel, "示例书", "", nil)
	if err != nil || first.Total != 5 || first.PendingSources != 7 || first.NextCursor == "" {
		t.Fatal(first, err)
	}
	second, err := s.SearchPageStream(context.Background(), model.SourceKindNovel, "示例书", first.NextCursor, nil)
	if err != nil || second.Total != 5 || second.PendingSources != 2 || count.Load() != 10 {
		t.Fatal(second, count.Load(), err)
	}
	seen := map[string]bool{}
	for _, r := range first.Results {
		seen[r.SourceKey] = true
	}
	for _, r := range second.Results {
		if seen[r.SourceKey] {
			t.Fatal("repeated already-searched source", r.SourceKey)
		}
	}
}
func TestLowScoreExactReadableWorkOutranksHighGlobalScore(t *testing.T) {
	s, _ := managedFixture(t, 2, false)
	ctx := context.Background()
	rows, err := db.GetSourcesByKind(model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	for i, source := range rows {
		score := 90
		if i == 1 {
			score = 12
		}
		id := sourceQualityID("novel", source.ApiUrl, source.StrategyConfig)
		report := quality.Report{Version: quality.Version, Source: quality.Source{ID: id, Kind: "novel"}, Summary: quality.Summary{Score: score, Confidence: "limited", Status: "healthy", Readable: 6, Attempts: 6, Matched: 2, Verified: 2}}
		raw, err := json.Marshal(report)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SaveReadingSourceCheck(ctx, db.ReadingSourceCheck{Kind: "novel", SourceID: id, CheckedAt: time.Now().Unix(), DeepAt: time.Now().Unix(), State: "deep_checked", Score: score, Confidence: "limited", SummaryJSON: string(raw)}); err != nil {
			t.Fatal(err)
		}
	}
	low := rows[1]
	ids, err := db.UpsertLibraryItems(model.SourceKindNovel, low.SourceKey, []*model.LibraryItem{{RemoteID: "special", Title: "冷门书", Author: "作者"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := db.ReplaceLibraryChapters(ids[0], []*model.LibraryChapter{{RemoteID: "1", Title: "第1章 起风"}}); err != nil {
		t.Fatal(err)
	}
	chapters, err := db.ListLibraryChapters(ids[0], false)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RecordReadingWorks(ctx, model.SourceKindNovel, low.SourceKey, sourceRevision(low), []db.ReadingWorkEvidence{{ItemID: ids[0], TitleKey: matching.Normalize("冷门书"), AuthorKey: matching.Normalize("作者")}}); err != nil {
		t.Fatal(err)
	}
	if err := db.RecordReadingChapter(ctx, model.SourceKindNovel, chapters[0].Id, sourceRevision(low), "text"); err != nil {
		t.Fatal(err)
	}
	if err := s.orderSearchSources(ctx, model.SourceKindNovel, "冷门书", rows); err != nil || rows[0].SourceKey != low.SourceKey {
		t.Fatal(rows, err)
	}
	comicRows, err := db.KnownReadingRoutes(ctx, model.SourceKindComic, matching.Normalize("冷门书"))
	if err != nil || len(comicRows) != 0 {
		t.Fatal("novel evidence leaked into manga", comicRows, err)
	}
}
func TestForegroundPreemptsAuditAndCancellationReleasesOwner(t *testing.T) {
	var p readingPriority
	ctx, cancel, err := p.background(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	release := p.begin()
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("foreground failed to cancel audit")
	}
	wait, stop := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer stop()
	if _, _, err := p.background(wait); err == nil {
		t.Fatal("audit entered during foreground work")
	}
	release()
	next, closeNext, err := p.background(context.Background())
	if err != nil || next.Err() != nil {
		t.Fatal(err)
	}
	closeNext()
}
func TestLightAuditDoesNotOverwriteDeepScoreOrMixModes(t *testing.T) {
	s := setupLibraryTest(t)
	ctx := context.Background()
	full := db.ReadingSourceCheck{Kind: "novel", SourceID: "same-id", CheckedAt: time.Now().Unix(), DeepAt: time.Now().Unix(), State: "deep_checked", Score: 81, Confidence: "limited", SummaryJSON: `{"kept":true}`}
	if err := db.SaveReadingSourceCheck(ctx, full); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveReadingSourceCheck(ctx, db.ReadingSourceCheck{Kind: "novel", SourceID: "same-id", CheckedAt: time.Now().Unix(), State: "network_unknown", Score: -1, Confidence: "insufficient"}); err != nil {
		t.Fatal(err)
	}
	rows, err := db.ReadReadingSourceChecks(ctx, model.SourceKindNovel)
	if err != nil || rows["same-id"].Score != 81 || rows["same-id"].SummaryJSON != full.SummaryJSON {
		t.Fatal(rows, err)
	}
	comicRows, err := db.ReadReadingSourceChecks(ctx, model.SourceKindComic)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := comicRows["same-id"]; exists {
		t.Fatal("cross-mode score")
	}
	if err := s.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestModeAuditRecordsRealEndpointWithoutInventingContentScore(t *testing.T) {
	s := setupLibraryTest(t)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if _, err := fmt.Fprint(w, "metadata source homepage"); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	id := sourceQualityID("novel", server.URL, "")
	s.registry = map[string][]*BatchSource{"novel": {{ID: id, Name: "metadata", Kind: "novel", URL: server.URL}}}
	if err := s.auditSources(context.Background(), model.SourceKindNovel); err != nil {
		t.Fatal(err)
	}
	checks, err := db.ReadReadingSourceChecks(context.Background(), model.SourceKindNovel)
	if err != nil || checks[id].State != "adapter_required" || checks[id].Score != -1 || checks[id].DeepAt != 0 || hits.Load() != 1 {
		t.Fatal(checks[id], hits.Load(), err)
	}
	comicChecks, err := db.ReadReadingSourceChecks(context.Background(), model.SourceKindComic)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := comicChecks[id]; ok {
		t.Fatal("novel audit polluted manga ranking")
	}
}

func TestAuditPreemptionIsNotSavedAsFailedSource(t *testing.T) {
	s := setupLibraryTest(t)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	entered := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { entered <- struct{}{}; <-r.Context().Done() }))
	defer server.Close()
	id := sourceQualityID("novel", server.URL, "")
	s.registry = map[string][]*BatchSource{"novel": {{ID: id, Name: "preempt", Kind: "novel", URL: server.URL}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.auditSources(ctx, model.SourceKindNovel) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("audit did not start")
	}
	release := s.priority.begin()
	defer release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("audit did not stop")
	}
	checks, err := db.ReadReadingSourceChecks(context.Background(), model.SourceKindNovel)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := checks[id]; ok {
		t.Fatal("foreground preemption became source failure")
	}
}
