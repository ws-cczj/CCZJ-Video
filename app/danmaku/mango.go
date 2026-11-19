package danmaku

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
)

const (
	mangoProviderID    = "mgtv"
	mangoSearchHost    = "mobileso.bz.mgtv.com"
	mangoCatalogHost   = "pcweb.api.mgtv.com"
	mangoBarrageHost   = "galaxy.bz.mgtv.com"
	mangoCDNAliHost    = "bullet-ali.hitv.com"
	mangoCDNWSHost     = "bullet-ws.hitv.com"
	mangoWeb           = "https://www.mgtv.com/"
	mangoSearchAgent   = "Dalvik/2.1.0 (Linux; U; Android 16; 23127PN0CC Build/AP1A.240505.003)"
	mangoSegmentMS     = 60_000
	mangoCatalogBudget = 12
)

var mangoMonth = regexp.MustCompile(`^[0-9]{4}(0[1-9]|1[0-2])$`)
var mangoEpisodeLabel = regexp.MustCompile(`^(第[零〇一二两三四五六七八九十百0-9]+[集话話]|[0-9]{1,4})$`)
var mangoCDNVersion = regexp.MustCompile(`^bullet/(?:[A-Za-z0-9_-]+/)+[0-9]+$`)

type mangoProvider struct {
	client   *Client
	deviceID string
}

func newMangoProvider(client *Client) *mangoProvider {
	// Anonymous request metadata lives only for the application session.
	return &mangoProvider{client: client, deviceID: fmt.Sprintf("%x", md5.Sum([]byte(rand.Text())))}
}

func (p *mangoProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	sequence := fmt.Sprintf("%x", md5.Sum([]byte(fmt.Sprintf("%s.%d", p.deviceID, time.Now().UnixMilli()))))
	query := url.Values{
		"q": {title}, "_support": {"10100001"}, "device": {"23127PN0CC"}, "osVersion": {"16"},
		"appVersion": {"9.3.3"}, "did": {p.deviceID}, "mac": {p.deviceID}, "seqId": {sequence},
		"ticket": {""}, "userId": {"0"}, "osType": {"android"}, "type": {"10"}, "abroad": {"0"},
	}
	data, err := p.data(ctx, mangoSearchHost, "/aphone/search/rebirth/v2", query, "code", "200")
	if err != nil {
		return nil, err
	}
	if data["forbidden"] == true {
		return nil, apperror.New(apperror.PermissionDenied, "芒果 TV 拒绝了弹幕作品搜索")
	}
	sections := arr(data["contents"])
	if sections == nil {
		return nil, schemaError(fmt.Errorf("missing Mango search contents"))
	}
	results := []Candidate{}
	seen := map[string]bool{}
	for _, section := range sections {
		block := obj(section)
		if text(block["type"]) != "mediaRebirthV2" {
			continue
		}
		rows := arr(block["data"])
		if rows == nil {
			return nil, schemaError(fmt.Errorf("missing Mango media search rows"))
		}
		for _, value := range rows {
			row := obj(value)
			id, source := text(row["clipId"]), text(row["source"])
			name := plainTitle(text(row["title"]))
			if (source != "" && source != "imgo") || !decimalID.MatchString(id) || name == "" || seen[id] {
				continue
			}
			description := ""
			if details := arr(row["desc"]); len(details) > 0 {
				description = text(details[0])
			}
			kind, _, _ := strings.Cut(description, "/")
			kind = strings.TrimSpace(strings.TrimPrefix(kind, "类型:"))
			switch mediaKind(kind) {
			case "tv", "animation", "movie":
				results = append(results, Candidate{Provider: mangoProviderID, Ref: id, Title: name, Year: metadataYear(description), Kind: kind})
				seen[id] = true
			}
		}
	}
	return results, nil
}

