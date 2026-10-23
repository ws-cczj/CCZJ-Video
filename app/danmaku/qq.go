package danmaku

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

type qqProvider struct{ client *Client }

func (p *qqProvider) Search(ctx context.Context, query string) ([]Candidate, error) {
	body := map[string]any{"version": "25071701", "clientType": 1, "filterValue": "", "uuid": "cczj-local", "query": query, "retry": 0, "pagenum": 0, "isPrefetch": true, "pagesize": 10, "queryFrom": 0, "searchDatakey": "", "transInfo": "", "isneedQc": true, "preQid": "", "adClientInfo": "", "extraInfo": map[string]any{"multi_terminal_pc": "1", "themeType": "1", "sugRelatedIds": "{}", "appVersion": ""}}
	root, err := p.client.object(ctx, "https://pbaccess.video.qq.com/trpc.videosearch.mobile_search.MultiTerminalSearch/MbSearch?vplatform=2", body)
	if err != nil {
		return nil, err
	}
	if err = business(root["ret"], "0"); err != nil {
		return nil, err
	}
	data := obj(root["data"])
	if data == nil {
		return nil, schemaError(fmt.Errorf("missing QQ search data"))
	}
	rows := arr(obj(data["normalList"])["itemList"])
	for _, value := range arr(data["areaBoxList"]) {
		box := obj(value)
		if text(box["boxId"]) == "MainNeed" {
			rows = append(rows, arr(box["itemList"])...)
		}
	}
	result := []Candidate{}
	seen := map[string]bool{}
	for _, value := range rows {
		row := obj(value)
		info := obj(row["videoInfo"])
		if info == nil {
			continue
		}
		onsite := false
		for _, key := range []string{"playSites", "episodeSites"} {
			for _, site := range arr(info[key]) {
				if text(obj(site)["enName"]) == "qq" {
					onsite = true
				}
			}
		}
		id := text(obj(row["doc"])["id"])
		if !onsite || !qqID.MatchString(id) || seen[id] {
			continue
		}
		seen[id] = true
		result = append(result, Candidate{Provider: "qq", Ref: "cid:" + id, Title: plainTitle(text(info["title"])), Year: metadataYear(text(info["year"])), Kind: text(info["typeName"])})
	}
	return result, nil
}
func qqReference(ref string) (cid, vid string, err error) {
	ref = strings.TrimSpace(ref)
	if strings.HasPrefix(ref, "https://") {
		u, e := url.Parse(ref)
		if e != nil || u.Hostname() != "v.qq.com" {
			return "", "", fmt.Errorf("invalid QQ page")
		}
		parts := strings.Split(strings.TrimSuffix(strings.Trim(u.Path, "/"), ".html"), "/")
		if len(parts) >= 4 && parts[0] == "x" && parts[1] == "cover" {
			cid, vid = parts[2], parts[3]
		} else if len(parts) == 3 && parts[0] == "x" {
			if parts[1] == "cover" {
				cid = parts[2]
			} else if parts[1] == "page" {
				vid = parts[2]
			}
		}
	} else if strings.HasPrefix(ref, "cid:") {
		cid = strings.TrimPrefix(ref, "cid:")
	} else {
		vid = strings.TrimPrefix(ref, "vid:")
	}
	if cid != "" && !qqID.MatchString(cid) || vid != "" && !qqID.MatchString(vid) || cid == "" && vid == "" {
		return "", "", fmt.Errorf("invalid QQ identity")
	}
	return cid, vid, nil
}
func (p *qqProvider) Episodes(ctx context.Context, ref string) ([]Episode, error) {
	cid, vid, err := qqReference(ref)
	if err != nil {
		return nil, schemaError(err)
	}
	if vid != "" {
		e := Episode{Provider: "qq", SeriesID: cid, EpisodeID: vid, Title: vid, PageURL: "https://v.qq.com/x/page/" + vid + ".html"}
		index, err := p.Index(ctx, e)
		if err != nil {
			return nil, err
		}
		if len(index.Segments) > 0 {
			e.DurationMS = index.Segments[len(index.Segments)-1].EndMS
		}
		return []Episode{e}, nil
	}
	queue := []string{"cid=" + cid + "&detail_page_type=1&req_from=web_vsite&req_from_second_type=&req_type=0"}
	visited := map[string]bool{}
	seen := map[string]bool{}
	result := []Episode{}
	for len(queue) > 0 {
		page := queue[0]
		queue = queue[1:]
		if visited[page] {
			continue
		}
		if len(visited) >= 80 {
			return nil, schemaError(fmt.Errorf("QQ episode pagination exceeds limit"))
		}
		visited[page] = true
		params := map[string]string{"req_from": "web_vsite", "page_id": "vsite_episode_list", "page_type": "detail_operation", "id_type": "1", "page_size": "", "cid": cid, "vid": "", "lid": "", "page_num": "", "page_context": page, "detail_page_type": "1"}
		root, err := p.client.object(ctx, "https://pbaccess.video.qq.com/trpc.universal_backend_service.page_server_rpc.PageServer/GetPageData?video_appid=3000010&vversion_name=8.2.96&vversion_platform=2", map[string]any{"has_cache": 1, "page_params": params})
		if err != nil {
			return nil, err
		}
		if err = business(root["ret"], "0"); err != nil {
			return nil, err
		}
		data := obj(root["data"])
		modules, ok := data["module_list_datas"].([]any)
		if !ok {
			return nil, schemaError(fmt.Errorf("missing QQ episode modules"))
		}
		for _, value := range modules {
			for _, value := range arr(obj(value)["module_datas"]) {
				module := obj(value)
				tabs := text(obj(module["module_params"])["tabs"])
				if tabs != "" {
					var decoded any
					if err = json.Unmarshal([]byte(tabs), &decoded); err != nil {
						return nil, schemaError(err)
					}
					walkMaps(decoded, func(row map[string]any) {
						context := text(row["page_context"])
						if context != "" && !visited[context] {
							queue = append(queue, context)
						}
					})
				}
				for _, value := range arr(obj(module["item_data_lists"])["item_datas"]) {
					row := obj(obj(value)["item_params"])
					id := text(row["vid"])
					if !qqID.MatchString(id) || seen[id] || text(row["is_trailer"]) == "1" {
						continue
					}
					if parent := text(row["cid"]); parent != "" && parent != cid {
						continue
					}
					seen[id] = true
					title := text(row["play_title"])
					if title == "" {
						title = text(row["union_title"])
					}
					ordinal := text(row["c_title_output"])
					if ordinal == "" {
						ordinal = text(row["title"])
					}
					result = append(result, Episode{Provider: "qq", SeriesID: cid, EpisodeID: id, Title: plainTitle(title), Number: ordinal, DurationMS: number(row["duration"]) * 1000, PageURL: "https://v.qq.com/x/cover/" + cid + "/" + id + ".html"})
				}
			}
		}
		if len(result) > 5000 || len(queue) > 5000 {
			return nil, schemaError(fmt.Errorf("too many QQ episodes"))
		}
	}
	return result, nil
}
func walkMaps(value any, visit func(map[string]any)) {
	var walk func(any, int)
	walk = func(value any, depth int) {
		if depth > 24 {
			return
		}
		switch v := value.(type) {
		case map[string]any:
			visit(v)
			for _, child := range v {
				walk(child, depth+1)
			}
		case []any:
			for _, child := range v {
				walk(child, depth+1)
			}
		}
	}
	walk(value, 0)
}

