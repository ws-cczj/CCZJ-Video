package danmaku

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cczjVideo/app/apperror"
	"golang.org/x/net/html"
)

const (
	leshiProviderID  = "leshi"
	leshiSearchHost  = "so.le.com"
	leshiCatalogHost = "d-api-m.le.com"
	leshiVideoHost   = "www.le.com"
	leshiBarrageHost = "hd-my.le.com"
	leshiSegmentMS   = 300_000
)

var leshiAlbumPath = regexp.MustCompile(`^/tv/([0-9]{1,24})\.html$`)
var leshiTitleYear = regexp.MustCompile(`((?:19|20)[0-9]{2})电视剧['"]`)

type leshiProvider struct{ client *Client }

func (p *leshiProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	query := url.Values{"wd": {title}, "query": {title}, "from": {"pc"}, "ref": {"click"},
		"click_area": {"search_button"}, "is_default_query": {"0"}, "module": {"search_rst_page"}}
	raw, err := p.client.read(ctx, "https://"+leshiSearchHost+"/s?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	page, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, schemaError(err)
	}
	results := []Candidate{}
	seen := map[string]bool{}
	for node := range page.Descendants() {
		if node.Data != "a" || node.Type != html.ElementNode {
			continue
		}
		link, err := url.Parse(htmlAttr(node, "href"))
		if err != nil || link.Host != leshiVideoHost {
			continue
		}
		path := leshiAlbumPath.FindStringSubmatch(link.Path)
		if path == nil || seen[path[1]] {
			continue
		}
		name := plainTitle(htmlAttr(node, "title"))
		if name == "" {
			continue
		}
		for parent := node.Parent; parent != nil; parent = parent.Parent {
			info := htmlAttr(parent, "data-info")
			if info == "" {
				continue
			}
			if !strings.Contains(info, "pid:'"+path[1]+"'") {
				break
			}
			var year string
			if match := leshiTitleYear.FindStringSubmatch(info); match != nil {
				year = match[1]
			}
			results = append(results, Candidate{Provider: leshiProviderID, Ref: path[1], Title: name, Year: year, Kind: "tv"})
			seen[path[1]] = true
			break
		}
		if len(results) >= 100 {
			break
		}
	}
	return results, nil
}

func (p *leshiProvider) Episodes(ctx context.Context, series string) ([]Episode, error) {
	if !decimalID.MatchString(series) {
		return nil, apperror.New(apperror.Validation, "乐视作品标识无效")
	}
	const pageSize = 50
	pages, total := 1, -1
	results := []Episode{}
	for page := 1; page <= pages; page++ {
		query := url.Values{"pid": {series}, "platform": {"pc"}, "page": {strconv.Itoa(page)}, "pagesize": {"50"}, "type": {"1"}}
		root, err := p.client.object(ctx, "https://"+leshiCatalogHost+"/detail/episode?"+query.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if err := business(root["code"], "200"); err != nil {
			return nil, err
		}
		data := obj(root["data"])
		count, err := strconv.Atoi(text(data["total"]))
		rows := arr(data["list"])
		if err != nil || count < 0 || rows == nil || len(rows) > pageSize || text(data["current_page"]) != strconv.Itoa(page) {
			return nil, schemaError(fmt.Errorf("invalid LeTV catalog page"))
		}
		if count > 500 {
			return nil, apperror.New(apperror.Unsupported, "乐视目录超出单次匹配范围")
		}
		if page == 1 {
			total, pages = count, (count+pageSize-1)/pageSize
		}
		if count != total || len(rows) != min(pageSize, count-(page-1)*pageSize) {
			return nil, schemaError(fmt.Errorf("incomplete LeTV catalog"))
		}
		for _, value := range rows {
			row := obj(value)
			vid, name, label := text(row["vid"]), plainTitle(text(row["title"])), text(row["episode"])
			duration, err := secondsMS(text(row["duration"]))
			_, numbered := episodeOrdinal(label)
			if text(row["pid"]) != series || !decimalID.MatchString(vid) || text(row["isyugao"]) != "0" ||
				text(obj(row["video_type"])["180001"]) != "正片" || !numbered || err != nil || duration <= 0 || name == "" || matchExtra.MatchString(name) {
				continue
			}
			results = append(results, Episode{Provider: leshiProviderID, SeriesID: series, EpisodeID: vid, Title: name,
				Number: label, DurationMS: duration, PageURL: "https://" + leshiVideoHost + "/ptv/vplay/" + vid + ".html"})
		}
	}
	return results, nil
}

func (p *leshiProvider) Index(_ context.Context, episode Episode) (Index, error) {
	if episode.DurationMS <= 0 || episode.DurationMS > 86_400_000 {
		return Index{}, schemaError(fmt.Errorf("invalid LeTV episode duration"))
	}
	return regularIndex(episode.DurationMS, leshiSegmentMS), nil
}

func (p *leshiProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !decimalID.MatchString(episode.SeriesID) || !decimalID.MatchString(episode.EpisodeID) || !regularSegment(segment, leshiSegmentMS) {
		return nil, 0, apperror.New(apperror.Validation, "乐视弹幕分片标识无效")
	}
	const callback = "cczj"
	query := url.Values{"vid": {episode.EpisodeID}, "start": {strconv.FormatInt(segment.StartMS/1000, 10)},
		"end": {strconv.FormatInt((segment.EndMS+999)/1000, 10)}, "callback": {callback}}
	raw, err := p.client.read(ctx, "https://"+leshiBarrageHost+"/danmu/list?"+query.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	raw = bytes.TrimSuffix(bytes.TrimSpace(raw), []byte(";"))
	raw, wrapped := bytes.CutPrefix(raw, []byte(callback+"("))
	if !wrapped || !bytes.HasSuffix(raw, []byte(")")) {
		return nil, 0, schemaError(fmt.Errorf("invalid LeTV barrage envelope"))
	}
	root, err := object(raw[:len(raw)-1])
	if err != nil {
		return nil, 0, err
	}
	if err := business(root["code"], "200"); err != nil {
		return nil, 0, err
	}
	rows := arr(obj(root["data"])["list"])
	if rows == nil || len(rows) > MaxSegmentItems {
		return nil, 0, schemaError(fmt.Errorf("invalid LeTV barrage items"))
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		at, err := secondsMS(text(row["start"]))
		if err != nil || at < segment.StartMS || at >= segment.EndMS || text(row["type"]) != "txt" {
			continue
		}
		item := Item{ID: text(row["_id"]), TimeMS: at, Text: text(row["txt"]), Mode: 1,
			Color: rgbColor(text(row["color"]), 16), FontSize: 25}
		if len(item.ID) <= 128 && validItem(item) {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}
