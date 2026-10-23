package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func officialTestPipeline(t *testing.T, adapter string, pages map[string]string) *Pipeline {
	t.Helper()
	doc, err := ParseDocument(fmt.Sprintf(`{"version":3,"kind":"comic","adapter":%q,"reverse":%t}`, adapter, adapter == "bh3-web"), "comic")
	if err != nil {
		t.Fatal(err)
	}
	f := NewFetcher()
	t.Cleanup(f.Close)
	f.client.Transport = fanqieTransport(func(r *http.Request) (*http.Response, error) {
		body, ok := pages[r.URL.Path]
		if !ok {
			return nil, fmt.Errorf("unexpected request %s", r.URL)
		}
		mime := "application/json"
		if strings.HasPrefix(body, "<") {
			mime = "text/html"
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {mime}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	return NewPipeline(doc, f, Limits{})
}
func TestOfficialBH3ReadingUsesStableChapterIDsAndChronologicalOrder(t *testing.T) {
	p := officialTestPipeline(t, "bh3-web", map[string]string{
		"/book":                  `<a href="/book/1001"><div class="container" id="1001"><img src="/cover.jpg"><div class="container-title">测试漫画</div><div class="container-description">源站提供的简介</div></div></a>`,
		"/book/1001":             `<div class="title">测试漫画</div><div class="detail_info1">简介</div><img class="cover" src="/cover.jpg">`,
		"/book/1001/get_chapter": `[{"title":"第二话","bookid":"1001","chapterid":"2"},{"title":"第一话","bookid":"1001","chapterid":"1"}]`,
		"/book/1001/1":           `<img class="lazy comic_img" data-original="https://cdn.example/one.jpg"><img class="lazy comic_img" data-original="https://cdn.example/two.jpg">`,
	})
	ctx := context.Background()
	found, err := p.Search(ctx, bh3Origin, "测试", 1, "")
	if err != nil || len(found.Items) != 1 {
		t.Fatalf("search %+v %v", found, err)
	}
	if found.Items[0].Summary != "源站提供的简介" {
		t.Fatal("catalog lost official summary", found.Items[0])
	}
	book, err := p.Detail(ctx, bh3Origin, found.Items[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	toc, err := p.Toc(ctx, bh3Origin, book)
	if err != nil || len(toc) != 2 || toc[0].Title != "第一话" {
		t.Fatalf("toc %+v %v", toc, err)
	}
	pages, err := p.Pages(ctx, bh3Origin, toc[0])
	if err != nil || len(pages) != 2 || pages[1].ImageURL != "https://cdn.example/two.jpg" {
		t.Fatalf("pages %+v %v", pages, err)
	}
	empty, err := p.Search(ctx, bh3Origin, "不存在", 1, "")
	if err != nil || len(empty.Items) != 0 {
		t.Fatal(empty, err)
	}
	if _, err = p.Detail(ctx, bh3Origin, "https://example.org/book/1001"); err == nil {
		t.Fatal("accepted foreign origin")
	}
}
func TestTerraOfficialComicsFetchAllTopicsAndOrderedPages(t *testing.T) {
	p := officialTestPipeline(t, "terra-web", map[string]string{
		"/api/comic":                        `{"code":0,"data":[{"cid":"7746","title":"测试漫画","authors":["作者"]}]}`,
		"/api/comic/7746":                   `{"code":0,"data":{"cid":"7746","title":"测试漫画","episodes":[{"cid":"8623","shortTitle":"01","title":"第一话"}]}}`,
		"/api/comic/7746/episode/8623":      `{"code":0,"data":{"pageInfos":[{},{}]}}`,
		"/api/comic/7746/episode/8623/page": `{"code":0,"data":{"url":"https://res.example/one.jpg"}}`,
	})
	ctx := context.Background()
	found, err := p.Search(ctx, terraOrigin, "测试", 1, "")
	if err != nil || len(found.Items) == 0 {
		t.Fatal(found, err)
	}
	book, err := p.Detail(ctx, terraOrigin, found.Items[0].URL)
	if err != nil {
		t.Fatal(err)
	}
	toc, err := p.Toc(ctx, terraOrigin, book)
	if err != nil || len(toc) != 1 {
		t.Fatal(toc, err)
	}
	pages, err := p.Pages(ctx, terraOrigin, toc[0])
	if err != nil || len(pages) != 2 || pages[1].Index != 1 {
		t.Fatal(pages, err)
	}
}
