package library

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestSearchPageKeepsOpaqueURLsAndStopsCompletedSources(t *testing.T) {
	s := setupLibraryTest(t)
	var mu sync.Mutex
	var seen []string
	const continuation = "cursor=two&pg=opaque&wd=server&sig=a%2Bb"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.RequestURI())
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Path == "/a" && r.URL.Query().Get("cursor") != "" && r.URL.RawQuery != continuation {
			http.Error(w, "continuation query was modified", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(w, `<ul class="results"><li><a href="/book/%s-%s">作品</a></li></ul>`, strings.TrimPrefix(r.URL.Path, "/"), r.URL.Query().Get("cursor"))
		if r.URL.Path == "/a" {
			// Repeated on page two: the loop must end after this page.
			fmt.Fprint(w, `<a class="next" href="/a?cursor=two&amp;pg=opaque&amp;wd=server&amp;sig=a%2Bb">下一页</a>`)
		}
	}))
	defer server.Close()
	document := strings.Replace(novelDocument(), `"search": {`, `"search": {"next":{"query":"a.next","attr":"href"},`, 1)
	addTestSource(t, server, "cursor_a", document)
	addTestSource(t, server, "cursor_b", document)
	for key, path := range map[string]string{"cursor_a": "/a", "cursor_b": "/b"} {
		row, err := db.GetSourceByKey(key)
		if err != nil {
			t.Fatal(err)
		}
		row.ApiUrl = server.URL + path
		if err := db.UpdateSource(row); err != nil {
			t.Fatal(err)
		}
	}
	first, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", "")
	if err != nil || first.Total != 2 || first.NextCursor == "" {
		t.Fatalf("first page: %+v, %v", first, err)
	}
	if _, err := s.SearchPage(context.Background(), model.SourceKindNovel, "different", first.NextCursor); apperror.CodeOf(err) != apperror.Conflict {
		t.Fatalf("accepted a continuation belonging to another query: %v", err)
	}
	second, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", first.NextCursor)
	if err != nil || second.Total != 1 || second.Page != 2 || second.FailedCount != 0 || second.NextCursor != "" {
		t.Fatalf("second page: %+v, %v", second, err)
	}
	if len(second.Results) != 1 || second.Results[0].SourceKey != "cursor_a" || second.Results[0].HasNext {
		t.Fatalf("completed source or repeated cursor continued: %+v", second.Results)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 3 || seen[2] != "/a?"+continuation {
		t.Fatalf("upstream requests: %v", seen)
	}
}

func TestSearchPageRejectsChangedRemovedAndExpiredSources(t *testing.T) {
	s := setupLibraryTest(t)
	server := fakeSource()
	defer server.Close()
	addTestSource(t, server, "cursor_changed", novelDocument())
	row, err := db.GetSourceByKey("cursor_changed")
	if err != nil {
		t.Fatal(err)
	}
	state := searchContinuation{Kind: model.SourceKindNovel, Keyword: "示例", Page: 2, Sources: []sourceSearchState{{Key: row.SourceKey, Version: sourceRevision(row), Page: 2, URL: server.URL + "/not-requested"}}}
	cursor := s.continuations.put(state)
	if err := s.UpdateSource(row.SourceKey, row.Name, server.URL+"/changed", ""); err != nil {
		t.Fatal(err)
	}
	out, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", cursor)
	if err != nil || out.FailedCount != 1 || out.Results[0].ErrorCode != string(apperror.Conflict) || out.NextCursor != "" {
		t.Fatalf("changed source was followed: %+v, %v", out, err)
	}
	if err := s.SetSourceEnabled(row.SourceKey, false); err != nil {
		t.Fatal(err)
	}
	out, err = s.SearchPage(context.Background(), model.SourceKindNovel, "示例", cursor)
	if err != nil || out.FailedCount != 1 || out.Results[0].ErrorCode != string(apperror.Unavailable) {
		t.Fatalf("disabled source was followed: %+v, %v", out, err)
	}
	s.continuations.mu.Lock()
	expired := s.continuations.entries[cursor]
	expired.Created = time.Now().Add(-searchCursorTTL)
	s.continuations.entries[cursor] = expired
	s.continuations.mu.Unlock()
	if _, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", cursor); apperror.CodeOf(err) != apperror.Conflict {
		t.Fatalf("expired continuation accepted: %v", err)
	}
	if _, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", "https://untrusted.example/next"); apperror.CodeOf(err) != apperror.Conflict {
		t.Fatalf("caller-supplied URL accepted as cursor: %v", err)
	}
}

func TestEmptySearchPageCanStillContinue(t *testing.T) {
	s := setupLibraryTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Query().Get("cursor") == "" {
			fmt.Fprint(w, `<ul class="results"></ul><a class="next" href="/?cursor=last">next</a>`)
			return
		}
		fmt.Fprint(w, `<ul class="results"><li><a href="/book/final">最后一页</a></li></ul>`)
	}))
	defer server.Close()
	document := strings.Replace(novelDocument(), `"search": {`, `"search": {"next":{"query":"a.next","attr":"href"},`, 1)
	addTestSource(t, server, "cursor_empty", document)
	first, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", "")
	if err != nil || first.Total != 0 || first.NextCursor == "" {
		t.Fatalf("empty intermediate page: %+v, %v", first, err)
	}
	second, err := s.SearchPage(context.Background(), model.SourceKindNovel, "示例", first.NextCursor)
	if err != nil || second.Total != 1 || second.NextCursor != "" {
		t.Fatalf("final page: %+v, %v", second, err)
	}
}
