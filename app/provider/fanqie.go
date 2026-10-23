package provider

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/model"
)

const fanqieOrigin = "https://fanqienovel.com"
const fanqieSearchURL = "https://api.fanqiesdk.com/api/novel/channel/homepage/search/search/v1/"

// The website's public text uses a font-specific substitution alphabet. Pin the
// observed font revision; an unknown font or code point is an explicit failure,
// never plausible-looking corrupted prose cached as a successful chapter.
//
//go:embed fanqie_font.json
var fanqieFontJSON []byte
var fanqieFontTable = func() map[string]map[string]string {
	var table map[string]map[string]string
	_ = json.Unmarshal(fanqieFontJSON, &table)
	return table
}()
var fanqieFontPattern = regexp.MustCompile(`/awesome-font/c/([a-f0-9]+)\.woff2?`)
var fanqieNumber = regexp.MustCompile(`^[0-9]{1,24}$`)

func (p *Pipeline) fanqieFetch(ctx context.Context, address string) (*Response, error) {
	return p.Fetcher.Fetch(ctx, p.Doc, &Operation{Format: "html"}, address, Target{URL: address, PreserveQuery: true})
}

func fanqieID(address, prefix string) (string, error) {
	parsed, err := url.Parse(address)
	if err != nil || parsed.Scheme != "https" || parsed.Host != "fanqienovel.com" || parsed.User != nil {
		return "", apperror.New(apperror.Validation, "番茄源仅接受官网作品与章节地址")
	}
	id := strings.TrimPrefix(parsed.Path, prefix)
	if !strings.HasPrefix(parsed.Path, prefix) || !fanqieNumber.MatchString(id) {
		return "", apperror.New(apperror.Validation, "番茄作品或章节标识无效")
	}
	return id, nil
}

func (p *Pipeline) fanqieSearch(ctx context.Context, keyword string, page int, next string) (*ItemPage, error) {
	offset := max(0, page-1) * 10
	if next != "" {
		parsed, err := url.Parse(next)
		if err != nil || parsed.Scheme != "https" || parsed.Host != "api.fanqiesdk.com" || parsed.Path != "/api/novel/channel/homepage/search/search/v1/" {
			return nil, apperror.New(apperror.Validation, "番茄搜索游标无效")
		}
		offset, err = strconv.Atoi(parsed.Query().Get("offset"))
		if err != nil || offset < 0 || offset > 100000 {
			return nil, apperror.New(apperror.Validation, "番茄搜索偏移无效")
		}
	}
	query := url.Values{"q": {keyword}, "offset": {strconv.Itoa(offset)}, "aid": {"13"}, "app_name": {"news_article"}, "version_code": {"708"}, "device_platform": {"android"}}
	response, err := p.fanqieFetch(ctx, fanqieSearchURL+"?"+query.Encode())
	if err != nil {
		return nil, err
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			HasMore bool `json:"has_more"`
			Offset  int  `json:"offset"`
			Rows    []struct {
				ID       string          `json:"book_id"`
				Title    string          `json:"title"`
				Author   string          `json:"author"`
				Summary  string          `json:"abstract"`
				Cover    string          `json:"thumb_url"`
				Category string          `json:"category"`
				Genre    json.RawMessage `json:"genre"`
				Status   string          `json:"creation_status"`
			} `json:"ret_data"`
		} `json:"data"`
	}
	if err = json.Unmarshal([]byte(response.Text), &payload); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "番茄搜索响应格式已变化")
	}
	if payload.Code != 0 {
		return nil, apperror.New(apperror.Unavailable, "番茄搜索暂不可用，请稍后重试")
	}
	out := &ItemPage{Items: []*Item{}}
	for _, row := range payload.Data.Rows {
		genre := strings.Trim(string(row.Genre), `"`)
		// Audio and comic hits share this endpoint; never feed them to text readers.
		if (genre != "0" && genre != "6") || !fanqieNumber.MatchString(row.ID) {
			continue
		}
		title := fanqiePlain(row.Title)
		if title == "" {
			continue
		}
		status := fanqieCreationStatus(row.Status)
		out.Items = append(out.Items, &Item{RemoteID: row.ID, Title: title, Author: row.Author, Summary: row.Summary, Cover: row.Cover, Genres: row.Category, Status: status, URL: fanqieOrigin + "/page/" + row.ID})
	}
	if payload.Data.HasMore && payload.Data.Offset > offset {
		query.Set("offset", strconv.Itoa(payload.Data.Offset))
		out.NextURL = fanqieSearchURL + "?" + query.Encode()
		out.HasNext = true
	}
	return out, nil
}

