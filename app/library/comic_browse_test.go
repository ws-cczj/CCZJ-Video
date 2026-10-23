package library

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestComicBrowseCreatesOnlyOfficialComicSourcesAndRespectsDisable(t *testing.T) {
	s := setupLibraryTest(t)
	if _, err := s.defaultComicBrowseSource(context.Background(), "made-up"); !apperror.Is(err, apperror.Validation) {
		t.Fatal(err)
	}
	first, err := s.defaultComicBrowseSource(context.Background(), "honkai")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.defaultComicBrowseSource(context.Background(), "honkai")
	if err != nil || again.source.SourceKey != first.source.SourceKey {
		t.Fatal(again, err)
	}
	terra, err := s.defaultComicBrowseSource(context.Background(), "terra")
	if err != nil || terra.source.SourceKey == first.source.SourceKey {
		t.Fatal(terra, err)
	}
	for _, kind := range []model.SourceKind{model.SourceKindNovel, model.SourceKindVideo} {
		rows, err := db.GetSourcesByKind(kind)
		if err != nil || len(rows) != 0 {
			t.Fatal(kind, rows, err)
		}
	}
	if err := s.SetSourceEnabled(first.source.SourceKey, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BrowseComics(context.Background(), "honkai", false); !apperror.Is(err, apperror.Unavailable) {
		t.Fatal("disabled source was replaced", err)
	}
	rows, err := db.GetSourcesByKind(model.SourceKindComic)
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
}

func TestComicBrowseCachedRefsRebuildLocalIDsAndProxyURLs(t *testing.T) {
	s := setupLibraryTest(t)
	resolved, err := s.defaultComicBrowseSource(context.Background(), "honkai")
	if err != nil {
		t.Fatal(err)
	}
	key := resolved.source.SourceKey
	ids, err := db.UpsertLibraryItems(model.SourceKindComic, key, []*model.LibraryItem{{RemoteID: "/book/1001", RemoteURL: "https://comic.bh3.com/book/1001", Title: "真实缓存漫画", Cover: "https://comic.bh3.com/cover.jpg"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	page := &ComicBrowsePage{ChannelID: "honkai", FetchedAt: time.Now().Unix(), Items: []*Item{{ID: ids[0] + 999, Kind: "comic", SourceKey: key, RemoteID: "/book/1001", Title: "旧快照", Cover: "http://old-host/proxy"}}}
	raw, err := json.Marshal(struct {
		Version string `json:"version"`
		*ComicBrowsePage
	}{resolved.version, page})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("comic.browse.v1.honkai", string(raw)); err != nil {
		t.Fatal(err)
	}
	s.SetImageURLBuilder(func(upstream, referer, source string) string { return "/current-proxy/" + source })
	got, err := s.BrowseComics(context.Background(), "honkai", false)
	if err != nil || !got.FromCache || got.Stale || len(got.Items) != 1 {
		t.Fatal(got, err)
	}
	if got.Items[0].ID != ids[0] || got.Items[0].Title != "真实缓存漫画" || got.Items[0].Cover != "/current-proxy/"+key {
		t.Fatal(got.Items[0])
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.BrowseComics(cancelled, "honkai", false); err == nil {
		t.Fatal("cancelled request returned cache")
	}
	if decodeComicBrowseCache(string(raw), "another-source", resolved.version, "honkai") != nil || decodeComicBrowseCache(string(raw), key, "other-version", "honkai") != nil || decodeComicBrowseCache(string(raw), key, resolved.version, "terra") != nil {
		t.Fatal("cache escaped source revision or channel")
	}
}

func TestComicBrowseCacheRejectsCorruptOrCrossModeItems(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*ComicBrowsePage)
	}{
		{"null item", func(p *ComicBrowsePage) { p.Items[0] = nil }},
		{"novel", func(p *ComicBrowsePage) { p.Items[0].Kind = "novel" }},
		{"future", func(p *ComicBrowsePage) { p.FetchedAt = time.Now().Add(time.Hour).Unix() }},
		{"duplicate", func(p *ComicBrowsePage) { p.Items = append(p.Items, p.Items[0]) }},
		{"empty identity", func(p *ComicBrowsePage) { p.Items[0].RemoteID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			page := &ComicBrowsePage{ChannelID: "honkai", FetchedAt: time.Now().Unix(), Items: []*Item{{ID: 1, Kind: "comic", SourceKey: "official", RemoteID: "/book/1", Title: "漫画"}}}
			test.edit(page)
			raw, err := json.Marshal(struct {
				Version string `json:"version"`
				*ComicBrowsePage
			}{"v1", page})
			if err != nil {
				t.Fatal(err)
			}
			if decodeComicBrowseCache(string(raw), "official", "v1", "honkai") != nil {
				t.Fatal("corrupt cache accepted")
			}
		})
	}
}

func TestExpandedBrowseSourcesAreReusedAndNotReenabled(t *testing.T) {
	s := setupLibraryTest(t)
	for _, channel := range []string{"guazi", "dongman"} {
		first, err := s.defaultComicBrowseSource(context.Background(), channel)
		if err != nil {
			t.Fatal(err)
		}
		again, err := s.defaultComicBrowseSource(context.Background(), channel)
		if err != nil || first.source.SourceKey != again.source.SourceKey {
			t.Fatalf("%s duplicated source: %v", channel, err)
		}
		if err := s.SetSourceEnabled(first.source.SourceKey, false); err != nil {
			t.Fatal(err)
		}
		if _, err := s.defaultComicBrowseSource(context.Background(), channel); !apperror.Is(err, apperror.Unavailable) {
			t.Fatalf("%s recreated a disabled source: %v", channel, err)
		}
	}
}
