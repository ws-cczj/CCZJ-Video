package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
)

const bh3Origin = "https://comic.bh3.com"
const terraOrigin = "https://comic.hypergryph.com"

func (p *Pipeline) officialFetch(ctx context.Context, address string) (*Response, error) {
	return p.Fetcher.Fetch(ctx, p.Doc, &Operation{Format: "html"}, address, Target{URL: address, PreserveQuery: true})
}
func officialPath(address, origin, prefix string) (string, error) {
	u, err := url.Parse(address)
	if err != nil || u.Scheme != "https" || u.Host != strings.TrimPrefix(origin, "https://") || u.User != nil || !strings.HasPrefix(u.Path, prefix) {
		return "", apperror.New(apperror.Validation, "漫画来源地址不匹配")
	}
	for _, segment := range strings.Split(strings.TrimPrefix(u.Path, prefix), "/") {
		if segment == "episode" {
			continue
		}
		if !fanqieNumber.MatchString(segment) {
			return "", apperror.New(apperror.Validation, "漫画作品或章节地址无效")
		}
	}
	return u.Path, nil
}

func (p *Pipeline) bh3Search(ctx context.Context, keyword string, page int) (*ItemPage, error) {
	if page > 1 {
		return &ItemPage{}, nil
	}
	response, err := p.officialFetch(ctx, bh3Origin+"/book")
	if err != nil {
		return nil, err
	}
	op := &Operation{Format: "html", List: &Selector{Query: "a[href*=book]"}, Fields: map[string]Selector{
		"title": {Query: ".container-title", Attr: "text"}, "url": {Query: "div.container", Attr: "id", Regex: &Replace{Pattern: "^", Replacement: "/book/"}}, "cover": {Query: "img", Attr: "src"},
		"summary": {Query: ".container-description", Attr: "text"},
	}}
	items, _, err := ExtractItems(p.Doc, op, response)
	if err != nil {
		return nil, err
	}
	return &ItemPage{Items: filterCatalogItems(items, keyword, "崩坏")}, nil
}
func (p *Pipeline) bh3Detail(ctx context.Context, address string) (*Item, error) {
	path, err := officialPath(address, bh3Origin, "/book/")
	if err != nil {
		return nil, err
	}
	response, err := p.officialFetch(ctx, bh3Origin+path)
	if err != nil {
		return nil, err
	}
	op := &Operation{Format: "html", Fields: map[string]Selector{"title": {Query: "div.title", Attr: "text"}, "cover": {Query: "img.cover", Attr: "src"}, "summary": {Query: "div.detail_info1", Attr: "text"}}}
	item, err := ExtractDetail(p.Doc, op, response)
	if err != nil {
		return nil, err
	}
	item.URL = bh3Origin + path
	item.RemoteID = path
	return item, nil
}
func (p *Pipeline) bh3Toc(ctx context.Context, item *Item) ([]*Chapter, error) {
	path, err := officialPath(item.URL, bh3Origin, "/book/")
	if err != nil {
		return nil, err
	}
	response, err := p.officialFetch(ctx, bh3Origin+path+"/get_chapter")
	if err != nil {
		return nil, err
	}
	var rows []struct {
		Title     string `json:"title"`
		BookID    string `json:"bookid"`
		ChapterID string `json:"chapterid"`
	}
	if err = json.Unmarshal([]byte(response.Text), &rows); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "漫画目录格式无效")
	}
	var out []*Chapter
	for _, row := range rows {
		if !fanqieNumber.MatchString(row.BookID) || !fanqieNumber.MatchString(row.ChapterID) {
			return nil, apperror.New(apperror.Corrupt, "漫画章节标识无效")
		}
		address := bh3Origin + "/book/" + row.BookID + "/" + row.ChapterID
		out = append(out, &Chapter{Title: row.Title, URL: address, RemoteID: RemoteID(address), Access: model.ChapterAccessAvailable})
	}
	if len(out) == 0 {
		return nil, apperror.New(apperror.Corrupt, "漫画目录为空")
	}
	return p.normalizeChapters(out), nil
}
func (p *Pipeline) bh3Pages(ctx context.Context, chapter *Chapter) ([]*Page, error) {
	path, err := officialPath(chapter.URL, bh3Origin, "/book/")
	if err != nil {
		return nil, err
	}
	response, err := p.officialFetch(ctx, bh3Origin+path)
	if err != nil {
		return nil, err
	}
	op := &Operation{Format: "html", List: &Selector{Query: "img.lazy.comic_img"}, Fields: map[string]Selector{"image_url": {Attr: "data-original"}}}
	pages, _, err := ExtractPages(p.Doc, op, response)
	if err != nil {
		return nil, err
	}
	if len(pages) == 0 {
		return nil, apperror.New(apperror.Corrupt, "未取得漫画图片")
	}
	return pages, nil
}

