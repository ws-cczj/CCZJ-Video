package collect

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/model"

	"github.com/andybalholm/brotli"
)

var headers = http.Header{
	"User-Agent":      []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36 Edg/137.0.0.0"},
	"Accept":          []string{"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
	"Accept-Language": []string{"zh-CN,zh;q=0.9,en;q=0.8"},
	"Accept-Encoding": []string{"gzip, deflate, br"},
}

var client = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	},
}

type FlexInt int

func (f *FlexInt) UnmarshalJSON(b []byte) error {
	s := strings.Trim(string(b), "\"")
	if s == "" || s == "null" {
		*f = 0
		return nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*f = FlexInt(n)
	return nil
}

func (f FlexInt) Int() int { return int(f) }

type FetchResult struct {
	Code      int            `json:"code"`
	Page      FlexInt        `json:"page"`
	Pagecount FlexInt        `json:"pagecount"`
	Limit     FlexInt        `json:"limit"`
	Total     FlexInt        `json:"total"`
	Msg       string         `json:"msg"`
	List      []*model.Video `json:"list"`
}

// FetchOptions 可选的查询参数
type FetchOptions struct {
	Limit        int               // 单页条数（0 表示不指定，使用接口默认）
	Hours        int               // h=N 小时内更新（0 表示不指定）
	Keyword      string            // wd=xxx 关键词搜索（空表示不指定）
	TypeID       string            // t=xxx 分类 ID（空表示不指定）
	FieldMapping map[string]string // 字段映射：源字段名 → 目标字段名
}

// BuildQueryUrl 根据 ApiUrl 拼接 pg/limit/h/wd 等参数，保持原有 ?ac=detail 等不变
// 规则：如果参数已存在则覆盖；否则追加
func BuildQueryUrl(apiUrl string, page int, opts FetchOptions) string {
	if apiUrl == "" {
		return ""
	}
	u, err := url.Parse(apiUrl)
	if err != nil {
		// 兜底：简单拼接
		return fmt.Sprintf("%s?pg=%d", apiUrl, page)
	}
	q := u.Query()

	// 基础页码
	q.Set("pg", strconv.Itoa(page))

	// limit（仅当 > 0 时设置）
	if opts.Limit > 0 {
		q.Set("limit", strconv.Itoa(opts.Limit))
	}
	// h（仅当 > 0 时设置，表示只拉取最近 N 小时更新）
	if opts.Hours > 0 {
		q.Set("h", strconv.Itoa(opts.Hours))
	}
	// wd 关键词搜索
	if strings.TrimSpace(opts.Keyword) != "" {
		q.Set("wd", strings.TrimSpace(opts.Keyword))
	}
	// t 分类筛选
	if strings.TrimSpace(opts.TypeID) != "" {
		q.Set("t", strings.TrimSpace(opts.TypeID))
	}

	u.RawQuery = q.Encode()
	return u.String()
}

func FetchPage(apiUrl string, page int) (*FetchResult, error) {
	return FetchPageWithOpts(apiUrl, page, FetchOptions{})
}

func FetchPageWithOpts(apiUrl string, page int, opts FetchOptions) (*FetchResult, error) {
	target := BuildQueryUrl(apiUrl, page, opts)
	return doFetch(target, opts.FieldMapping)
}

func FetchVideoDetail(apiUrl string, vodId string) (*model.Video, error) {
	u, err := url.Parse(apiUrl)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	q := u.Query()
	q.Set("ac", "detail")
	q.Set("ids", vodId)
	u.RawQuery = q.Encode()
	target := u.String()

	result, err := doFetch(target, nil)
	if err != nil {
		return nil, err
	}
	if len(result.List) > 0 {
		return result.List[0], nil
	}
	return nil, fmt.Errorf("video not found: %s", vodId)
}

func fetchVideoDetailWithStrategy(detailUrl string, fieldMapping map[string]string) (*model.Video, error) {
	result, err := doFetch(detailUrl, fieldMapping)
	if err != nil {
		return nil, err
	}
	if len(result.List) > 0 {
		return result.List[0], nil
	}
	return nil, fmt.Errorf("video not found")
}

func doFetch(target string, fieldMapping map[string]string) (*FetchResult, error) {
	req, err := http.NewRequest("GET", target, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range headers {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", target, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d for %s", resp.StatusCode, target)
	}

	bodyReader, err := decompress(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, fmt.Errorf("decompress: %w", err)
	}

	body, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	result := &FetchResult{}

	videos, err := ParseVideosWithMapping(body, fieldMapping)
	if err != nil {
		preview := string(body)
		if len(preview) > 300 {
			preview = preview[:300]
		}
		return nil, fmt.Errorf("parse json with mapping: %w (body preview: %s)", err, preview)
	}
	result.List = videos

	var meta struct {
		Code      int     `json:"code"`
		Page      FlexInt `json:"page"`
		Pagecount FlexInt `json:"pagecount"`
		Limit     FlexInt `json:"limit"`
		Total     FlexInt `json:"total"`
		Msg       string  `json:"msg"`
	}
	if err := json.Unmarshal(body, &meta); err == nil {
		result.Code = meta.Code
		result.Page = meta.Page
		result.Pagecount = meta.Pagecount
		result.Limit = meta.Limit
		result.Total = meta.Total
		result.Msg = meta.Msg
	}

	applog.Info("[FETCH] 原始响应 - URL: %s, Code: %d, Total: %d, ListSize: %d", target, result.Code, result.Total.Int(), len(result.List))
	if len(result.List) > 0 {
		for i, v := range result.List {
			if v == nil { continue }
			applog.Info("[FETCH] 视频[%d]原始数据 - vod_id: %s, vod_name: %s, vod_actor: %s, vod_director: %s, vod_content: %s, vod_pic: %s",
				i, v.VodId.String(), v.VodName, v.VodActor, v.VodDirector, truncate(v.VodContent, 100), v.VodPic)
		}
	}

	if result.Code != 1 {
		return nil, fmt.Errorf("api error: code=%d msg=%s", result.Code, result.Msg)
	}
	return result, nil
}

func decompress(r io.Reader, encoding string) (io.Reader, error) {
	switch strings.ToLower(encoding) {
	case "gzip", "deflate":
		gzReader, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("gzip decompress: %w", err)
		}
		return gzReader, nil
	case "br":
		return io.NopCloser(brotli.NewReader(r)), nil
	default:
		return io.NopCloser(r), nil
	}
}

func FetchAll(apiUrl string, progress func(current, total int)) (*FetchResult, error) {
	first, err := FetchPage(apiUrl, 1)
	if err != nil {
		return nil, err
	}
	all := first
	if progress != nil {
		progress(1, first.Pagecount.Int())
	}

	for p := 2; p <= first.Pagecount.Int(); p++ {
		page, err := FetchPage(apiUrl, p)
		if err != nil {
			return nil, fmt.Errorf("fetch page %d: %w", p, err)
		}
		all.List = append(all.List, page.List...)
		if progress != nil {
			progress(p, first.Pagecount.Int())
		}
		time.Sleep(2 * time.Second)
	}
	return all, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
