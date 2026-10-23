package danmaku

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
)

var titleTags = regexp.MustCompile(`<[^>]*>`)

func plainTitle(value string) string {
	return strings.TrimSpace(html.UnescapeString(titleTags.ReplaceAllString(value, "")))
}

type bilibiliProvider struct {
	client  *Client
	mu      sync.Mutex
	mixin   string
	expires time.Time
}

var wbiOrder = []int{46, 47, 18, 2, 53, 8, 23, 32, 15, 50, 10, 31, 58, 3, 45, 35, 27, 43, 5, 49, 33, 9, 42, 19, 29, 28, 14, 39, 12, 38, 41, 13, 37, 48, 7, 16, 24, 55, 40, 61, 26, 17, 0, 1, 60, 51, 30, 4, 22, 25, 54, 21, 56, 59, 6, 63, 57, 62, 11, 36, 20, 34, 44, 52}

func (p *bilibiliProvider) sign(ctx context.Context, q url.Values) (url.Values, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.mixin == "" || time.Now().After(p.expires) {
		root, err := p.client.object(ctx, "https://api.bilibili.com/x/web-interface/nav", nil)
		if err != nil {
			return nil, err
		}
		images := obj(obj(root["data"])["wbi_img"])
		var key string
		for _, name := range []string{"img_url", "sub_url"} {
			u, err := url.Parse(text(images[name]))
			if err != nil {
				return nil, schemaError(err)
			}
			key += strings.TrimSuffix(path.Base(u.Path), path.Ext(u.Path))
		}
		if len(key) != 64 {
			return nil, apperror.New(apperror.AuthenticationRequired, "B站搜索校验信息不可用")
		}
		var mixed strings.Builder
		for _, i := range wbiOrder[:32] {
			mixed.WriteByte(key[i])
		}
		p.mixin = mixed.String()
		p.expires = time.Now().Add(time.Hour)
	}
	q.Set("wts", strconv.FormatInt(time.Now().Unix(), 10))
	for k, vs := range q {
		for i, v := range vs {
			vs[i] = strings.Map(func(r rune) rune {
				if strings.ContainsRune("!'()*", r) {
					return -1
				}
				return r
			}, v)
		}
		q[k] = vs
	}
	canonical := strings.ReplaceAll(q.Encode(), "+", "%20")
	sum := md5.Sum([]byte(canonical + p.mixin))
	q.Set("w_rid", hex.EncodeToString(sum[:]))
	return q, nil
}
func (p *bilibiliProvider) Search(ctx context.Context, query string) ([]Candidate, error) {
	result := []Candidate{}
	for _, kind := range []string{"media_bangumi", "media_ft", "video"} {
		q, err := p.sign(ctx, url.Values{"search_type": {kind}, "keyword": {query}, "page": {"1"}})
		if err != nil {
			return nil, err
		}
		root, err := p.client.object(ctx, "https://api.bilibili.com/x/web-interface/wbi/search/type?"+q.Encode(), nil)
		if err != nil {
			return nil, err
		}
		if err = business(root["code"], "0"); err != nil {
			return nil, err
		}
		data, ok := root["data"].(map[string]any)
		if !ok {
			return nil, schemaError(fmt.Errorf("missing search data"))
		}
		rows, ok := data["result"].([]any)
		if !ok {
			if data["result"] == nil && text(data["numResults"]) == "0" {
				continue
			}
			return nil, schemaError(fmt.Errorf("missing search result kind=%s type=%T total=%v page=%v", kind, data["result"], data["numResults"], data["page"]))
		}
		for _, value := range rows {
			row := obj(value)
			ref := text(row["bvid"])
			if kind != "video" {
				ref = "ss" + text(row["season_id"])
			}
			if ref == "" || ref == "ss" {
				continue
			}
			year := metadataYear(text(row["fix_pubtime_str"]))
			if year == "" {
				year = text(row["pubtime"])
			}
			category := kind
			if kind != "video" {
				if name := text(row["season_type_name"]); name != "" {
					category = name
				} else if name := map[int64]string{1: "番剧", 2: "电影", 3: "纪录片", 4: "国产动漫", 5: "电视剧", 7: "综艺"}[number(row["season_type"])]; name != "" {
					category = name
				}
			}
			result = append(result, Candidate{Provider: "bilibili", Ref: ref, Title: plainTitle(text(row["title"])), Year: metadataYear(year), Kind: category})
		}
	}
	return result, nil
}

var bvidPattern = regexp.MustCompile(`^BV[A-Za-z0-9]{10}$`)

