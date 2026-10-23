package danmaku

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
)

type iqiyiProvider struct{ client *Client }

func (p *iqiyiProvider) Search(ctx context.Context, query string) ([]Candidate, error) {
	q := url.Values{"key": {query}, "current_page": {"1"}, "mode": {"1"}, "source": {"input"}, "suggest": {""}, "pcv": {"13.074.22699"}, "version": {"13.074.22699"}, "pageNum": {"1"}, "pageSize": {"10"}, "pu": {""}, "u": {"cczj-local"}, "scale": {"200"}, "token": {""}, "userVip": {"0"}, "conduit": {""}, "vipType": {"-1"}, "os": {""}, "osShortName": {"win10"}, "dataType": {""}, "appMode": {""}, "ad": {`{"lm":3,"azd":1000000000951,"azt":733,"position":"feed"}`}, "adExt": {`{"r":"2.1.5-ares6-pure"}`}}
	root, err := p.client.object(ctx, "https://mesh.if.iqiyi.com/portal/lw/search/homePageV3?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err = business(root["code"], "0"); err != nil {
		return nil, err
	}
	data := obj(root["data"])
	templates, ok := data["templates"].([]any)
	if !ok {
		return nil, schemaError(fmt.Errorf("missing IQ search templates"))
	}
	result := []Candidate{}
	seen := map[string]bool{}
	add := func(row map[string]any) {
		if text(row["siteId"]) != "iqiyi" {
			return
		}
		id := text(row["playQipuId"])
		if id == "" {
			for _, v := range arr(row["videos"]) {
				id = text(obj(v)["qipuId"])
				if decimalID.MatchString(id) {
					break
				}
			}
		}
		if !decimalID.MatchString(id) || seen[id] {
			return
		}
		seen[id] = true
		year := text(row["year"])
		if value := obj(row["year"]); value != nil {
			year = text(value["value"])
		}
		kind := text(row["channelName"])
		if kind == "" {
			kind = strings.SplitN(text(row["channel"]), ",", 2)[0]
		}
		result = append(result, Candidate{Provider: "iqiyi", Ref: "entity:" + id, Title: plainTitle(text(row["title"])), Year: metadataYear(year), Kind: kind})
	}
	for _, value := range templates {
		row := obj(value)
		switch number(row["template"]) {
		case 101, 102, 103:
			add(obj(row["albumInfo"]))
		case 112:
			for _, v := range arr(row["intentAlbumInfos"]) {
				add(obj(v))
			}
		}
	}
	return result, nil
}

