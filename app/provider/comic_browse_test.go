package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestDongmanBrowseUsesHomepageCardsAndDeduplicates(t *testing.T) {
	fetcher := NewFetcher()
	defer fetcher.Close()
	doc := &Document{Kind: "comic", Headers: map[string]string{"Referer": "https://www.dongmanmanhua.cn/"}}
	fetcher.client.Transport = fanqieTransport(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/" || req.URL.Query().Get("keyword") != "" {
			t.Errorf("browse used search: %s", req.URL)
		}
		if req.Header.Get("Referer") != doc.Headers["Referer"] {
			t.Error("lost declared source headers")
		}
		card := `<a class="card_item" href="//www.dongmanmanhua.cn/story/list?title_no=7"><div class="card_front"><h3 class="subj">真实目录样式</h3><img src="/cover.jpg"><span class="genre">奇幻</span></div><p class="author">作者</p><p class="summary">简介</p></a>`
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html"}}, Body: io.NopCloser(strings.NewReader(card + card + `<a class="card_item" href="https://other.example/story"><div class="card_front"><h3 class="subj">外站</h3></div></a>`))}, nil
	})
	pipeline := NewPipeline(doc, fetcher, Limits{})
	page, err := pipeline.ComicBrowse(context.Background(), "https://www.dongmanmanhua.cn", "dongman")
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "真实目录样式" || page.Items[0].Genres != "奇幻" || page.Items[0].Summary != "简介" {
		t.Fatalf("unexpected cards: %+v", page)
	}
	if _, err := pipeline.ComicBrowse(context.Background(), "https://other.example", "dongman"); err == nil {
		t.Fatal("accepted mismatched catalog host")
	}
}
