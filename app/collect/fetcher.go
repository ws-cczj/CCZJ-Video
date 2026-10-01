package collect

import (
	"cczjVideo/app/apperror"
	"compress/gzip"
	"context"
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
	"cczjVideo/app/netstats"

	"github.com/andybalholm/brotli"
)

var headers = http.Header{
	"User-Agent":      []string{"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36 Edg/137.0.0.0"},
	"Accept":          []string{"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8"},
	"Accept-Language": []string{"zh-CN,zh;q=0.9,en;q=0.8"},
	"Accept-Encoding": []string{"gzip, deflate, br"},
}

const maxFetchResponseBytes = 32 << 20

var client = &http.Client{
	Timeout: 30 * time.Second,
	Transport: netstats.WrapTransport(netstats.CategoryCollect, &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	}),
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
	// CodeText is the code the source actually returned, normalised to text so
	// 1, "1" and true are all comparable against a declarative ok_codes list.
	// The MAC-CMS path leaves it empty and keeps using Code.
	CodeText string `json:"-"`
	// OK records whether the envelope's code hit the shape's ok_codes. It is
	// set for declared envelopes only; the MAC-CMS verdict stays Code == 1.
	OK bool `json:"-"`
}

// HTTPError preserves transport semantics for retry policy decisions. Callers
// must not infer a status code by parsing an error message.
type HTTPError struct {
	StatusCode int
	RetryAfter time.Duration
	URL        string
}

func (e *HTTPError) Error() string { return fmt.Sprintf("HTTP %d for %s", e.StatusCode, e.URL) }

func retryAfter(header string) time.Duration {
	seconds, err := strconv.Atoi(strings.TrimSpace(header))
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if when, err := http.ParseTime(header); err == nil {
		if d := time.Until(when); d > 0 {
			return d
		}
	}
	return 0
}