func (p *mangoProvider) Episodes(ctx context.Context, ref string) ([]Episode, error) {
	if !decimalID.MatchString(ref) {
		return nil, apperror.New(apperror.Validation, "芒果 TV 作品标识无效")
	}
	results := []Episode{}
	seenEpisodes := map[string]bool{}
	months := []string{""}
	requests := 0
	for monthIndex := 0; monthIndex < len(months); monthIndex++ {
		month := months[monthIndex]
		lastPage := 1
		for page := 1; page <= lastPage; page++ {
			requests++
			if requests > mangoCatalogBudget {
				return nil, apperror.New(apperror.Unsupported, "芒果 TV 目录超出单次匹配范围")
			}
			data, err := p.data(ctx, mangoCatalogHost, "/variety/showlist", url.Values{
				"allowedRC": {"1"}, "collection_id": {ref}, "month": {month},
				"page": {strconv.Itoa(page)}, "_support": {"10000000"},
			}, "code", "200")
			if err != nil {
				return nil, err
			}
			rows := arr(data["list"])
			total, totalErr := strconv.Atoi(text(data["total"]))
			size, sizeErr := strconv.Atoi(text(data["pageSize"]))
			if rows == nil || len(rows) > 5000 || totalErr != nil || sizeErr != nil || total < 0 || total > 5000 || size < 1 || size > 5000 || text(data["pageNo"]) != strconv.Itoa(page) {
				return nil, schemaError(fmt.Errorf("invalid Mango catalog page"))
			}
			if page == 1 {
				lastPage = int((int64(total) + int64(size) - 1) / int64(size))
			}
			if requests == 1 {
				current := text(obj(data["cur"])["m"])
				seenMonths := map[string]bool{current: true}
				for i, value := range arr(data["tab_m"]) {
					key := text(obj(value)["m"])
					if !mangoMonth.MatchString(key) {
						return nil, schemaError(fmt.Errorf("invalid Mango catalog month"))
					}
					// Month zero is the initial page for the first listed month.
					if !seenMonths[key] && !(current == "0" && i == 0) {
						seenMonths[key] = true
						months = append(months, key)
					}
				}
			}
			for _, value := range rows {
				episode, ok := mangoEpisode(obj(value), ref)
				if ok && !seenEpisodes[episode.EpisodeID] {
					results = append(results, episode)
					seenEpisodes[episode.EpisodeID] = true
					if len(results) > 5000 {
						return nil, schemaError(fmt.Errorf("too many Mango episodes"))
					}
				}
			}
		}
	}
	return results, nil
}

func mangoEpisode(row map[string]any, series string) (Episode, bool) {
	vid, label := text(row["video_id"]), strings.TrimSpace(text(row["t2"]))
	if !decimalID.MatchString(vid) || text(row["src_clip_id"]) != series || text(row["isIntact"]) != "1" || text(row["isnew"]) == "2" {
		return Episode{}, false
	}
	title := plainTitle(text(row["t3"]))
	if title == "" {
		title = plainTitle(text(row["t1"]))
	}
	if title == "" || matchExtra.MatchString(title) {
		return Episode{}, false
	}
	if !mangoEpisodeLabel.MatchString(label) {
		label = ""
	}
	return Episode{
		Provider: mangoProviderID, SeriesID: series, EpisodeID: vid, Title: title, Number: label,
		DurationMS: clockDurationMS(text(row["time"])), PageURL: mangoWeb + "b/" + series + "/" + vid + ".html",
	}, true
}

func (p *mangoProvider) Index(ctx context.Context, episode Episode) (Index, error) {
	if !decimalID.MatchString(episode.SeriesID) || !decimalID.MatchString(episode.EpisodeID) {
		return Index{}, apperror.New(apperror.Validation, "芒果 TV 单集标识无效")
	}
	query := url.Values{"cid": {episode.SeriesID}, "vid": {episode.EpisodeID}}
	detail, err := p.data(ctx, mangoCatalogHost, "/video/info", query, "code", "200")
	if err != nil {
		return Index{}, err
	}
	info := obj(detail["info"])
	duration := clockDurationMS(text(info["time"]))
	if text(info["clipId"]) != episode.SeriesID || duration <= 0 {
		return Index{}, schemaError(fmt.Errorf("invalid Mango episode duration or collection"))
	}
	query.Set("version", "8.1.39")
	query.Set("os", "10.15.7")
	query.Set("abroad", "0")
	query.Set("platform", "0")
	for _, key := range []string{"uuid", "mac", "pid", "ticket"} {
		query.Set(key, "")
	}
	config, err := p.data(ctx, mangoBarrageHost, "/getctlbarrage", query, "status", "0")
	if err != nil {
		return Index{}, err
	}
	enabled, ok := config["switcher"].(bool)
	if ok && !enabled {
		return Index{}, apperror.New(apperror.PermissionDenied, "芒果 TV 已关闭该单集弹幕")
	}
	if !ok || text(config["interval"]) != "60" {
		return Index{}, schemaError(fmt.Errorf("invalid Mango barrage configuration"))
	}
	base, err := mangoCDNBase(text(config["cdn_list"]), text(config["cdn_version"]), episode.EpisodeID)
	if err != nil {
		return Index{}, err
	}
	index := Index{Segments: make([]Segment, 0, (duration+mangoSegmentMS-1)/mangoSegmentMS)}
	for start := int64(0); start < duration; start += mangoSegmentMS {
		key := mangoSegmentName(start)
		if base != "" {
			key = base + "/" + key
		}
		index.Segments = append(index.Segments, Segment{Key: key, StartMS: start, EndMS: min(start+mangoSegmentMS, duration)})
	}
	return index, nil
}

