package library

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

func TestRefreshingComicPagesReplacesExpiredSignedURLs(t *testing.T) {
	s := setupLibraryTest(t)
	var generation atomic.Int32
	generation.Store(1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<img src="/image?signature=%d">`, generation.Load())
	}))
	defer server.Close()
	var doc provider.Document
	_ = json.Unmarshal([]byte(novelDocument()), &doc)
	doc.Kind = "comic"
	doc.Operations.Content = nil
	doc.Operations.Pages = &provider.Operation{Format: "html", List: &provider.Selector{Query: "img"}, Fields: map[string]provider.Selector{"image_url": {Attr: "src"}}}
	raw, _ := json.Marshal(doc)
	source, err := s.AddSource(model.SourceKindComic, "signed", server.URL, string(raw))
	if err != nil {
		t.Fatal(err)
	}
	ids, err := db.UpsertLibraryItems(model.SourceKindComic, source.SourceKey, []*model.LibraryItem{{RemoteID: "/book", RemoteURL: server.URL + "/book", Title: "Signed"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = db.ReplaceLibraryChapters(ids[0], []*model.LibraryChapter{{RemoteID: "/chapter", RemoteURL: server.URL + "/chapter", Title: "one"}})
	if err != nil {
		t.Fatal(err)
	}
	chapters, _ := db.ListLibraryChapters(ids[0], false)
	first, err := s.ChapterPages(context.Background(), chapters[0].Id)
	if err != nil {
		t.Fatal(err)
	}
	generation.Store(2)
	cached, err := s.ChapterPages(context.Background(), chapters[0].Id)
	if err != nil || !cached.FromCache {
		t.Fatal(cached, err)
	}
	refreshed, err := s.RefreshChapterPages(context.Background(), chapters[0].Id)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.FromCache || refreshed.Pages[0].URL == first.Pages[0].URL {
		t.Fatalf("retry reused expired URL: %+v", refreshed)
	}
	latest, err := s.ChapterPages(context.Background(), chapters[0].Id)
	if err != nil || latest.Pages[0].URL != refreshed.Pages[0].URL {
		t.Fatal(latest, err)
	}
}

func TestReadingTocRefreshIsNearEndAndThrottled(t *testing.T) {
	s := setupLibraryTest(t)
	server := fakeSource()
	defer server.Close()
	addTestSource(t, server, "refresh_window", novelDocument())
	found, err := s.Search(context.Background(), model.SourceKindNovel, "example", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	id := found.Results[0].Items[0].ID
	detail, err := s.RefreshToc(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// A manual/initial refresh starts the same cooldown as an automatic refresh.
	if s.claimTocRefresh(id, detail.Chapters[len(detail.Chapters)-1].ID, detail) {
		t.Fatal("refreshed again inside cooldown")
	}
	other := *detail
	other.Item = &Item{ID: id + 1, Available: true}
	other.Chapters = make([]*Chapter, 6)
	for i := range other.Chapters {
		other.Chapters[i] = &Chapter{ID: 100 + i, Ord: i, Available: true}
	}
	if s.claimTocRefresh(id+1, 100, &other) {
		t.Fatal("refreshed while many chapters remain")
	}
	if !s.claimTocRefresh(id+1, 105, &other) {
		t.Fatal("did not refresh near end")
	}
	if s.claimTocRefresh(id+1, 105, &other) {
		t.Fatal("parallel refresh bypassed cooldown")
	}
}

func TestDetailRedirectKeepsSearchedItemIdentityAndItsCatalog(t *testing.T) {
	s := setupLibraryTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("wd") != "" {
			fmt.Fprint(w, `<ul class="results"><li><a href="/redirect?tracking=1">book</a></li></ul>`)
			return
		}
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "/canonical", http.StatusFound)
			return
		}
		fmt.Fprint(w, `<div class="detail"><h1>book</h1></div><ul class="chapters"><li><a href="/chapter">one</a></li></ul>`)
	}))
	defer server.Close()
	addTestSource(t, server, "redirect_identity", novelDocument())
	found, err := s.Search(context.Background(), model.SourceKindNovel, "book", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	item := found.Results[0].Items[0]
	detail, err := s.RefreshToc(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(detail.Chapters) != 1 || detail.Item.ID != item.ID || detail.Item.RemoteID != item.RemoteID {
		t.Fatalf("redirect orphaned item/catalog: %+v", detail)
	}
}

func TestDetailWithoutAuthorPreservesSearchIdentity(t *testing.T) {
	s := setupLibraryTest(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.URL.Query().Get("wd") != "" {
			if _, err := fmt.Fprint(w, `<ul class="results"><li><a href="/book">book</a><span class="author">作者甲</span></li></ul>`); err != nil {
				t.Error(err)
			}
			return
		}
		if _, err := fmt.Fprint(w, `<div class="detail"><h1>book</h1></div><ul class="chapters"><li><a href="/chapter">one</a></li></ul>`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	addTestSource(t, server, "missing_detail_author", novelDocument())
	found, err := s.Search(context.Background(), model.SourceKindNovel, "book", 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	item := found.Results[0].Items[0]
	detail, err := s.RefreshToc(context.Background(), item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Item.Author != "作者甲" {
		t.Fatalf("detail erased the searched author: %+v", detail.Item)
	}
}