func fanqieState(text string) (map[string]json.RawMessage, error) {
	marker := "window.__INITIAL_STATE__"
	index := strings.Index(text, marker)
	if index < 0 {
		return nil, apperror.New(apperror.Corrupt, "番茄页面未提供阅读数据，可能需要验证")
	}
	tail := text[index+len(marker):]
	equals := strings.Index(tail, "=")
	if equals < 0 {
		return nil, apperror.New(apperror.Corrupt, "番茄页面数据格式已变化")
	}
	var state map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(tail[equals+1:]))
	if err := decoder.Decode(&state); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "番茄页面数据格式已变化")
	}
	return state, nil
}

func (p *Pipeline) fanqieDetail(ctx context.Context, address string) (*Item, error) {
	id, err := fanqieID(address, "/page/")
	if err != nil {
		return nil, err
	}
	response, err := p.fanqieFetch(ctx, address)
	if err != nil {
		return nil, err
	}
	state, err := fanqieState(response.Text)
	if err != nil {
		return nil, err
	}
	var page struct {
		ID       string `json:"bookId"`
		Name     string `json:"bookName"`
		Author   string `json:"author"`
		Cover    string `json:"thumbUri"`
		Summary  string `json:"abstract"`
		Words    int64  `json:"wordNumber"`
		Status   *int   `json:"creationStatus"`
		Latest   string `json:"lastChapterTitle"`
		Category string `json:"category"`
	}
	if err = json.Unmarshal(state["page"], &page); err != nil || page.Name == "" || page.ID != id {
		return nil, apperror.New(apperror.Corrupt, "番茄详情缺少匹配的作品数据")
	}
	status := ""
	if page.Status != nil {
		status = fanqieCreationStatus(strconv.Itoa(*page.Status))
	}
	return &Item{RemoteID: id, URL: fanqieOrigin + "/page/" + id, TocURL: fanqieOrigin + "/page/" + id, Title: page.Name, Author: page.Author, Cover: page.Cover, Summary: page.Summary, Words: fanqieWordCount(page.Words), Status: status, Latest: page.Latest, Genres: page.Category}, nil
}

func (p *Pipeline) fanqieToc(ctx context.Context, item *Item) ([]*Chapter, error) {
	if item == nil {
		return nil, apperror.New(apperror.Validation, "缺少番茄作品")
	}
	id, err := fanqieID(item.URL, "/page/")
	if err != nil {
		return nil, err
	}
	response, err := p.fanqieFetch(ctx, fanqieOrigin+"/api/reader/directory/detail?bookId="+id)
	if err != nil {
		return nil, err
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Chapters [][]struct {
				ID              string `json:"itemId"`
				Title           string `json:"title"`
				Volume          string `json:"volume_name"`
				NeedPay         int    `json:"needPay"`
				Locked          bool   `json:"isChapterLock"`
				PaidPublication bool   `json:"isPaidPublication"`
				PaidStory       bool   `json:"isPaidStory"`
			} `json:"chapterListWithVolume"`
		} `json:"data"`
	}
	if err = json.Unmarshal([]byte(response.Text), &payload); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "番茄目录格式已变化")
	}
	if payload.Code != 0 {
		return nil, apperror.New(apperror.Unavailable, "番茄目录暂不可用")
	}
	out := []*Chapter{}
	seen := map[string]bool{}
	for _, volume := range payload.Data.Chapters {
		for _, row := range volume {
			if !fanqieNumber.MatchString(row.ID) || row.Title == "" || seen[row.ID] {
				continue
			}
			seen[row.ID] = true
			access := model.ChapterAccessAvailable
			if row.NeedPay != 0 || row.Locked || row.PaidPublication || row.PaidStory {
				access = model.ChapterAccessSubscription
			}
			out = append(out, &Chapter{RemoteID: row.ID, Title: row.Title, Volume: row.Volume, URL: fanqieOrigin + "/reader/" + row.ID, Ord: len(out), Access: access})
		}
	}
	if len(out) == 0 {
		return nil, apperror.New(apperror.Corrupt, "番茄目录没有可识别章节")
	}
	return out, nil
}

