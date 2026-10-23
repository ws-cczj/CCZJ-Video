package provider

import (
	"context"
	"net/url"
	"strings"

	"cczjVideo/app/apperror"
)

const zhaicoolOrigin = "https://book.zhaicool.net"

// This site publishes a bounded, multi-author catalog rather than a search
// endpoint. Filter the fetched records, never return the same fixed list for
// every query. Extraction and content pagination keep the shared pipeline.
func (p *Pipeline) zhaicoolSearch(ctx context.Context, keyword string, page int) (*ItemPage, error) {
	if page > 1 {
		return &ItemPage{Items: []*Item{}}, nil
	}
	op := &Operation{Path: "/", Format: "html", List: &Selector{Query: ".book_box, .each"}, Fields: map[string]Selector{
		"title": {Query: "a.bookname, a.sbookname", Attr: "text"}, "url": {Query: "a.bookname, a.sbookname", Attr: "href"},
		"author": {Query: ".author a, .sauthor a", Attr: "text"}, "cover": {Query: ".bookimg img", Attr: "src"}, "summary": {Query: ".booktxt", Attr: "text"},
	}}
	response, err := p.Fetcher.Fetch(ctx, p.Doc, op, zhaicoolOrigin, Target{})
	if err != nil {
		return nil, err
	}
	items, _, err := ExtractItems(p.Doc, op, response)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	found := []*Item{}
	for _, item := range filterCatalogItems(items, keyword, "") {
		if _, err := zhaicoolAddress(item.URL); err != nil || seen[item.RemoteID] {
			continue
		}
		seen[item.RemoteID] = true
		found = append(found, item)
	}
	return &ItemPage{Items: found, Garbled: response.Garbled}, nil
}

func zhaicoolAddress(address string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host != "book.zhaicool.net" || u.User != nil || !strings.HasSuffix(u.Path, ".html") || u.RawQuery != "" || u.Fragment != "" {
		return "", apperror.New(apperror.Validation, "书籍或章节地址不属于当前来源")
	}
	return u.Path, nil
}

func (p *Pipeline) zhaicoolPipeline(address string) (*Pipeline, error) {
	if _, err := zhaicoolAddress(address); err != nil {
		return nil, err
	}
	doc := &Document{Version: DocumentVersion, Kind: "novel", Headers: p.Doc.Headers, Encoding: "utf-8", Operations: Operations{
		Detail: &Operation{Format: "html", Fields: map[string]Selector{"title": {Query: "h1", Attr: "text"}, "author": {Query: ".site a[href*=author]", Attr: "text"}}},
		Toc:    &Operation{Format: "html", List: &Selector{Query: ".booklist > span a[href]"}, Fields: map[string]Selector{"title": {Attr: "text"}, "chapter_url": {Attr: "href"}}},
		Content: &Operation{Format: "html", Text: &Selector{Fallback: []Selector{
			{Concat: []Selector{{Query: ".bookcontent > p", Attr: "text"}}, Separator: "\n"},
			// Older books use BR-separated direct text, with recommendations
			// appended inside the same container after a stable section label.
			{Query: ".bookcontent", Attr: "text", Regex: &Replace{Pattern: `(?s)站长推荐[：:].*`}},
		}}},
	}}
	return NewPipeline(doc, p.Fetcher, p.Limits), nil
}