type terraComic struct {
	CID          string   `json:"cid"`
	Title        string   `json:"title"`
	Cover        string   `json:"cover"`
	Authors      []string `json:"authors"`
	Introduction string   `json:"introduction"`
	Episodes     []struct {
		CID        string `json:"cid"`
		Title      string `json:"title"`
		ShortTitle string `json:"shortTitle"`
	} `json:"episodes"`
}

func (p *Pipeline) terraJSON(ctx context.Context, address string, target any) error {
	response, err := p.officialFetch(ctx, address)
	if err != nil {
		return err
	}
	var envelope struct {
		Code int             `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal([]byte(response.Text), &envelope); err != nil {
		return apperror.Wrap(apperror.Corrupt, err, "漫画接口格式无效")
	}
	if envelope.Code != 0 {
		return apperror.New(apperror.Unavailable, "漫画接口暂不可用")
	}
	if err = json.Unmarshal(envelope.Data, target); err != nil {
		return apperror.Wrap(apperror.Corrupt, err, "漫画数据格式无效")
	}
	return nil
}
func terraItem(row terraComic) *Item {
	return &Item{RemoteID: "/api/comic/" + row.CID, Title: row.Title, Cover: row.Cover, Author: strings.Join(row.Authors, " · "), Summary: row.Introduction, URL: terraOrigin + "/api/comic/" + row.CID}
}
func (p *Pipeline) terraSearch(ctx context.Context, keyword string, page int) (*ItemPage, error) {
	if page > 1 {
		return &ItemPage{}, nil
	}
	var items []*Item
	for _, topic := range []string{"terra-historicus", "talos-ii-historicus"} {
		var rows []terraComic
		if err := p.terraJSON(ctx, terraOrigin+"/api/comic?topicKey="+topic, &rows); err != nil {
			return nil, err
		}
		for _, row := range rows {
			if !fanqieNumber.MatchString(row.CID) {
				return nil, apperror.New(apperror.Corrupt, "漫画标识无效")
			}
			items = append(items, terraItem(row))
		}
	}
	return &ItemPage{Items: filterCatalogItems(items, keyword, "明日方舟泰拉终末地")}, nil
}
func (p *Pipeline) terraDetail(ctx context.Context, address string) (*Item, error) {
	path, err := officialPath(address, terraOrigin, "/api/comic/")
	if err != nil {
		return nil, err
	}
	var row terraComic
	if err = p.terraJSON(ctx, terraOrigin+path, &row); err != nil {
		return nil, err
	}
	if !fanqieNumber.MatchString(row.CID) {
		return nil, apperror.New(apperror.Corrupt, "漫画标识无效")
	}
	return terraItem(row), nil
}
func (p *Pipeline) terraToc(ctx context.Context, item *Item) ([]*Chapter, error) {
	path, err := officialPath(item.URL, terraOrigin, "/api/comic/")
	if err != nil {
		return nil, err
	}
	var row terraComic
	if err = p.terraJSON(ctx, terraOrigin+path, &row); err != nil {
		return nil, err
	}
	var out []*Chapter
	for _, episode := range row.Episodes {
		if !fanqieNumber.MatchString(episode.CID) {
			return nil, apperror.New(apperror.Corrupt, "漫画章节标识无效")
		}
		address := terraOrigin + path + "/episode/" + episode.CID
		out = append(out, &Chapter{Title: strings.TrimSpace(episode.ShortTitle + " " + episode.Title), URL: address, RemoteID: RemoteID(address), Access: model.ChapterAccessAvailable})
	}
	if len(out) == 0 {
		return nil, apperror.New(apperror.Corrupt, "漫画目录为空")
	}
	return p.normalizeChapters(out), nil
}
func (p *Pipeline) terraPages(ctx context.Context, chapter *Chapter) ([]*Page, error) {
	path, err := officialPath(chapter.URL, terraOrigin, "/api/comic/")
	if err != nil {
		return nil, err
	}
	var episode struct {
		PageInfos []json.RawMessage `json:"pageInfos"`
	}
	if err = p.terraJSON(ctx, terraOrigin+path, &episode); err != nil {
		return nil, err
	}
	if len(episode.PageInfos) == 0 || len(episode.PageInfos) > 2000 {
		return nil, apperror.New(apperror.Corrupt, "漫画页数无效")
	}
	urls := make([]string, len(episode.PageInfos))
	nonce := time.Now().UnixMilli()
	for i := range urls {
		urls[i] = fmt.Sprintf("%s%s/page?pageNum=%d&_=%d", terraOrigin, path, i+1, nonce)
	}
	op := &Operation{Format: "json", List: &Selector{Path: "data"}, Fields: map[string]Selector{"image_url": {Path: "url"}}}
	batches, err := p.fetchPagesConcurrently(ctx, terraOrigin, op, urls)
	if err != nil {
		return nil, err
	}
	var out []*Page
	for _, batch := range batches {
		if len(batch) != 1 {
			return nil, apperror.New(apperror.Corrupt, "漫画页地址无效")
		}
		batch[0].Index = len(out)
		out = append(out, batch[0])
	}
	return out, nil
}