// FetchOptions 可选的查询参数
type FetchOptions struct {
	Limit        int               // 单页条数（0 表示不指定，使用接口默认）
	Hours        int               // h=N 小时内更新（0 表示不指定）
	Keyword      string            // wd=xxx 关键词搜索（空表示不指定）
	TypeID       string            // t=xxx 分类 ID（空表示不指定）
	FieldMapping map[string]string // 字段映射：源字段名 → 目标字段名
	// Response 非空时按声明的信封解析（列表路径、成功码、分页字段），
	// 为 nil 时保持 MAC CMS 顶层信封的历史行为。
	Response *ResponseShape
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

// FetchVideoDetailWithURL is the operation-aware detail fetch entry point.
// Its URL must have been produced by a SourceStrategy; it never adds params.
func FetchVideoDetailWithURL(detailURL string, fieldMapping map[string]string) (*model.Video, error) {
	return FetchVideoDetailWithURLContext(context.Background(), detailURL, fieldMapping)
}

// FetchVideoDetailWithURLContext lets detail callers cancel an in-flight
// source request when the consumer has navigated away.
func FetchVideoDetailWithURLContext(ctx context.Context, detailURL string, fieldMapping map[string]string) (*model.Video, error) {
	result, err := doFetchContext(ctx, detailURL, fieldMapping)
	if err != nil {
		return nil, err
	}
	if len(result.List) > 0 {
		return result.List[0], nil
	}
	return nil, apperror.New(apperror.NotFound, "video not found")
}

// FetchPageURL consumes an operation-aware URL without adding parameters.
func FetchPageURL(target string, fieldMapping map[string]string) (*FetchResult, error) {
	if strings.TrimSpace(target) == "" {
		return nil, apperror.New(apperror.Validation, "request URL is empty")
	}
	return doFetch(target, fieldMapping)
}

func doFetch(target string, fieldMapping map[string]string) (*FetchResult, error) {
	return doFetchContext(context.Background(), target, fieldMapping)
}

// doFetchContext keeps the MAC-CMS envelope: strategies that declare no
// response block decode exactly as they always did.
func doFetchContext(ctx context.Context, target string, fieldMapping map[string]string) (*FetchResult, error) {
	return doFetchShape(ctx, target, fieldMapping, nil)
}

// shapeOfStrategy reports the envelope a strategy declares, if it advertises the
// optional ResponseShaper capability at all. Widening SourceStrategy would force
// every implementation to change; type-asserting keeps the seam intact.
func shapeOfStrategy(strategy SourceStrategy) *ResponseShape {
	shaper, ok := strategy.(ResponseShaper)
	if !ok {
		return nil
	}
	shape, ok := shaper.ResponseShape()
	if !ok {
		return nil
	}
	return shape
}

// FetchWithStrategy fetches an operation URL and decodes it with the envelope
// the same strategy declares. Callers that hold a strategy should prefer this
// over FetchPageURL, which only knows the MAC-CMS envelope.
func FetchWithStrategy(ctx context.Context, strategy SourceStrategy, target string) (*FetchResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if strings.TrimSpace(target) == "" {
		return nil, apperror.New(apperror.Validation, "request URL is empty")
	}
	if strategy == nil {
		return doFetchContext(ctx, target, nil)
	}
	return doFetchShape(ctx, target, strategy.GetFieldMapping(), shapeOfStrategy(strategy))
}

// FetchVideoDetailWithStrategy is FetchVideoDetailWithURLContext for sources that
// describe their own envelope: the detail list may live anywhere the strategy
// says it does.
func FetchVideoDetailWithStrategy(ctx context.Context, strategy SourceStrategy, detailURL string) (*model.Video, error) {
	result, err := FetchWithStrategy(ctx, strategy, detailURL)
	if err != nil {
		return nil, err
	}
	for _, video := range result.List {
		if video != nil {
			return video, nil
		}
	}
	return nil, apperror.New(apperror.NotFound, "video not found")
}

func doFetchShape(ctx context.Context, target string, fieldMapping map[string]string, shape *ResponseShape) (*FetchResult, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "create request")
	}
	for k, v := range headers {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, fmt.Sprintf("fetch %s", target))
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After")), URL: target}
	}
	if resp.ContentLength > maxFetchResponseBytes {
		return nil, apperror.Newf(apperror.Corrupt, "response exceeds %d bytes", maxFetchResponseBytes)
	}

	bodyReader, err := decompress(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "decompress")
	}

	body, err := io.ReadAll(io.LimitReader(bodyReader, maxFetchResponseBytes+1))
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, "read body")
	}
	if len(body) > maxFetchResponseBytes {
		return nil, apperror.Newf(apperror.Corrupt, "response exceeds %d bytes", maxFetchResponseBytes)
	}

	// ⭐ 预检：响应体是否为 JSON 格式，避免将纯文本错误消息当作 JSON 解析
	trimmed := strings.TrimSpace(string(body))
	if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		preview := trimmed
		if len(preview) > 300 {
			preview = preview[:300]
		}
		return nil, apperror.Newf(apperror.Corrupt, "源站返回了非JSON响应: %s", preview)
	}

	var result *FetchResult
	if shape == nil {
		result, err = decodeCMSEnvelope(body, fieldMapping)
	} else {
		result, err = decodeShapedEnvelope(body, shape, fieldMapping)
	}
	if err != nil {
		return nil, err
	}

	logFetchedPage(target, result)

	if !result.OK {
		if shape == nil {
			return nil, apperror.Newf(apperror.Unavailable, "api error: code=%d msg=%s", result.Code, result.Msg)
		}
		return nil, apperror.Newf(apperror.Unavailable, "api error: code=%s msg=%s", result.CodeText, result.Msg)
	}
	return result, nil
}

// decodeCMSEnvelope is the flat MAC-CMS decode used by every strategy that does
// not declare a response shape. Its behaviour is unchanged by the declarative
// work: the same mapping-aware list parse, the same best-effort metadata read,
// and the same "code == 1 means ok" verdict.
func decodeCMSEnvelope(body []byte, fieldMapping map[string]string) (*FetchResult, error) {
	result := &FetchResult{}

	videos, err := ParseVideosWithMapping(body, fieldMapping)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, fmt.Sprintf("parse json with mapping (body preview: %s)", bodyPreview(body)))
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
	result.OK = result.Code == 1
	return result, nil
}

// logFetchedPage keeps the one place the raw page is logged, so a shaped decode
// cannot drift from what the MAC-CMS path already reported.
func logFetchedPage(target string, result *FetchResult) {
	applog.Info("[FETCH] 原始响应 - URL: %s, Code: %d, Total: %d, ListSize: %d", target, result.Code, result.Total.Int(), len(result.List))
	if len(result.List) > 0 {
		for i, v := range result.List {
			if v == nil {
				continue
			}
			applog.Info("[FETCH] 视频[%d]原始数据 - vod_id: %s, vod_name: %s, vod_actor: %s, vod_director: %s, vod_content: %s, vod_pic: %s",
				i, v.VodId.String(), v.VodName, v.VodActor, v.VodDirector, truncate(v.VodContent, 100), v.VodPic)
		}
	}
}

func bodyPreview(body []byte) string {
	preview := string(body)
	if len(preview) > 300 {
		preview = preview[:300]
	}
	return preview
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

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