func (p *Pipeline) fanqieContent(ctx context.Context, chapter *Chapter) (*TextContent, error) {
	if chapter == nil {
		return nil, apperror.New(apperror.Validation, "缺少番茄章节")
	}
	id, err := fanqieID(chapter.URL, "/reader/")
	if err != nil {
		return nil, err
	}
	if chapter.Access == model.ChapterAccessSubscription {
		return nil, apperror.New(apperror.PermissionDenied, "此章节需要在番茄官方客户端获取访问权限")
	}
	response, err := p.fanqieFetch(ctx, chapter.URL)
	if err != nil {
		return nil, err
	}
	return decodeFanqieContent(response.Text, id, p.Limits.MaxContentBytes)
}

func decodeFanqieContent(page, id string, maxBytes int) (*TextContent, error) {
	state, err := fanqieState(page)
	if err != nil {
		return nil, err
	}
	var reader struct {
		Chapter struct {
			ID              string `json:"itemId"`
			Content         string `json:"content"`
			NeedPay         int    `json:"needPay"`
			Locked          bool   `json:"isChapterLock"`
			PaidPublication bool   `json:"isPaidPublication"`
			PaidStory       bool   `json:"isPaidStory"`
		} `json:"chapterData"`
	}
	if err = json.Unmarshal(state["reader"], &reader); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "番茄章节数据格式已变化")
	}
	row := reader.Chapter
	if row.NeedPay != 0 || row.Locked || row.PaidPublication || row.PaidStory {
		return nil, apperror.New(apperror.PermissionDenied, "此章节需要在番茄官方客户端获取访问权限")
	}
	if row.ID != id || strings.TrimSpace(row.Content) == "" {
		return nil, apperror.New(apperror.Unavailable, "番茄未提供此章节的公开正文")
	}
	content, err := decodeFanqieFont(row.Content, page)
	if err != nil {
		return nil, err
	}
	extractor, err := NewExtractor("html", content)
	if err != nil {
		return nil, err
	}
	text, err := extractor.String(extractor.Root(), &Selector{Attr: "text"})
	if err != nil {
		return nil, err
	}
	if LooksGarbled(text) {
		return nil, apperror.New(apperror.Corrupt, "番茄正文字符异常，请重新检测源")
	}
	paragraphs := SplitParagraphs(text)
	if len(paragraphs) == 0 {
		return nil, apperror.New(apperror.Corrupt, "番茄正文为空")
	}
	if len(text) > maxBytes {
		return nil, apperror.New(apperror.Corrupt, "番茄正文超过单章大小上限")
	}
	return &TextContent{Paragraphs: paragraphs}, nil
}

func decodeFanqieFont(content, page string) (string, error) {
	var table map[string]string
	needsTable := false
	for _, r := range content {
		if r >= 0xe000 && r <= 0xf8ff {
			needsTable = true
			break
		}
	}
	if !needsTable {
		return content, nil
	}
	if match := fanqieFontPattern.FindStringSubmatch(page); match != nil {
		table = fanqieFontTable[match[1]]
	}
	if table == nil {
		return "", apperror.New(apperror.Unsupported, "番茄网页字体版本已更新，请更新内置源适配；未缓存乱码正文")
	}
	var decoded strings.Builder
	for _, r := range content {
		if r >= 0xe000 && r <= 0xf8ff {
			key := strconv.Itoa(int(r))
			replacement, ok := table[key]
			if !ok || replacement == "" {
				if canonical, legacy := legacyGBKRune(r); legacy {
					decoded.WriteRune(canonical)
					continue
				}
				return "", apperror.New(apperror.Unsupported, fmt.Sprintf("番茄字体包含未知字符 U+%04X，请更新适配", r))
			}
			decoded.WriteString(replacement)
		} else {
			decoded.WriteRune(r)
		}
	}
	return decoded.String(), nil
}

func fanqiePlain(value string) string {
	extractor, err := NewExtractor("html", value)
	if err != nil {
		return ""
	}
	text, _ := extractor.String(extractor.Root(), &Selector{Attr: "text"})
	return text
}
