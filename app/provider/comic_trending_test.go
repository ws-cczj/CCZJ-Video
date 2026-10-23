package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestComicTrendingUsesRankedCardsAndPreservesBoardOrder(t *testing.T) {
	fetcher := NewFetcher()
	defer fetcher.Close()
	raw := `<a href="/detail/mc99">Search suggestion</a>`
	for _, title := range []string{"航海王", "一拳超人", "间谍过家家", "航海王"} {
		raw += `<div class="_rank-item-content_ABC"><a class="_manga-title_ABC" href="/detail/mc1">` + title + `</a><picture><img src="https://i0.hdslb.com/cover.jpg"></picture><p class="_author-text_ABC">作者</p><p class="_style-text_ABC">热血</p><a href="/mc1/22">第22话</a></div>`
	}
	fetcher.client.Transport = fanqieTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://manga.bilibili.com/ranking/0" {
			t.Fatalf("wrong board: %s", req.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html;charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(raw)), Request: req}, nil
	})
	found, err := ComicTrending(context.Background(), fetcher, "japan")
	if err != nil || len(found) != 3 || found[0].Title != "航海王" || found[1].Title != "一拳超人" {
		t.Fatalf("bad ranking: %+v %v", found, err)
	}
	if _, err = ComicTrending(context.Background(), fetcher, "invalid"); err == nil {
		t.Fatal("invalid board accepted")
	}
	if _, err = extractComicTrending(`<a href="/detail/mc1">Not a ranked card</a>`, "https://manga.bilibili.com"); err == nil {
		t.Fatal("changed markup became an empty success")
	}
}
