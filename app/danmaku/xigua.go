package danmaku

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cczjVideo/app/apperror"
	"golang.org/x/net/html"
)

const (
	xiguaProviderID  = "xigua"
	xiguaWebHost     = "m.ixigua.com"
	xiguaBarrageHost = "api3-normal-c-hl.ixigua.com"
	xiguaAgent       = "Mozilla/5.0 (iPhone; CPU iPhone OS 15_0 like Mac OS X) AppleWebKit/603.1.30 (KHTML, like Gecko) Version/17.5 Mobile/15A5370a Safari/602.1"
	xiguaSegmentMS   = 30_000
)

var xiguaVideoPath = regexp.MustCompile(`^/video/([0-9]{1,24})/?$`)

type xiguaProvider struct{ client *Client }

func (p *xiguaProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	raw, err := p.client.read(ctx, "https://"+xiguaWebHost+"/s/"+url.PathEscape(title), nil)
	if err != nil {
		return nil, err
	}
	page, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, schemaError(err)
	}
	results := []Candidate{}
	seen := map[string]bool{}
	seeds := xiguaSearchSeeds(xiguaPageState(page))
	for node := range page.Descendants() {
		if node.Type != html.ElementNode || !htmlHasClass(node, "s-long-video") {
			continue
		}
		var id, name, kind, year string
		for child := range node.Descendants() {
			if child.Type != html.ElementNode {
				continue
			}
			if child.Data == "a" && id == "" {
				if match := xiguaVideoPath.FindStringSubmatch(htmlAttr(child, "href")); match != nil {
					id = match[1]
				}
			}
			if htmlHasClass(child, "s-long-video-info-title") {
				name = plainTitle(htmlText(child))
				for link := range child.Descendants() {
					if title := htmlAttr(link, "title"); title != "" {
						name = plainTitle(title)
						break
					}
				}
			}
			if child.Data == "p" && kind == "" {
				metadata := strings.Split(htmlText(child), "/")
				if len(metadata) == 3 {
					kind, year = mediaKind(metadata[0]), metadataYear(metadata[2])
				}
			}
		}
		if id == "" || name == "" || year == "" || seen[id] || (kind != "tv" && kind != "animation") {
			continue
		}
		ref := id
		if seed := seeds[id]; seed != "" {
			ref = id + ":" + seed
		}
		results = append(results, Candidate{Provider: xiguaProviderID, Ref: ref, Title: name, Year: year, Kind: kind})
		seen[id] = true
		if len(results) >= 100 {
			break
		}
	}
	return results, nil
}

func (p *xiguaProvider) detail(ctx context.Context, id string) (map[string]any, error) {
	if !decimalID.MatchString(id) {
		return nil, apperror.New(apperror.Validation, "西瓜视频标识无效")
	}
	raw, err := p.client.read(ctx, "https://"+xiguaWebHost+"/video/"+id, nil)
	if err != nil {
		return nil, err
	}
	page, err := html.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, schemaError(err)
	}
	root := xiguaPageState(page)
	for _, key := range []string{"data", "storeState", "detail", "videoData", "result"} {
		root = obj(root[key])
	}
	if root == nil || (text(root["gid"]) != id && text(root["album_id"]) != id) {
		return nil, schemaError(fmt.Errorf("Xigua detail does not match the requested video"))
	}
	return root, nil
}

func xiguaPageState(page *html.Node) map[string]any {
	for node := range page.Descendants() {
		if node.Type != html.ElementNode || node.Data != "script" || node.FirstChild == nil {
			continue
		}
		state, ok := strings.CutPrefix(strings.TrimSpace(node.FirstChild.Data), "window._SSR_DATA = ")
		if !ok {
			continue
		}
		decoder := json.NewDecoder(strings.NewReader(state))
		decoder.UseNumber()
		var root map[string]any
		if err := decoder.Decode(&root); err == nil {
			return root
		}
	}
	return nil
}

