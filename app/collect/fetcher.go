package collect

import (
	"bufio"
	"bytes"
	"cczjVideo/app/apperror"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/operation"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"context"
	"encoding/json"
	"errors"
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
	Transport: netpolicy.WrapTransport(string(netstats.CategoryCollect), netstats.WrapTransport(netstats.CategoryCollect, &http.Transport{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 5,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  true,
	})),
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
	RawCount      int            `json:"-"`
	SearchLimited bool           `json:"-"`
	HTTPStatus    int            `json:"-"`
	ResponseBytes int            `json:"-"`
	Code          int            `json:"code"`
	Page          FlexInt        `json:"page"`
	Pagecount     FlexInt        `json:"pagecount"`
	Limit         FlexInt        `json:"limit"`
	Total         FlexInt        `json:"total"`
	Msg           string         `json:"msg"`
	List          []*model.Video `json:"list"`
	// CodeText is the code the source actually returned, normalised to text so
	// 1, "1" and true are all comparable against a declarative ok_codes list.
	// The MAC-CMS path leaves it empty and keeps using Code.
	CodeText string `json:"-"`
	// OK records whether the envelope's code hit the shape's ok_codes. It is
	// set for declared envelopes only; the MAC-CMS verdict stays Code == 1.
	OK bool `json:"-"`
}

// APIError preserves the remote protocol code for operation-specific decisions.
// A collection refusal must not change the meaning of a detail or list request.
type APIError struct {
	Code     int
	CodeText string
	Message  string
}

func (e *APIError) Error() string {
	if e.CodeText != "" {
		return fmt.Sprintf("api error: code=%s msg=%s", e.CodeText, e.Message)
	}
	return fmt.Sprintf("api error: code=%d msg=%s", e.Code, e.Message)
}

var errSearchUnsupported = errors.New("source keyword search unsupported")

// ResponseError retains HTTP evidence when decompression or decoding fails.
type ResponseError struct {
	StatusCode int
	Stage      string
	Err        error
}

func (e *ResponseError) Error() string { return e.Err.Error() }
func (e *ResponseError) Unwrap() error { return e.Err }

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

// FetchVideoDetailWithStrategy builds and decodes the source's detail request,
// returning only the requested source ID, even if the upstream returns a list.
func FetchVideoDetailWithStrategy(ctx context.Context, strategy SourceStrategy, vodID string) (*model.Video, error) {
	vodID = strings.TrimSpace(vodID)
	if vodID == "" {
		return nil, apperror.New(apperror.Validation, "video ID is required")
	}
	detailURL := strategy.BuildDetailUrl(vodID)
	result, err := FetchWithStrategy(ctx, strategy, detailURL)
	if err != nil {
		return nil, err
	}
	for _, video := range result.List {
		if video != nil && video.VodId.String() == vodID {
			return video, nil
		}
	}
	if len(result.List) > 0 {
		return nil, apperror.New(apperror.Corrupt, "源站详情与请求的影片身份不一致")
	}
	return nil, apperror.New(apperror.NotFound, "video not found")
}

