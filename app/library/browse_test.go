package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestNovelBrowseFirstUseCreatesOnlyOneTextSourceAndRespectsDisable(t *testing.T) {
	s := setupLibraryTest(t)
	first, err := s.defaultBrowseSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.defaultBrowseSource(context.Background())
	if err != nil || first.source.SourceKey != second.source.SourceKey {
		t.Fatal(second, err)
	}
	comic, err := db.GetSourcesByKind(model.SourceKindComic)
	if err != nil || len(comic) != 0 {
		t.Fatal(comic, err)
	}
	if err := s.SetSourceEnabled(first.source.SourceKey, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.defaultBrowseSource(context.Background()); !apperror.Is(err, apperror.Unavailable) {
		t.Fatal("disabled source was replaced", err)
	}
}

func TestNovelBrowseCacheIsScopedAndSurvivesWithoutNetwork(t *testing.T) {
	s := setupLibraryTest(t)
	resolved, err := s.defaultBrowseSource(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	key := resolved.source.SourceKey
	ids, err := db.UpsertLibraryItems(model.SourceKindNovel, key, []*model.LibraryItem{{RemoteID: "123", RemoteURL: "https://fanqienovel.com/page/123", Title: "已有书目"}}, true)
	if err != nil {
		t.Fatal(err)
	}
	page := NovelBrowsePage{ChannelID: "history", SourceName: "番茄小说", FetchedAt: time.Now().Unix(), Items: []*Item{{ID: ids[0], Kind: "novel", SourceKey: key, RemoteID: "123", Title: "已有书目", Available: true}}}
	raw, err := json.Marshal(struct {
		Version string `json:"version"`
		NovelBrowsePage
	}{resolved.version, page})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting("novel.browse.v1.history", string(raw)); err != nil {
		t.Fatal(err)
	}
	got, err := s.BrowseNovels(context.Background(), "history", "", false)
	if err != nil || !got.FromCache || len(got.Items) != 1 || got.Stale {
		t.Fatal(got, err)
	}
	if decodeBrowseCache(string(raw), "another-source", resolved.version, "history") != nil {
		t.Fatal("cross-source cache reused")
	}
	position, _ := json.Marshal(browseCursor{SourceKey: key, Version: resolved.version, Channel: "history", Offset: 20})
	_, err = s.BrowseNovels(context.Background(), "popular", base64.RawURLEncoding.EncodeToString(position), false)
	if !apperror.Is(err, apperror.Conflict) {
		t.Fatal("cross-channel cursor reused", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.BrowseNovels(cancelled, "history", "", false); err == nil {
		t.Fatal("cancelled browse returned cached success")
	}
}
