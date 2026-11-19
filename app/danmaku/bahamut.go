package danmaku

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/textvariant"
)

const (
	bahamutProviderID = "bahamut"
	bahamutAPIHost    = "api.gamer.com.tw"
	bahamutAgent      = "Anime/2.29.2 (7N5749MM3F.tw.com.gamer.anime; build:972; iOS 26.0.0) Alamofire/5.6.4"
	bahamutPage       = "https://ani.gamer.com.tw/animeVideo.php?sn="
	bahamutPoolKey    = "episode"
	bahamutMaxItems   = 12_000
)

type bahamutProvider struct{ client *Client }

func (p *bahamutProvider) Search(ctx context.Context, title string) ([]Candidate, error) {
	query := url.Values{"kw": {textvariant.Traditional(title)}}
	root, err := p.client.object(ctx, "https://"+bahamutAPIHost+"/mobile_app/anime/v1/search.php?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	rows := arr(root["anime"])
	if rows == nil || len(rows) > 100 {
		return nil, schemaError(fmt.Errorf("invalid Bahamut search results"))
	}
	results := []Candidate{}
	seen := map[string]bool{}
	for _, value := range rows {
		row := obj(value)
		id := text(row["video_sn"])
		name := plainTitle(textvariant.Simplified(text(row["title"])))
		if !decimalID.MatchString(id) || name == "" || seen[id] {
			continue
		}
		results = append(results, Candidate{Provider: bahamutProviderID, Ref: id, Title: name, Year: metadataYear(text(row["info"])), Kind: "animation"})
		seen[id] = true
	}
	return results, nil
}

func (p *bahamutProvider) detail(ctx context.Context, id string) (map[string]any, map[string]any, error) {
	if !decimalID.MatchString(id) {
		return nil, nil, apperror.New(apperror.Validation, "巴哈姆特单集标识无效")
	}
	query := url.Values{"videoSn": {id}}
	root, err := p.client.object(ctx, "https://"+bahamutAPIHost+"/anime/v1/video.php?"+query.Encode(), nil)
	if err != nil {
		return nil, nil, err
	}
	data := obj(root["data"])
	video, anime := obj(data["video"]), obj(data["anime"])
	if text(video["videoSn"]) != id || !decimalID.MatchString(text(video["animeSn"])) || text(video["animeSn"]) != text(anime["animeSn"]) {
		return nil, nil, schemaError(fmt.Errorf("Bahamut video identity does not match"))
	}
	return video, anime, nil
}

func (p *bahamutProvider) Episodes(ctx context.Context, ref string) ([]Episode, error) {
	video, anime, err := p.detail(ctx, ref)
	if err != nil {
		return nil, err
	}
	// The current video type selects its language group. Other groups may
	// contain the same episode numbers with separate comment timelines.
	rows := arr(obj(anime["episodes"])[text(video["type"])])
	if rows == nil || len(rows) > 500 {
		return nil, schemaError(fmt.Errorf("invalid Bahamut episode group"))
	}
	series := text(anime["animeSn"])
	title := plainTitle(textvariant.Simplified(text(anime["title"])))
	title, _, _ = strings.Cut(title, " [")
	results := make([]Episode, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		id, label := text(row["videoSn"]), text(row["episode"])
		if _, ok := episodeOrdinal(label); !ok || !decimalID.MatchString(id) {
			continue
		}
		results = append(results, Episode{Provider: bahamutProviderID, SeriesID: series, EpisodeID: id,
			Title: "第" + label + "集 " + strings.TrimSpace(title), Number: label, PageURL: bahamutPage + id})
	}
	return results, nil
}

func (p *bahamutProvider) Index(ctx context.Context, episode Episode) (Index, error) {
	video, _, err := p.detail(ctx, episode.EpisodeID)
	if err != nil {
		return Index{}, err
	}
	minutes, err := strconv.ParseInt(text(video["duration"]), 10, 64)
	if err != nil || minutes < 1 || minutes >= 240 || text(video["animeSn"]) != episode.SeriesID {
		return Index{}, schemaError(fmt.Errorf("invalid Bahamut episode duration or series"))
	}
	// The site reports whole minutes, so include the remainder of the final
	// minute when clamping decisecond timestamps.
	return Index{Segments: []Segment{{Key: bahamutPoolKey, EndMS: (minutes + 1) * 60_000}}}, nil
}

func (p *bahamutProvider) Segment(ctx context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	if !decimalID.MatchString(episode.EpisodeID) || !decimalID.MatchString(episode.SeriesID) ||
		segment.Key != bahamutPoolKey || segment.StartMS != 0 || segment.EndMS <= 0 || segment.EndMS > 14_400_000 {
		return nil, 0, apperror.New(apperror.Validation, "巴哈姆特弹幕标识无效")
	}
	query := url.Values{"videoSn": {episode.EpisodeID}, "geo": {"TW,HK"}}
	root, err := p.client.object(ctx, "https://"+bahamutAPIHost+"/anime/v1/danmu.php?"+query.Encode(), nil)
	if err != nil {
		return nil, 0, err
	}
	rows := arr(obj(root["data"])["danmu"])
	if rows == nil || len(rows) > bahamutMaxItems {
		return nil, 0, schemaError(fmt.Errorf("invalid Bahamut episode pool"))
	}
	items := make([]Item, 0, len(rows))
	for _, value := range rows {
		row := obj(value)
		tick, err := strconv.ParseInt(text(row["time"]), 10, 64)
		if err != nil || tick < 0 || tick >= segment.EndMS/100 {
			continue
		}
		mode := 1
		switch text(row["position"]) {
		case "1":
			mode = 5
		case "2":
			mode = 4
		case "0":
		default:
			continue
		}
		item := Item{ID: text(row["sn"]), TimeMS: tick * 100, Text: text(row["text"]), Mode: mode,
			Color: rgbColor(text(row["color"]), 16), FontSize: 25}
		if len(item.ID) <= 128 && validItem(item) {
			items = append(items, item)
		}
	}
	return sorted(items), len(rows) - len(items), nil
}
