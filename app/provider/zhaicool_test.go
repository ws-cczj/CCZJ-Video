package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestZhaicoolCatalogSearchVariesByTitleAndAuthorAndReadsWithoutAdWidgets(t *testing.T) {
	doc, err := ParseDocument(`{"version":3,"kind":"novel","adapter":"zhaicool-web"}`, "novel")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher()
	defer f.Close()
	pages := map[string]string{
		"/":           `<div class="book_box"><a class="bookname" href="/gcd.html">鬼吹灯</a><div class="author"><a>天下霸唱</a></div></div><div class="each"><a class="sbookname" href="/sdyx.html">射雕英雄传</a><div class="sauthor"><a>金庸</a></div></div>`,
		"/gcd.html":   `<h1>鬼吹灯</h1><span class="site"><a href="/author/one.html">天下霸唱</a></span><div class="booklist"><div class="google-ad"><a href="/ad.html">广告</a></div><span><a href="/gcd/1.html">引子</a></span><span><a href="/gcd/2.html">第一章 白纸人</a></span></div>`,
		"/gcd/1.html": `<div class="bookcontent"><ins>广告代码</ins><script>bad()</script><p>第一段正文</p><p>第二段正文</p>站长推荐：<a href="/ad.html">广告推荐</a></div>`,
		"/gcd/2.html": `<div class="bookcontent"><ins class="adsbygoogle"></ins><script>bad()</script>甲段正文<br><br>乙段正文<br>站长推荐：<a href="/ad.html">广告推荐</a></div>`,
	}
	var requests int
	f.client.Transport = fanqieTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"text/html; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(pages[r.URL.Path]))}, nil
	})
	p := NewPipeline(doc, f, Limits{})
	ctx := context.Background()
	for _, tc := range []struct{ keyword, title string }{{"鬼吹灯", "鬼吹灯"}, {"天下霸唱", "鬼吹灯"}, {"金庸", "射雕英雄传"}, {"不存在", ""}} {
		page, err := p.Search(ctx, zhaicoolOrigin, tc.keyword, 1, "")
		if err != nil {
			t.Fatal(err)
		}
		if tc.title == "" {
			if len(page.Items) != 0 {
				t.Fatal("fixed unfiltered list", page.Items)
			}
		} else if len(page.Items) != 1 || page.Items[0].Title != tc.title {
			t.Fatal(tc, page)
		}
	}
	before := requests
	if page, err := p.Search(ctx, zhaicoolOrigin, "鬼吹灯", 2, ""); err != nil || len(page.Items) != 0 || requests != before {
		t.Fatal("single page catalog created more network paging", page, err)
	}
	item, err := p.Detail(ctx, zhaicoolOrigin, zhaicoolOrigin+"/gcd.html")
	if err != nil || item.Title != "鬼吹灯" || item.Author != "天下霸唱" {
		t.Fatal(item, err)
	}
	toc, err := p.Toc(ctx, zhaicoolOrigin, item)
	if err != nil || len(toc) != 2 || toc[0].Title != "引子" {
		t.Fatal(toc, err)
	}
	text, err := p.Content(ctx, zhaicoolOrigin, toc[0], toc[1].URL)
	if err != nil || len(text.Paragraphs) != 2 || text.Paragraphs[0] != "第一段正文" {
		t.Fatal(text, err)
	}
	br, err := p.Content(ctx, zhaicoolOrigin, toc[1], "")
	if err != nil || len(br.Paragraphs) != 2 || br.Paragraphs[1] != "乙段正文" {
		t.Fatal("BR-based chapters lost paragraphs or included recommendations", br, err)
	}
	if _, err := p.Content(ctx, zhaicoolOrigin, nil, ""); err == nil {
		t.Fatal("missing chapter accepted")
	}
	if _, err := p.Detail(ctx, zhaicoolOrigin, "https://foreign.example/book.html"); err == nil {
		t.Fatal("accepted foreign detail URL")
	}
	if _, err := ParseDocument(`{"version":3,"kind":"comic","adapter":"zhaicool-web"}`, "comic"); err == nil {
		t.Fatal("novel adapter accepted as manga")
	}
}
