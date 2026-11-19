package danmaku

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cczjVideo/app/apperror"
	"golang.org/x/text/encoding/simplifiedchinese"
)

const (
	sohuProviderID   = "sohu"
	sohuSearchHost   = "m.so.tv.sohu.com"
	sohuCatalogHost  = "pl.hd.sohu.com"
	sohuBarrageHost  = "api.danmu.tv.sohu.com"
	sohuWeb          = "https://tv.sohu.com"
	sohuSegmentMS    = 300_000
	sohuSearchSuffix = "vxWaXm3C5SA9&fpc"
	sohuCatalogKey   = "f351515304020cad28c92f70f002261c"
)

type sohuProvider struct{ client *Client }

func (p *sohuProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	visitor := fmt.Sprintf("%x", md5.Sum([]byte(rand.Text())))
	code := fmt.Sprintf("%x", md5.Sum([]byte(timestamp+visitor+sohuSearchSuffix)))
	query := url.Values{
		"key": {title}, "type": {"1"}, "page": {"1"}, "page_size": {"20"}, "user_id": {""},
		"tabsChosen": {"0"}, "poster": {"4"}, "tuple": {"6"}, "extSource": {"1"},
		"show_star_detail": {"3"}, "pay": {"1"}, "hl": {"3"}, "uid": {timestamp + "000"},
		"passport": {""}, "plat": {"-1"}, "ssl": {"0"}, "fpc": {visitor}, "timeStamp": {timestamp}, "code": {code},
	}
	root, err := p.client.object(ctx, "https://"+sohuSearchHost+"/search/pc/keyword?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if err := business(root["status"], "200"); err != nil {
		return nil, err
	}
	rows := arr(obj(root["data"])["items"])
	if rows == nil || len(rows) > 100 {
		return nil, schemaError(fmt.Errorf("invalid Sohu search results"))
	}
	results := []Candidate{}
	seen := map[string]bool{}
	for _, value := range rows {
		row := obj(value)
		id := text(row["aid"])
		name := plainTitle(strings.NewReplacer("<<<", "", ">>>", "").Replace(text(row["album_name"])))
		if text(row["whole_source"]) != "sohu" || text(row["site"]) != "1" || text(row["is_album"]) != "1" ||
			text(row["is_trailer"]) != "0" || !decimalID.MatchString(id) || name == "" || seen[id] {
			continue
		}
		results = append(results, Candidate{Provider: sohuProviderID, Ref: id, Title: name, Year: metadataYear(text(row["year"]))})
		seen[id] = true
	}
	return results, nil
}

func (p *sohuProvider) Episodes(ctx context.Context, series string) ([]Episode, error) {
	if !decimalID.MatchString(series) {
		return nil, apperror.New(apperror.Validation, "搜狐视频作品标识无效")
	}
	query := url.Values{"playlistid": {series}, "api_key": {sohuCatalogKey}}
	raw, err := p.client.read(ctx, "https://"+sohuCatalogHost+"/videolist?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(raw) {
		raw, err = simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		if err != nil {
			return nil, schemaError(err)
		}
	}
	raw = bytes.TrimSpace(raw)
	if data, ok := bytes.CutPrefix(raw, []byte("jsonp(")); ok {
		raw = bytes.TrimSuffix(bytes.TrimSuffix(data, []byte(";")), []byte(")"))
	}
	root, err := object(raw)
	if err != nil {
		return nil, err
	}
	rows := arr(root["videos"])
	if text(root["playlistid"]) != series || rows == nil || len(rows) > 500 {
		return nil, schemaError(fmt.Errorf("invalid Sohu catalog"))
	}
	if text(root["currentPage"]) != "1" || number(root["totalSet"]) > int64(len(rows)) {
		return nil, apperror.New(apperror.Unsupported, "搜狐视频目录超出单次匹配范围")
	}
	results := make([]Episode, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		id, title := text(row["vid"]), plainTitle(text(row["name"]))
		label := text(row["showName"])
		_, numbered := episodeOrdinal(label)
		duration, err := secondsMS(text(row["playLength"]))
		if text(row["site"]) != "1" || !decimalID.MatchString(id) || title == "" || matchExtra.MatchString(title) ||
			!numbered || err != nil || duration <= 0 {
			continue
		}
		results = append(results, Episode{Provider: sohuProviderID, SeriesID: series, EpisodeID: id,
			Title: title, Number: label, DurationMS: duration})
	}
	return results, nil
}

func (p *sohuProvider) Index(ctx context.Context, episode Episode) (Index, error) {
	if episode.DurationMS <= 0 || episode.DurationMS > 86_400_000 {
		return Index{}, schemaError(fmt.Errorf("invalid Sohu episode duration"))
	}
	return regularIndex(episode.DurationMS, sohuSegmentMS), nil
}

func (p *sohuProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !decimalID.MatchString(episode.SeriesID) || !decimalID.MatchString(episode.EpisodeID) || !regularSegment(segment, sohuSegmentMS) {
		return nil, 0, apperror.New(apperror.Validation, "搜狐视频弹幕分片标识无效")
	}
	query := url.Values{"act": {"dmlist_v2"}, "vid": {episode.EpisodeID}, "aid": {episode.SeriesID}, "pct": {"2"},
		"time_begin": {strconv.FormatInt(segment.StartMS/1000, 10)}, "time_end": {strconv.FormatInt((segment.EndMS+999)/1000, 10)},
		"dct": {"1"}, "request_from": {"h5_js"}}
	root, err := p.client.object(ctx, "https://"+sohuBarrageHost+"/dmh5/dmListAll?"+query.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	if err := business(root["status"], "1"); err != nil {
		return nil, 0, err
	}
	rows := arr(obj(root["info"])["comments"])
	if rows == nil || len(rows) > MaxSegmentItems {
		return nil, 0, schemaError(fmt.Errorf("invalid Sohu barrage results"))
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		at, err := secondsMS(text(row["v"]))
		if err != nil || at < segment.StartMS || at >= segment.EndMS {
			continue
		}
		style := obj(row["t"])
		color := rgbColor(text(style["c"]), 10)
		mode := 1
		switch text(style["p"]) {
		case "4", "t":
			mode = 5
		case "5", "b":
			mode = 4
		}
		item := Item{ID: text(row["i"]), TimeMS: at, Text: text(row["c"]), Mode: mode, Color: color, FontSize: 25}
		if len(item.ID) <= 128 && validItem(item) {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}