func doFetchShape(ctx context.Context, target string, fieldMapping map[string]string, shape *ResponseShape) (output *FetchResult, failure error) {
	began := time.Now()
	stage := "request"
	fields := operation.Fields(ctx, applog.Fields{"url": applog.SafeURL(target), "method": "GET", "decoder": "cms", "mapping_sha256": ""})
	config, _ := json.Marshal(struct {
		Mapping map[string]string
		Shape   *ResponseShape
	}{fieldMapping, shape})
	fields["mapping_sha256"] = applog.Fingerprint(config)
	if shape != nil {
		fields["decoder"], fields["list_path"] = "declared", shape.withDefaults().ListPath
	}
	defer func() {
		fields["stage"], fields["duration_ms"] = stage, time.Since(began).Milliseconds()
		if failure != nil {
			fields["failure_kind"] = FetchFailureKind(failure)
			fields["outcome"], fields["code"], fields["error"] = "failed", apperror.Public(failure).Code, apperror.Diagnostic(failure)
			applog.WarnFields("source fetch finished", fields)
		} else {
			fields["outcome"] = "success"
			applog.InfoFields("source fetch finished", fields)
		}
	}()
	req, err := http.NewRequestWithContext(ctx, "GET", target, nil)
	if err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "create request")
	}
	for k, v := range headers {
		req.Header[http.CanonicalHeaderKey(k)] = v
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, "请求源站失败")
	}
	defer resp.Body.Close()
	fields["http_status"], fields["content_type"], fields["encoding"] = resp.StatusCode, resp.Header.Get("Content-Type"), resp.Header.Get("Content-Encoding")
	if resp.Request != nil {
		fields["response_url"] = applog.SafeURL(resp.Request.URL.String())
	}
	defer func() {
		if failure != nil {
			failure = &ResponseError{StatusCode: resp.StatusCode, Stage: stage, Err: failure}
		}
	}()

	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode, RetryAfter: retryAfter(resp.Header.Get("Retry-After")), URL: target}
	}
	stage = "decompress"
	if resp.ContentLength > maxFetchResponseBytes {
		return nil, apperror.Newf(apperror.Corrupt, "response exceeds %d bytes", maxFetchResponseBytes)
	}

	bodyReader, err := decompress(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "decompress")
	}
	defer bodyReader.Close()

	stage = "read_body"
	body, err := io.ReadAll(io.LimitReader(bodyReader, maxFetchResponseBytes+1))
	fields["response_bytes"], fields["response_sha256"] = len(body), applog.Fingerprint(body)
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, "read body")
	}
	if len(body) > maxFetchResponseBytes {
		return nil, apperror.Newf(apperror.Corrupt, "response exceeds %d bytes", maxFetchResponseBytes)
	}

	stage = "decode"
	body = bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf})
	trimmed := strings.TrimSpace(string(body))
	if shape == nil && strings.HasPrefix(trimmed, "<") && (strings.HasPrefix(trimmed, "<?xml") || strings.HasPrefix(trimmed, "<rss")) {
		body, err = normalizeCMSXML(body)
		if err != nil {
			return nil, apperror.Wrap(apperror.Corrupt, err, "源站 XML 响应无效")
		}
		trimmed = strings.TrimSpace(string(body))
	}
	if trimmed == "暂不支持搜索" {
		return nil, apperror.Wrap(apperror.Unsupported, errSearchUnsupported, "采集接口暂不支持搜索")
	}
	if len(trimmed) > 0 && trimmed[0] != '{' && trimmed[0] != '[' {
		return nil, apperror.New(apperror.Corrupt, "源站返回了非JSON响应")
	}

	var result *FetchResult
	if shape == nil {
		result, err = decodeCMSEnvelope(body, fieldMapping)
	} else {
		result, err = decodeShapedEnvelope(body, shape, fieldMapping)
	}
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "源响应无法解析，请检查响应结构和字段映射")
	}
	if result.OK {
		valid := result.List[:0]
		for i, video := range result.List {
			if video == nil || strings.TrimSpace(video.VodId.String()) == "" || strings.TrimSpace(video.VodName) == "" {
				applog.Warn("[FieldMapping] 第 %d 条记录缺少影片标识或名称", i+1)
				continue
			}
			valid = append(valid, video)
		}
		if len(result.List) > 0 && len(valid) == 0 {
			return nil, apperror.New(apperror.Corrupt, "源响应无法解析：所有记录均缺少影片标识或名称")
		}
		result.List = valid
	}

	result.HTTPStatus = resp.StatusCode
	result.ResponseBytes = len(body)
	fields["api_code"], fields["normalized_count"], fields["total"], fields["page"], fields["page_count"] = result.Code, len(result.List), result.Total.Int(), result.Page.Int(), result.Pagecount.Int()
	fields["raw_count"], fields["decode_dropped_count"] = result.RawCount, result.RawCount-len(result.List)
	if shape != nil {
		fields["api_code"] = result.CodeText
	}
	ids := make([]string, 0, 5)
	for _, video := range result.List {
		if video != nil && len(ids) < 5 {
			ids = append(ids, video.VodId.String())
		}
	}
	fields["sample_ids"] = ids

	if !result.OK {
		return nil, apperror.Wrap(apperror.Unavailable, &APIError{Code: result.Code, CodeText: result.CodeText, Message: result.Msg}, "采集接口返回失败")
	}
	stage = "decoded"
	return result, nil
}

// decodeCMSEnvelope validates the flat MAC-CMS envelope, including numeric
// strings. A missing list is a protocol error rather than a successful empty page.
func decodeCMSEnvelope(body []byte, fieldMapping map[string]string) (*FetchResult, error) {
	result := &FetchResult{}

	var meta struct {
		Code      json.RawMessage `json:"code"`
		List      json.RawMessage `json:"list"`
		Page      FlexInt         `json:"page"`
		Pagecount FlexInt         `json:"pagecount"`
		Limit     FlexInt         `json:"limit"`
		Total     FlexInt         `json:"total"`
		Msg       string          `json:"msg"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "parse CMS metadata")
	}
	var code FlexInt
	if len(meta.Code) > 0 {
		if err := json.Unmarshal(meta.Code, &code); err != nil {
			return nil, apperror.Wrap(apperror.Corrupt, err, "parse CMS response code")
		}
	}
	result.Code = code.Int()
	result.Page = meta.Page
	result.Pagecount = meta.Pagecount
	result.Limit = meta.Limit
	result.Total = meta.Total
	result.Msg = meta.Msg
	result.OK = result.Code == 1
	if !result.OK {
		return result, nil
	}
	if len(meta.List) == 0 {
		return nil, apperror.New(apperror.Corrupt, "采集接口返回成功但缺少 list 字段")
	}
	node, err := decodeJSONDocument(meta.List)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "parse CMS list")
	}
	videos, err := parseVideosFromValue(node, fieldMapping)
	if err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, "parse json with mapping")
	}
	result.List = videos
	if items, err := videoValueItems(node); err == nil {
		result.RawCount = len(items)
	}
	return result, nil
}

func decompress(r io.Reader, encoding string) (io.ReadCloser, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "gzip":
		gzReader, err := gzip.NewReader(r)
		if err != nil {
			return nil, fmt.Errorf("gzip decompress: %w", err)
		}
		return gzReader, nil
	case "deflate":
		buffered := bufio.NewReader(r)
		header, err := buffered.Peek(2)
		if err != nil {
			return nil, fmt.Errorf("deflate header: %w", err)
		}
		if header[0]&0x0f == 8 && (int(header[0])*256+int(header[1]))%31 == 0 {
			return zlib.NewReader(buffered)
		}
		// Older servers emit raw DEFLATE rather than the RFC 1950 wrapper.
		return flate.NewReader(buffered), nil
	case "br":
		return io.NopCloser(brotli.NewReader(r)), nil
	case "", "identity":
		return io.NopCloser(r), nil
	default:
		return nil, fmt.Errorf("unsupported content encoding %q", encoding)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