func xiguaSearchSeeds(root map[string]any) map[string]string {
	seeds := map[string]string{}
	for _, loader := range obj(obj(root["data"])["loadersData"]) {
		for _, hit := range arr(obj(loader)["data"]) {
			for _, display := range arr(obj(hit)["display"]) {
				for _, value := range arr(obj(display)["album_content"]) {
					album := obj(value)
					id := text(album["album_id"])
					if text(album["mp_en"]) != "xigua" || !decimalID.MatchString(id) || seeds[id] == "conflict" {
						continue
					}
					for _, row := range arr(obj(album["episode_link"])["asc_link"]) {
						episode := obj(row)
						seed := text(episode["id"])
						if text(episode["album_id"]) != id || text(episode["seq"]) != "1" ||
							text(episode["episode_type"]) != "1" || !decimalID.MatchString(seed) {
							continue
						}
						if previous := seeds[id]; previous != "" && previous != seed {
							seeds[id] = "conflict"
							break
						}
						seeds[id] = seed
					}
				}
			}
		}
	}
	for id, seed := range seeds {
		if seed == "conflict" {
			delete(seeds, id)
		}
	}
	return seeds
}

func (p *xiguaProvider) Episodes(ctx context.Context, ref string) ([]Episode, error) {
	album, id, hasSeed := strings.Cut(ref, ":")
	if !hasSeed {
		id = album
	}
	detail, err := p.detail(ctx, id)
	if err != nil {
		return nil, err
	}
	series, current := text(detail["album_id"]), text(detail["gid"])
	rows := arr(detail["episodes_list"])
	if (hasSeed && series != album) || !decimalID.MatchString(series) || !decimalID.MatchString(current) || rows == nil || len(rows) > 500 {
		return nil, apperror.New(apperror.Unsupported, "西瓜视频未提供可匹配的正片目录")
	}
	results := make([]Episode, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		id, label := text(row["gid"]), text(row["seq_num"])
		_, numbered := episodeOrdinal(label)
		title := plainTitle(text(row["title"]))
		if !decimalID.MatchString(id) || !numbered || matchExtra.MatchString(title) {
			continue
		}
		if title == "" {
			title = "第" + label + "集"
		}
		duration := int64(0)
		if current == id {
			duration, _ = secondsMS(text(detail["duration"]))
		}
		results = append(results, Episode{Provider: xiguaProviderID, SeriesID: series, EpisodeID: id,
			Title: title, Number: label, DurationMS: duration, PageURL: "https://" + xiguaWebHost + "/video/" + id})
	}
	return results, nil
}

func (p *xiguaProvider) Index(ctx context.Context, episode Episode) (Index, error) {
	detail, err := p.detail(ctx, episode.EpisodeID)
	if err != nil {
		return Index{}, err
	}
	duration, err := secondsMS(text(detail["duration"]))
	if err != nil || duration <= 0 || text(detail["gid"]) != episode.EpisodeID || text(detail["album_id"]) != episode.SeriesID {
		return Index{}, schemaError(fmt.Errorf("Xigua episode duration or identity does not match"))
	}
	return regularIndex(duration, xiguaSegmentMS), nil
}

func (p *xiguaProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !decimalID.MatchString(episode.SeriesID) || !decimalID.MatchString(episode.EpisodeID) || !regularSegment(segment, xiguaSegmentMS) {
		return nil, 0, apperror.New(apperror.Validation, "西瓜视频弹幕分片标识无效")
	}
	query := url.Values{"item_id": {episode.EpisodeID}, "start_time": {strconv.FormatInt(segment.StartMS, 10)},
		"end_time": {strconv.FormatInt(segment.EndMS, 10)}, "format": {"json"}}
	root, err := p.client.object(ctx, "https://"+xiguaBarrageHost+"/vapp/danmaku/list/v1/?"+query.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	if err := business(obj(root["base_resp"])["status_code"], "0"); err != nil {
		return nil, 0, err
	}
	if text(root["start_time"]) != query.Get("start_time") || text(root["end_time"]) != query.Get("end_time") {
		return nil, 0, schemaError(fmt.Errorf("Xigua barrage window does not match the request"))
	}
	rows := arr(root["data"])
	if rows == nil && text(root["count"]) == "0" && root["data"] == nil {
		return []Item{}, 0, nil
	}
	if rows == nil || len(rows) > MaxSegmentItems {
		return nil, 0, schemaError(fmt.Errorf("invalid Xigua barrage items"))
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		at, err := strconv.ParseInt(text(row["offset_time"]), 10, 64)
		if err != nil || at < segment.StartMS || at >= segment.EndMS || text(row["item_id"]) != episode.EpisodeID {
			continue
		}
		item := Item{ID: text(row["danmaku_id"]), TimeMS: at, Text: text(row["text"]), Mode: 1,
			Color: rgbColor(strings.TrimPrefix(text(row["text_color"]), "0x"), 16), FontSize: 25}
		if len(item.ID) <= 128 && validItem(item) {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}
