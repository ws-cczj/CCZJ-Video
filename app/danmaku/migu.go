package danmaku

import (
	"context"
	"crypto/aes"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"cczjVideo/app/apperror"
)

const (
	miguProviderID  = "migu"
	miguSearchHost  = "jadeite.migu.cn"
	miguCatalogHost = "v3-sc.miguvideo.com"
	miguBarrageHost = "webapi.miguvideo.com"
	miguWeb         = "https://www.miguvideo.com"
	miguSegmentMS   = 30_000
	miguPacks       = "1002581,1002601,1003861,1003862,1003863,1003864,1003865,1003866,1004041,1004121,1004261,1004262,1004281,1004321,1004322,1004421,1004422,1002781,1004301,1004641,1005061,1005261,1005301,1005321,1005361,1005362,1005341,1005342,1005521,1005722,1005721,1015749,1015761,1015760,1015762,1015763,1015768,1015786,1015790,1015812,1015813,1015814,1015815,1015816,1015817,1015820,1015819,1015821"
)

type miguProvider struct{ client *Client }

func (p *miguProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	root, err := p.client.object(ctx, "https://"+miguSearchHost+"/search/v3/open-search", map[string]any{
		"appVersion": "6.1.1.00", "ct": 101, "isCorrectWord": 1, "k": title,
		"mediaSource": 9000000, "packId": miguPacks, "pageIdx": 1, "pageSize": 20,
		"copyrightTerminal": 3, "searchScene": 2, "uiVersion": "A3.26.0",
	})
	if err != nil {
		return nil, err
	}
	if err := business(root["code"], "200"); err != nil {
		return nil, err
	}
	rows := arr(obj(root["body"])["contentInfoList"])
	if rows == nil || len(rows) > 100 {
		return nil, schemaError(fmt.Errorf("invalid Migu search results"))
	}
	results := []Candidate{}
	seen := map[string]bool{}
	for _, value := range rows {
		row := obj(obj(value)["shortMediaAsset"])
		id, name := text(row["pID"]), plainTitle(text(row["name"]))
		if text(row["videoType"]) != "VOD" || text(row["programType"]) != "TV_PLAY" || text(row["isLong"]) != "1" ||
			!decimalID.MatchString(id) || name == "" || seen[id] {
			continue
		}
		results = append(results, Candidate{Provider: miguProviderID, Ref: id, Title: name, Year: metadataYear(text(row["year"])), Kind: "电视剧"})
		seen[id] = true
	}
	return results, nil
}

func (p *miguProvider) detail(ctx context.Context, id string) (map[string]any, error) {
	if !decimalID.MatchString(id) {
		return nil, apperror.New(apperror.Validation, "咪咕视频标识无效")
	}
	root, err := p.client.object(ctx, "https://"+miguCatalogHost+"/program/v4/cont/content-info/"+id+"/1", nil)
	if err != nil {
		return nil, err
	}
	if err := business(root["code"], "200"); err != nil {
		return nil, err
	}
	detail := obj(obj(root["body"])["data"])
	if detail == nil {
		return nil, schemaError(fmt.Errorf("missing Migu detail"))
	}
	return detail, nil
}

func (p *miguProvider) Episodes(ctx context.Context, series string) ([]Episode, error) {
	detail, err := p.detail(ctx, series)
	if err != nil {
		return nil, err
	}
	if text(detail["epsID"]) != series || text(detail["programType"]) != "TV_PLAY" {
		return nil, apperror.New(apperror.Unsupported, "咪咕视频未提供该电视剧的正片目录")
	}
	rows := arr(detail["datas"])
	if rows == nil || len(rows) > 200 {
		return nil, schemaError(fmt.Errorf("invalid Migu episode catalog"))
	}
	if text(detail["totalPage"]) != "1" {
		return nil, apperror.New(apperror.Unsupported, "咪咕视频目录超出单次匹配范围")
	}
	results := make([]Episode, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		id, title := text(row["pID"]), plainTitle(text(row["name"]))
		count, err := strconv.Atoi(text(row["index"]))
		if err != nil || count < 1 || count > 9999 || title == "" || matchExtra.MatchString(title) || !decimalID.MatchString(id) ||
			text(row["isPrevue"]) != "0" || text(row["programTypeV2"]) != "TV_PLAY" {
			continue
		}
		results = append(results, Episode{
			Provider: miguProviderID, SeriesID: series, EpisodeID: id, Title: title, Number: strconv.Itoa(count),
			DurationMS: clockDurationMS(text(row["duration"])), PageURL: miguWeb + "/p/detail/" + id,
		})
	}
	return results, nil
}