func mangoCDNBase(hostList, version, vid string) (string, error) {
	if hostList == "" && version == "" {
		return "", nil
	}
	if !validMangoCDNVersion(version, vid) {
		return "", schemaError(fmt.Errorf("invalid Mango CDN version"))
	}
	for _, value := range strings.Split(hostList, ",") {
		host := strings.TrimSpace(value)
		if isMangoCDN(host) {
			return "https://" + host + "/" + version, nil
		}
	}
	return "", apperror.New(apperror.Unsupported, "芒果 TV 返回了未适配的弹幕 CDN")
}

func isMangoCDN(host string) bool { return host == mangoCDNAliHost || host == mangoCDNWSHost }

func validMangoCDNVersion(version, vid string) bool {
	return len(version) <= 128 && strings.HasSuffix(version, "/"+vid) && mangoCDNVersion.MatchString(version)
}

func mangoSegmentName(startMS int64) string {
	return strconv.FormatInt(startMS/mangoSegmentMS, 10) + ".json"
}

func (p *mangoProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !decimalID.MatchString(episode.SeriesID) || !decimalID.MatchString(episode.EpisodeID) || segment.StartMS < 0 || segment.StartMS%mangoSegmentMS != 0 {
		return nil, 0, apperror.New(apperror.Validation, "芒果 TV 弹幕分片标识无效")
	}
	name := mangoSegmentName(segment.StartMS)
	if segment.Key != name {
		target, err := url.Parse(segment.Key)
		if err != nil || !AllowedURL(target) || !isMangoCDN(target.Host) || target.RawQuery != "" || target.Fragment != "" {
			return nil, 0, schemaError(fmt.Errorf("invalid Mango CDN segment URL"))
		}
		base, found := strings.CutSuffix(strings.TrimPrefix(target.Path, "/"), "/"+name)
		if !found || !validMangoCDNVersion(base, episode.EpisodeID) {
			return nil, 0, schemaError(fmt.Errorf("Mango CDN segment does not belong to the episode"))
		}
		data, err := p.data(ctx, target.Host, target.Path, nil, "status", "0")
		if err == nil {
			return decodeMangoSegment(data, segment)
		}
		// A sparse or retired CDN version can lack the file. The first-party
		// barrage endpoint still resolves that exact minute, including empty lists.
		if !apperror.Is(err, apperror.NotFound) {
			return nil, 0, err
		}
	}
	data, err := p.data(ctx, mangoBarrageHost, "/rdbarrage", url.Values{
		"cid": {episode.SeriesID}, "vid": {episode.EpisodeID}, "time": {strconv.FormatInt(segment.StartMS, 10)},
	}, "status", "0")
	if err != nil {
		return nil, 0, err
	}
	return decodeMangoSegment(data, segment)
}

func decodeMangoSegment(data map[string]any, segment Segment) ([]Item, int, error) {
	rows := arr(data["items"])
	if rows == nil || len(rows) > MaxSegmentItems {
		return nil, 0, schemaError(fmt.Errorf("invalid Mango barrage items"))
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		if kind := text(row["type"]); kind != "" && kind != "0" {
			continue
		}
		at, err := strconv.ParseInt(text(row["time"]), 10, 64)
		if err != nil || at < segment.StartMS || at >= segment.EndMS {
			continue
		}
		mode := 1
		switch text(row["v2_position"]) {
		case "1":
			mode = 5
		case "2":
			mode = 4
		case "", "0":
		default:
			continue
		}
		id := text(row["id"])
		if id == "" {
			id = text(row["ids"])
		}
		item := Item{ID: id, TimeMS: at, Text: text(row["content"]), Mode: mode, Color: mangoColor(obj(row["v2_color"])), FontSize: 25}
		if validItem(item) && len(id) <= 128 {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}

func mangoColor(value map[string]any) uint32 {
	for _, name := range []string{"color_left", "color_right"} {
		color := obj(value[name])
		r, er := strconv.ParseUint(text(color["r"]), 10, 8)
		g, eg := strconv.ParseUint(text(color["g"]), 10, 8)
		b, eb := strconv.ParseUint(text(color["b"]), 10, 8)
		if er == nil && eg == nil && eb == nil {
			return uint32(r<<16 | g<<8 | b)
		}
	}
	return 0xffffff
}

func (p *mangoProvider) data(ctx context.Context, host, path string, query url.Values, code, ok string) (map[string]any, error) {
	endpoint := url.URL{Scheme: "https", Host: host, Path: path, RawQuery: query.Encode()}
	root, err := p.client.object(ctx, endpoint.String(), nil)
	if err != nil {
		return nil, err
	}
	if err := business(root[code], ok); err != nil {
		return nil, err
	}
	data := obj(root["data"])
	if data == nil {
		return nil, schemaError(fmt.Errorf("missing Mango response data"))
	}
	return data, nil
}
