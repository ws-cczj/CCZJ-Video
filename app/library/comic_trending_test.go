package library

import (
	"cczjVideo/app/db"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestComicTrendCacheKeepsReferencesSeparateFromReadableItems(t *testing.T) {
	s := setupLibraryTest(t)
	var before int
	if err := db.DB().Get(&before, "SELECT COUNT(*) FROM library_items"); err != nil {
		t.Fatal(err)
	}
	key := "comic.trending.v1.japan"
	old, _ := db.GetSetting(key)
	t.Cleanup(func() { _ = db.SetSetting(key, old) })
	page := &ComicTrendPage{Board: "japan", FetchedAt: time.Now().Unix(), Items: []*ComicTrend{}}
	for i := 0; i < 3; i++ {
		page.Items = append(page.Items, &ComicTrend{Title: fmt.Sprintf("热门%d", i), URL: fmt.Sprintf("https://manga.bilibili.com/detail/mc%d", i), Cover: "https://i0.hdslb.com/cover.jpg"})
	}
	raw, _ := json.Marshal(page)
	if err := db.SetSetting(key, string(raw)); err != nil {
		t.Fatal(err)
	}
	s.SetImageURLBuilder(func(upstream, referer, source string) string {
		if source != "" || referer != "https://manga.bilibili.com/" {
			t.Error("ranking inherited a reading source identity")
		}
		return "/current-image-proxy"
	})
	got, err := s.TrendingComics(context.Background(), "japan", false)
	if err != nil || !got.FromCache || got.Stale || got.Items[0].Cover != "/current-image-proxy" {
		t.Fatal(got, err)
	}
	var after int
	if err := db.DB().Get(&after, "SELECT COUNT(*) FROM library_items"); err != nil || after != before {
		t.Fatal("rankings became readable items", before, after, err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = s.TrendingComics(cancelled, "japan", false); err == nil {
		t.Fatal("cancelled cache read continued")
	}
	if validComicTrend(page, "china") {
		t.Fatal("cache escaped board")
	}
	page.FetchedAt = time.Now().Add(time.Hour).Unix()
	if validComicTrend(page, "japan") {
		t.Fatal("future cache accepted")
	}
}
