package library

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestSearchContinuationKeepsFailedSourcesVisibleWithoutRetryingThem(t *testing.T) {
	s := setupLibraryTest(t)
	var failures atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			failures.Add(1)
			http.Error(w, "unavailable", 503)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<ul class="results"><li><a href="/book/one">作品</a></li></ul>`)
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `<a class="next" href="/good?cursor=last">next</a>`)
		}
	}))
	defer server.Close()
	document := strings.Replace(novelDocument(), `"search": {`, `"search": {"next":{"query":"a.next","attr":"href"},`, 1)
	for key, path := range map[string]string{"good_page": "/good", "bad_page": "/bad"} {
		addTestSource(t, server, key, document)
		row, err := db.GetSourceByKey(key)
		if err != nil {
			t.Fatal(err)
		}
		row.ApiUrl = server.URL + path
		if err := db.UpdateSource(row); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.SearchPage(context.Background(), model.SourceKindNovel, "query", "")
	if err != nil || first.FailedCount != 1 || first.NextCursor == "" {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	second, err := s.SearchPage(context.Background(), model.SourceKindNovel, "query", first.NextCursor)
	if err != nil || second.FailedCount != 1 || len(second.Results) != 2 || second.NextCursor != "" {
		t.Fatalf("continued page: %+v, %v", second, err)
	}
	if failures.Load() != 1 {
		t.Fatalf("failed source was retried %d times", failures.Load())
	}
}