func iqDetailURL(id string, now time.Time) string {
	q := url.Values{"entity_id": {id}, "device_id": {"cczj-local"}, "auth_cookie": {""}, "user_id": {"0"}, "vip_type": {"-1"}, "vip_status": {"0"}, "conduit_id": {""}, "pcv": {"13.082.22866"}, "app_version": {"13.082.22866"}, "ext": {""}, "app_mode": {"standard"}, "scale": {"100"}, "timestamp": {strconv.FormatInt(now.UnixMilli(), 10)}, "src": {"pca_tvg"}, "os": {""}, "ad_ext": {`{"r":"2.2.0-ares6-pure"}`}}
	keys := make([]string, 0, len(q))
	for key := range q {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	values := make([]string, 0, len(keys))
	for _, key := range keys {
		values = append(values, key+"="+q.Get(key))
	}
	sum := md5.Sum([]byte(strings.Join(values, "&") + "&secret_key=howcuteitis"))
	q.Set("sign", strings.ToUpper(hex.EncodeToString(sum[:])))
	return "https://www.iqiyi.com/prelw/tvg/v2/lw/base_info?" + q.Encode()
}

var tvidInURL = regexp.MustCompile(`(?:^|qips://|[;?&])tvid=([0-9]+)`)
var iqPage = regexp.MustCompile(`^v_([a-z0-9]+)\.html$`)

func (p *iqiyiProvider) resolveID(ctx context.Context, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if decimalID.MatchString(ref) {
		return ref, nil
	}
	u, err := url.Parse(ref)
	if err != nil || u.Scheme != "https" || u.Hostname() != "www.iqiyi.com" {
		return "", apperror.New(apperror.Validation, "请粘贴爱奇艺单集页面或数字 TVid")
	}
	match := iqPage.FindStringSubmatch(path.Base(u.Path))
	if match == nil {
		return "", apperror.New(apperror.Validation, "请使用具体单集页面，而非专辑页面")
	}
	root, err := p.client.object(ctx, "https://pcw-api.iq.com/api/decode/"+match[1]+"?platformId=3&modeCode=intl&langCode=sg", nil)
	if err != nil {
		return "", err
	}
	if err = business(root["code"], "0"); err != nil {
		return "", err
	}
	id := text(root["data"])
	if !decimalID.MatchString(id) {
		return "", schemaError(fmt.Errorf("missing IQ TVid"))
	}
	return id, nil
}
func (p *iqiyiProvider) base(ctx context.Context, id string) (Episode, error) {
	root, err := p.client.object(ctx, "https://pcw-api.iqiyi.com/video/video/baseinfo/"+id, nil)
	if err != nil {
		return Episode{}, err
	}
	if err = business(root["code"], "A00000"); err != nil {
		return Episode{}, err
	}
	data := obj(root["data"])
	if text(data["tvId"]) != id || number(data["durationSec"]) <= 0 {
		return Episode{}, schemaError(fmt.Errorf("invalid IQ episode metadata"))
	}
	if visible, ok := data["displayBarrage"].(bool); ok && !visible {
		return Episode{}, apperror.New(apperror.PermissionDenied, "爱奇艺已关闭该单集弹幕")
	}
	return Episode{Provider: "iqiyi", EpisodeID: id, SeriesID: text(data["albumId"]), Title: plainTitle(text(data["name"])), Number: text(data["order"]), DurationMS: number(data["durationSec"]) * 1000, PageURL: text(data["playUrl"])}, nil
}
func (p *iqiyiProvider) Episodes(ctx context.Context, ref string) ([]Episode, error) {
	if !strings.HasPrefix(ref, "entity:") {
		id, err := p.resolveID(ctx, ref)
		if err != nil {
			return nil, err
		}
		e, err := p.base(ctx, id)
		if err != nil {
			return nil, err
		}
		return []Episode{e}, nil
	}
	id := strings.TrimPrefix(ref, "entity:")
	if !decimalID.MatchString(id) {
		return nil, apperror.New(apperror.Validation, "爱奇艺作品标识无效")
	}
	root, err := p.client.object(ctx, iqDetailURL(id, time.Now()), nil)
	if err != nil {
		return nil, err
	}
	if err = business(root["status_code"], "0"); err != nil {
		return nil, err
	}
	data := obj(root["data"])
	template := obj(data["template"])
	tabs, ok := template["tabs"].([]any)
	if !ok {
		return nil, schemaError(fmt.Errorf("missing IQ episode tabs"))
	}
	result := []Episode{}
	seen := map[string]bool{}
	for _, value := range tabs {
		for _, value := range arr(obj(value)["blocks"]) {
			block := obj(value)
			kind := text(block["bk_type"])
			if kind != "album_episodes" && kind != "video_list" {
				continue
			}
			for _, group := range arr(obj(block["data"])["data"]) {
				walkMaps(group, func(row map[string]any) {
					if number(row["content_type"]) != 1 {
						return
					}
					id := text(row["qipu_id"])
					if match := tvidInURL.FindStringSubmatch(text(row["play_url"])); match != nil {
						id = match[1]
					}
					if !decimalID.MatchString(id) || seen[id] {
						return
					}
					seen[id] = true
					result = append(result, Episode{Provider: "iqiyi", EpisodeID: id, Title: plainTitle(text(row["short_display_name"]) + " " + text(row["subtitle"])), Number: text(row["album_order"]), PageURL: text(row["page_url"])})
				})
			}
		}
	}
	if len(result) > 5000 {
		return nil, schemaError(fmt.Errorf("too many IQ episodes"))
	}
	if len(result) == 0 {
		return nil, apperror.New(apperror.Unsupported, "该作品目录暂未适配")
	}
	sort.SliceStable(result, func(i, j int) bool { return number(result[i].Number) < number(result[j].Number) })
	return result, nil
}
func (p *iqiyiProvider) Index(ctx context.Context, e Episode) (Index, error) {
	verified, err := p.base(ctx, e.EpisodeID)
	if err != nil {
		return Index{}, err
	}
	total := (verified.DurationMS + 59999) / 60000
	if total > 1440 {
		return Index{}, schemaError(fmt.Errorf("IQ video too long"))
	}
	index := Index{Segments: make([]Segment, 0, total)}
	for i := int64(0); i < total; i++ {
		index.Segments = append(index.Segments, Segment{Key: strconv.FormatInt(i+1, 10), StartMS: i * 60000, EndMS: (i + 1) * 60000})
	}
	return index, nil
}
func iqSegmentURL(id, page string) string {
	padded := "0000" + id
	padded = padded[len(padded)-4:]
	sum := md5.Sum([]byte(id + "_60_" + page + "cbzuw1259a"))
	sign := hex.EncodeToString(sum[:])
	return "https://cmts.iqiyi.com/bullet/" + padded[:2] + "/" + padded[2:] + "/" + id + "_60_" + page + "_" + sign[len(sign)-8:] + ".br"
}
func (p *iqiyiProvider) Segment(ctx context.Context, e Episode, s Segment) ([]Item, int, error) {
	raw, err := p.client.read(ctx, iqSegmentURL(e.EpisodeID, s.Key), nil)
	if err != nil {
		return nil, 0, err
	}
	return DecodeIQiyi(raw)
}