func biliRef(input string) (string, string, error) {
	input = strings.TrimSpace(input)
	if strings.HasPrefix(input, "http") {
		u, err := url.Parse(input)
		if err != nil || u.Scheme != "https" || !(u.Hostname() == "www.bilibili.com" || u.Hostname() == "bilibili.com") {
			return "", "", apperror.New(apperror.Validation, "请使用B站视频或番剧单集地址")
		}
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		input = parts[len(parts)-1]
	}
	switch {
	case bvidPattern.MatchString(input):
		return "bvid", input, nil
	case strings.HasPrefix(input, "av") && decimalID.MatchString(input[2:]):
		return "aid", input[2:], nil
	case strings.HasPrefix(input, "ss") && decimalID.MatchString(input[2:]):
		return "season_id", input[2:], nil
	case strings.HasPrefix(input, "ep") && decimalID.MatchString(input[2:]):
		return "ep_id", input[2:], nil
	}
	return "", "", apperror.New(apperror.Validation, "请输入 BV号、av号、ss/ep号或完整视频地址")
}
func (p *bilibiliProvider) Episodes(ctx context.Context, ref string) ([]Episode, error) {
	kind, id, err := biliRef(ref)
	if err != nil {
		return nil, err
	}
	endpoint := "https://api.bilibili.com/x/web-interface/view?"
	pgc := kind == "season_id" || kind == "ep_id"
	if pgc {
		endpoint = "https://api.bilibili.com/pgc/view/web/season?"
	}
	root, err := p.client.object(ctx, endpoint+url.Values{kind: {id}}.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err = business(root["code"], "0"); err != nil {
		return nil, err
	}
	data := obj(root["data"])
	rowsKey := "pages"
	if pgc {
		data = obj(root["result"])
		rowsKey = "episodes"
	}
	rows, ok := data[rowsKey].([]any)
	if !ok {
		return nil, schemaError(fmt.Errorf("missing Bilibili episodes"))
	}
	result := []Episode{}
	for _, value := range rows {
		row := obj(value)
		e := Episode{Provider: "bilibili", SeriesID: text(data["season_id"]), EpisodeID: text(row["cid"]), AID: text(data["aid"]), Title: plainTitle(text(row["part"])), Number: text(row["page"]), DurationMS: number(row["duration"]) * 1000}
		if pgc {
			e.AID = text(row["aid"])
			e.Title = plainTitle(text(row["title"]) + " " + text(row["long_title"]))
			// Number alone cannot distinguish the preview and full episode. Keep
			// explicit content badges for matching; membership is not a content kind.
			if badge := plainTitle(text(row["badge"])); matchExtra.MatchString(badge) {
				e.Title += " " + badge
			}
			e.Number = text(row["title"])
			e.DurationMS = number(row["duration"])
			e.PageURL = "https://www.bilibili.com/bangumi/play/ep" + text(row["id"])
		} else {
			e.PageURL = "https://www.bilibili.com/video/" + text(data["bvid"]) + "?p=" + e.Number
		}
		if decimalID.MatchString(e.EpisodeID) {
			result = append(result, e)
		}
	}
	return result, nil
}
func (p *bilibiliProvider) Index(ctx context.Context, e Episode) (Index, error) {
	q := url.Values{"type": {"1"}, "oid": {e.EpisodeID}}
	if e.AID != "" {
		q.Set("pid", e.AID)
	}
	raw, err := p.client.read(ctx, "https://api.bilibili.com/x/v2/dm/web/view?"+q.Encode(), nil)
	if err != nil {
		return Index{}, err
	}
	fs, err := fields(raw)
	if err != nil {
		return Index{}, schemaError(err)
	}
	if pnum(fs, 1) != 0 {
		return Index{}, apperror.New(apperror.PermissionDenied, "B站已关闭或限制该弹幕池")
	}
	total := int64(0)
	span := int64(360000)
	found := false
	for _, f := range fs {
		if f.tag == 4 && f.wire == 2 {
			found = true
			segment, err := fields(f.data)
			if err != nil {
				return Index{}, schemaError(err)
			}
			total = int64(pnum(segment, 2))
			if value := int64(pnum(segment, 1)); value >= 1000 && value <= 3600000 {
				span = value
			}
		}
	}
	if !found {
		return Index{}, schemaError(fmt.Errorf("missing Bilibili segment metadata"))
	}
	if total == 0 && e.DurationMS > 0 {
		total = (e.DurationMS + span - 1) / span
	}
	if total < 0 || total > 1500 {
		return Index{}, schemaError(fmt.Errorf("invalid Bilibili segment count"))
	}
	index := Index{Segments: []Segment{}}
	for i := int64(0); i < total; i++ {
		index.Segments = append(index.Segments, Segment{Key: strconv.FormatInt(i+1, 10), StartMS: i * span, EndMS: (i + 1) * span})
	}
	return index, nil
}
func (p *bilibiliProvider) Segment(ctx context.Context, e Episode, s Segment) ([]Item, int, error) {
	q := url.Values{"type": {"1"}, "oid": {e.EpisodeID}, "segment_index": {s.Key}}
	if e.AID != "" {
		q.Set("pid", e.AID)
	}
	raw, err := p.client.read(ctx, "https://api.bilibili.com/x/v2/dm/web/seg.so?"+q.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	return DecodeBilibili(raw)
}
