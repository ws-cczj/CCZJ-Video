package provider

import (
	"context"
	"net/url"
	"strings"

	"cczjVideo/app/apperror"
)

// ComicBrowse reads actual catalog routes. It does not fabricate search terms
// to fill the home page; reading still uses the source's declared operations.
func (p *Pipeline) ComicBrowse(ctx context.Context, apiURL, channel string) (*ItemPage, error) {
	if p.Doc.Kind != "comic" {
		return nil, apperror.New(apperror.Unsupported, "这个源不支持漫画浏览")
	}
	if channel == "honkai" && p.Doc.Adapter == "bh3-web" || channel == "terra" && p.Doc.Adapter == "terra-web" {
		return p.Search(ctx, apiURL, "", 1, "")
	}
	var origin string
	var op Operation
	switch channel {
	case "dongman":
		origin = "https://www.dongmanmanhua.cn"
		op = Operation{Path: "/", Format: "html", List: &Selector{Query: "a.card_item"}, Fields: map[string]Selector{
			"title": {Query: ".card_front .subj", Attr: "text"}, "url": {Attr: "href"}, "cover": {Query: "img", Attr: "src"},
			"author": {Query: ".author", Attr: "text"}, "summary": {Query: ".summary", Attr: "text"}, "genres": {Query: ".genre", Attr: "text"},
		}}
	case "guazi":
		origin = "https://www.guazimanhua.com"
		if p.Doc.Operations.Search == nil {
			return nil, apperror.New(apperror.Unsupported, "漫画浏览源缺少目录配置")
		}
		op = *p.Doc.Operations.Search
		op.Path = "/category.php"
		op.Params = map[string]string{"sort": "hits"}
	default:
		return nil, apperror.New(apperror.Validation, "漫画浏览分类无效")
	}
	if strings.TrimRight(apiURL, "/") != origin {
		return nil, apperror.New(apperror.Unsupported, "漫画浏览来源与目录不匹配")
	}
	response, err := p.Fetcher.Fetch(ctx, p.Doc, &op, origin, Target{Vars: map[string]string{VarPage: "1"}})
	if err != nil {
		return nil, err
	}
	items, _, err := ExtractItems(p.Doc, &op, response)
	if err != nil {
		return nil, err
	}
	out := &ItemPage{Items: []*Item{}}
	seen := map[string]bool{}
	for _, item := range items {
		address, parseErr := url.Parse(item.URL)
		if parseErr != nil || address.Scheme+"://"+address.Host != origin || item.RemoteID == "" || seen[item.RemoteID] {
			continue
		}
		seen[item.RemoteID] = true
		out.Items = append(out.Items, item)
		if len(out.Items) >= 80 {
			break
		}
	}
	if len(out.Items) == 0 {
		return nil, apperror.New(apperror.Corrupt, "漫画目录没有返回有效作品")
	}
	return out, nil
}