func (p *miguProvider) Index(ctx context.Context, episode Episode) (Index, error) {
	detail, err := p.detail(ctx, episode.EpisodeID)
	if err != nil {
		return Index{}, err
	}
	playing := obj(detail["playing"])
	duration := clockDurationMS(text(playing["duration"]))
	if duration <= 0 || text(playing["pID"]) != episode.EpisodeID || text(detail["epsID"]) != episode.SeriesID {
		return Index{}, schemaError(fmt.Errorf("Migu detail does not identify the requested episode"))
	}
	return regularIndex(duration, miguSegmentMS), nil
}

func (p *miguProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !decimalID.MatchString(episode.SeriesID) || !decimalID.MatchString(episode.EpisodeID) || !regularSegment(segment, miguSegmentMS) {
		return nil, 0, apperror.New(apperror.Validation, "咪咕视频弹幕分片标识无效")
	}
	path := fmt.Sprintf("/gateway/live_barrage/videox/barrage/v2/list/%s/%s/%d/%d/020",
		episode.SeriesID, episode.EpisodeID, segment.StartMS/1000, (segment.EndMS+999)/1000)
	endpoint := url.URL{Scheme: "https", Host: miguBarrageHost, Path: path}
	raw, err := p.client.read(ctx, endpoint.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	root, err := decodeMiguBarrage(raw)
	if err != nil {
		return nil, 0, err
	}
	if err := business(root["code"], "200"); err != nil {
		return nil, 0, err
	}
	rows := arr(obj(root["body"])["result"])
	if rows == nil || len(rows) > MaxSegmentItems {
		return nil, 0, schemaError(fmt.Errorf("invalid Migu barrage results"))
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		at, err := secondsMS(text(row["playtime"]))
		if err != nil || at < segment.StartMS || at >= segment.EndMS {
			continue
		}
		id := text(row["cid"])
		item := Item{ID: id, TimeMS: at, Text: text(row["msg"]), Mode: 1, Color: rgbColor(text(row["textcolor"]), 16), FontSize: 25}
		if len(id) <= 128 && validItem(item) {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}

// This fixed AES key identifies the anonymous gateway response encoding. It is
// shared by public playback clients and has no account or session privileges.
const miguBarrageKey = "\x62\x39\x36\x66\x61\x64\x64\x38\x31\x35\x30\x33\x64\x32\x37\x63\x32\x31\x32\x64\x31\x32\x36\x65\x37\x38\x31\x35\x35\x64\x39\x63"

func decodeMiguBarrage(raw []byte) (map[string]any, error) {
	encoded := strings.TrimSpace(string(raw))
	if strings.HasPrefix(encoded, "{") {
		return object(raw)
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(data) == 0 || len(data)%aes.BlockSize != 0 {
		return nil, schemaError(fmt.Errorf("invalid Migu barrage envelope"))
	}
	cipher, err := aes.NewCipher([]byte(miguBarrageKey))
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(data); start += aes.BlockSize {
		cipher.Decrypt(data[start:start+aes.BlockSize], data[start:start+aes.BlockSize])
	}
	padding := int(data[len(data)-1])
	if padding < 1 || padding > aes.BlockSize {
		return nil, schemaError(fmt.Errorf("invalid Migu barrage padding"))
	}
	end := len(data) - padding
	for _, b := range data[end:] {
		if int(b) != padding {
			return nil, schemaError(fmt.Errorf("invalid Migu barrage padding"))
		}
	}
	if !utf8.Valid(data[:end]) {
		return nil, schemaError(fmt.Errorf("invalid Migu barrage text"))
	}
	return object(data[:end])
}
