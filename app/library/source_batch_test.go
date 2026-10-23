package library

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/library/quality"
)

func TestBatchSearchOnlyCapabilityAndSharedEndpointProbe(t *testing.T) {
	var home atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			home.Add(1)
			fmt.Fprint(w, "home")
			return
		}
		fmt.Fprint(w, `<ul class="books"><li><a href="/book/1">三国校准</a><span>作者</span></li></ul>`)
	}))
	defer server.Close()
	var rows []map[string]any
	for _, name := range []string{"one", "two"} {
		rows = append(rows, map[string]any{"bookSourceName": name, "bookSourceUrl": server.URL, "searchUrl": "/search?q={{key}}", "ruleSearch": map[string]any{"bookList": ".books li", "name": "a@text", "bookUrl": "a@href", "author": "span@text"}, "ruleContent": map[string]any{"content": "<js>not executed</js>"}})
	}
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := ParseBatchSources(raw, "novel", "")
	if err != nil || len(sources) != 2 {
		t.Fatal(sources, err)
	}
	for _, s := range sources {
		if !s.CanSearch || s.CanRead {
			t.Fatal("partial search became a full adapter", s)
		}
	}
	c := quality.Corpus{Version: quality.Version, UpdatedAt: "2026-10-21", Works: []quality.Work{{ID: "cal", Kind: "novel", Title: "三国校准", Authors: []string{"作者"}, Tags: []string{"历史"}, Language: "zh", Layout: "text", Edition: "原著", Selection: "test fixture", References: []quality.Reference{{URL: server.URL + "/book/1", CheckedAt: "2026-10-21", Role: "metadata"}}}}}
	x, err := quality.Compile(c)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewBatchEngine(x, BatchOptions{ProbeTimeout: time.Second, OperationTimeout: time.Second, Queries: 1, Matches: 1, MaxDeepSources: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	stats, err := quality.Dispatch(context.Background(), sources, 5, e.Execute, func(s *BatchSource, r BatchResult) error {
		if r.State != "search_matched" || r.Quality != nil || r.Discovery == nil || len(r.Discovery.MatchedIDs) != 1 {
			return fmt.Errorf("partial source got wrong result: %+v", r)
		}
		return nil
	})
	if err != nil || stats.Completed != 2 || home.Load() != 1 || e.Stats().Reused != 1 || e.NetworkStats().PeakInFlight > 5 {
		t.Fatal(stats, e.Stats(), home.Load(), err)
	}
}
func TestExtensionMetadataPreservesRemoteLanguageIdentity(t *testing.T) {
	raw := `[{"packageName":"example.extension","sources":[{"id":"1","name":"same","homeUrl":"https://example.org","language":"en"},{"id":"2","name":"same","homeUrl":"https://example.org","language":"ja"}]}]`
	rows, err := ParseBatchSources([]byte(raw), "comic", "")
	if err != nil || len(rows) != 2 || rows[0].ID == rows[1].ID || rows[0].Language == rows[1].Language {
		t.Fatal(rows, err)
	}
}

func TestHomepageOnlyDoesNotCallNetworkFailuresInspected(t *testing.T) {
	x, err := quality.DefaultCorpus()
	if err != nil {
		t.Fatal(err)
	}
	index, err := quality.Compile(x)
	if err != nil {
		t.Fatal(err)
	}
	e, err := NewBatchEngine(index, BatchOptions{ProbeTimeout: time.Second, OperationTimeout: time.Second, Queries: 1, Matches: 1, HomepagesOnly: true, InspectHomepage: true})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	// An already completed origin flight isolates classification from live DNS.
	done := make(chan struct{})
	close(done)
	e.probes["https://unreachable.example"] = &probeFlight{done: done, result: EndpointProbe{Status: "network_unknown"}}
	r := e.Execute(context.Background(), &BatchSource{URL: "https://unreachable.example", Kind: "novel"})
	if r.State != "homepage_unavailable" || r.Quality != nil {
		t.Fatal(r)
	}
}
