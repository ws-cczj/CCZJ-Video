package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"cczjVideo/app/apperror"
)

// BrowseChannel is an implemented, source-owned browsing route, independent of search.
type BrowseChannel struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Category    string `json:"-"`
}

var fanqieBrowseChannels = []BrowseChannel{
	{ID: "popular", Title: "阅读榜", Description: "正在被读者翻开的故事"},
	{ID: "publication", Title: "出版作品", Description: "从网络连载走向纸页"},
	{ID: "history", Title: "历史古代", Description: "走进另一个时代", Category: "273"},
	{ID: "fantasy", Title: "东方玄幻", Description: "让想象走得更远", Category: "511"},
	{ID: "mystery", Title: "悬疑推理", Description: "在字里行间寻找线索", Category: "506"},
	{ID: "city", Title: "都市异能", Description: "日常之外的另一种可能", Category: "516"},
	{ID: "xianxia", Title: "奇幻仙侠", Description: "山海之间，自有天地", Category: "259"},
}

// NovelBrowseChannels returns a copy so consumers cannot alter protocol routes.
func NovelBrowseChannels() []BrowseChannel {
	return append([]BrowseChannel(nil), fanqieBrowseChannels...)
}

// BrowsePage keeps upstream offsets distinct from the number of retained text books.
type BrowsePage struct {
	Items      []*Item
	NextOffset int
	HasNext    bool
}

const browsePageSize = 20

func browseChannel(id string) (BrowseChannel, bool) {
	for _, channel := range fanqieBrowseChannels {
		if channel.ID == id {
			return channel, true
		}
	}
	return BrowseChannel{}, false
}

// Browse fetches a bounded page from a compiled text provider. It accepts no URL.
func (p *Pipeline) Browse(ctx context.Context, channelID string, offset int) (*BrowsePage, error) {
	channel, ok := browseChannel(channelID)
	if !ok || offset < 0 || offset > 10000 {
		return nil, apperror.New(apperror.Validation, "浏览分类或分页位置无效")
	}
	if p.Doc.Adapter != "fanqie-web" || p.Doc.Kind != "novel" {
		return nil, apperror.New(apperror.Unsupported, "这个源尚未提供小说浏览")
	}
	query := url.Values{"aid": {"1967"}, "offset": {strconv.Itoa(offset)}, "limit": {strconv.Itoa(browsePageSize)}}
	address := "https://api.fanqiesdk.com/api/novel/channel/homepage/new_category/book_list/v1/"
	switch channelID {
	case "popular":
		address = "https://api-lf.fanqiesdk.com/api/novel/channel/homepage/rank/rank_list/v2/"
		query.Set("aid", "13")
		query.Set("side_type", "10")
		query.Set("type", "1")
	case "publication":
		address = fanqieOrigin + "/api/node/publication/list"
		query = url.Values{"page_index": {strconv.Itoa(offset / browsePageSize)}, "page_count": {strconv.Itoa(browsePageSize)}}
	default:
		query.Set("category_id", channel.Category)
		query.Set("gender", "1")
	}
	response, err := p.fanqieFetch(ctx, address+"?"+query.Encode())
	if err != nil {
		return nil, err
	}
	return decodeFanqieBrowse(response.Text, channelID, offset)
}

type fanqieBrowseBook struct {
	ID       string `json:"book_id"`
	Title    string `json:"book_name"`
	Author   string `json:"author"`
	Cover    string `json:"thumb_url"`
	Summary  string `json:"abstract"`
	Category string `json:"category"`
	Genre    string `json:"genre"`
	Status   string `json:"creation_status"`
	Words    string `json:"word_number"`
	Latest   string `json:"last_chapter_title"`
	BookType string `json:"book_type"`
}

// The rank API returns a boolean; category pages use numeric 0/1.
type fanqieBrowseFlag bool

func (flag *fanqieBrowseFlag) UnmarshalJSON(raw []byte) error {
	switch string(raw) {
	case "true", "1":
		*flag = true
	case "false", "0":
		*flag = false
	default:
		return fmt.Errorf("invalid browse continuation flag")
	}
	return nil
}

func decodeFanqieBrowse(text, channel string, offset int) (*BrowsePage, error) {
	var payload struct {
		Code *int `json:"code"`
		Data *struct {
			Rows         []fanqieBrowseBook `json:"data"`
			Rank         []fanqieBrowseBook `json:"result"`
			Publications []fanqieBrowseBook `json:"publication_list"`
			HasMore      fanqieBrowseFlag   `json:"has_more"`
			Total        int                `json:"total_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "浏览书目格式已变化")
	}
	if payload.Code == nil || *payload.Code != 0 || payload.Data == nil {
		return nil, apperror.New(apperror.Unavailable, "书目暂时无法读取，请稍后再试")
	}
	rows := payload.Data.Rows
	if channel == "popular" {
		rows = payload.Data.Rank
	} else if channel == "publication" {
		rows = payload.Data.Publications
	}
	if rows == nil || len(rows) > 200 {
		return nil, apperror.New(apperror.Corrupt, "来源没有返回有效的书目列表")
	}
	out := &BrowsePage{Items: []*Item{}, NextOffset: offset + len(rows), HasNext: bool(payload.Data.HasMore)}
	if channel == "publication" {
		out.NextOffset = offset + browsePageSize
		out.HasNext = out.NextOffset < payload.Data.Total && len(rows) > 0
	}
	seen := make(map[string]bool)
	for _, row := range rows {
		// This mixed-media API also serves comics/audio. Only publication's
		// dedicated endpoint has text-book semantics without a genre field.
		if channel != "publication" && row.Genre != "0" && row.Genre != "6" {
			continue
		}
		if !fanqieNumber.MatchString(row.ID) || row.Title == "" || seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		genres := row.Category
		if channel == "publication" {
			genres = row.BookType
		}
		words := ""
		if count, err := strconv.ParseInt(row.Words, 10, 64); err == nil && count > 0 {
			words = fanqieWordCount(count)
		}
		out.Items = append(out.Items, &Item{RemoteID: row.ID, URL: fanqieOrigin + "/page/" + row.ID,
			Title: fanqiePlain(row.Title), Author: row.Author, Cover: row.Cover, Summary: row.Summary,
			Genres: genres, Status: fanqieCreationStatus(row.Status), Words: words, Latest: row.Latest})
	}
	if len(rows) == 0 || out.NextOffset > 10000 {
		out.HasNext = false
	}
	return out, nil
}

func fanqieWordCount(count int64) string {
	if count <= 0 {
		return ""
	}
	if count < 10000 {
		return fmt.Sprintf("%d字", count)
	}
	return fmt.Sprintf("%.1f万字", float64(count)/10000)
}

func fanqieCreationStatus(value string) string {
	switch value {
	case "0":
		return "完结"
	case "1":
		return "连载"
	default:
		return ""
	}
}
