package library

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPinyinSearchSendsResolvedChineseTitleToSource(t *testing.T) {
	service := setupLibraryTest(t)
	seen := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.URL.Query().Get("wd")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<ul class="results"><li><a href="/book/west">西游记</a></li></ul>`)
	}))
	defer server.Close()
	addTestSource(t, server, "phonetic_query", novelDocument())
	result, err := service.SearchPage(context.Background(), model.SourceKindNovel, "xiyouji", "")
	if err != nil {
		t.Fatal(err)
	}
	if result.Keyword != "西游记" || result.Total != 1 || result.FailedCount != 0 {
		t.Fatalf("resolved search: %+v", result)
	}
	if got := <-seen; got != "西游记" {
		t.Fatalf("source received %q instead of Chinese title", got)
	}
}

func TestPinyinResolvesKnownTitlesWithinMode(t *testing.T) {
	setupLibraryTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	addTestSource(t, server, "phonetic_local", novelDocument())
	if _, err := db.UpsertLibraryItems(model.SourceKindNovel, "phonetic_local", []*model.LibraryItem{{RemoteID: "new-title", Title: "鬼吹灯"}}, true); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		kind        model.SourceKind
		query, want string
	}{
		{model.SourceKindNovel, "xiyouji", "西游记"},
		{model.SourceKindNovel, "xi you ji", "西游记"},
		{model.SourceKindNovel, "GuiChuiDeng", "鬼吹灯"},
		{model.SourceKindNovel, "西游记", "西游记"},
		{model.SourceKindNovel, "unknown-book-name", "unknown-book-name"},
	} {
		got, err := resolveSearchKeyword(context.Background(), row.kind, row.query)
		if err != nil || got != row.want {
			t.Errorf("%s: %q, %v; want %q", row.query, got, err, row.want)
		}
	}
}