var segmentPath = regexp.MustCompile(`^[A-Za-z0-9_/-]{1,200}$`)

func (p *qqProvider) Index(ctx context.Context, e Episode) (Index, error) {
	root, err := p.client.object(ctx, "https://dm.video.qq.com/barrage/base/"+e.EpisodeID, nil)
	if err != nil {
		return Index{}, err
	}
	rows, ok := root["segment_index"].(map[string]any)
	if !ok {
		return Index{}, schemaError(fmt.Errorf("missing QQ segment_index"))
	}
	if len(rows) > 10000 {
		return Index{}, schemaError(fmt.Errorf("too many QQ segments"))
	}
	span := number(root["segment_span"])
	if span <= 0 || span > 3600000 {
		return Index{}, schemaError(fmt.Errorf("invalid QQ segment span"))
	}
	result := Index{Segments: make([]Segment, 0, len(rows))}
	for _, value := range rows {
		row := obj(value)
		name := text(row["segment_name"])
		if !segmentPath.MatchString(name) || text(row["segment_start"]) == "" {
			return Index{}, schemaError(fmt.Errorf("invalid QQ segment"))
		}
		at := number(row["segment_start"])
		result.Segments = append(result.Segments, Segment{Key: name, StartMS: at, EndMS: at + span})
	}
	sort.Slice(result.Segments, func(i, j int) bool { return result.Segments[i].StartMS < result.Segments[j].StartMS })
	for i := 0; i+1 < len(result.Segments); i++ {
		result.Segments[i].EndMS = result.Segments[i+1].StartMS
	}
	return result, nil
}
func (p *qqProvider) Segment(ctx context.Context, e Episode, s Segment) ([]Item, int, error) {
	raw, err := p.client.read(ctx, "https://dm.video.qq.com/barrage/segment/"+e.EpisodeID+"/"+strings.TrimPrefix(s.Key, "/"), nil)
	if err != nil {
		return nil, 0, err
	}
	return DecodeQQ(raw)
}
