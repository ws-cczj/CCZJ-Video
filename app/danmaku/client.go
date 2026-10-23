package danmaku

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
)

// Client allows only the provider endpoints used by these adapters. Dial and
// concurrency policy are injected by the application, including SSRF checks.
type Client struct{ http *http.Client }

func NewClient(client *http.Client) *Client { return &Client{http: client} }

var allowedHosts = map[string]bool{"api.bilibili.com": true, "www.bilibili.com": true, "pbaccess.video.qq.com": true, "dm.video.qq.com": true, "mesh.if.iqiyi.com": true, "www.iqiyi.com": true, "pcw-api.iqiyi.com": true, "pcw-api.iq.com": true, "cmts.iqiyi.com": true}

func AllowedURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && allowedHosts[u.Hostname()]
}

func (c *Client) read(ctx context.Context, rawURL string, body any) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || !AllowedURL(u) {
		return nil, apperror.New(apperror.Validation, "弹幕接口地址无效")
	}
	var data []byte
	method := http.MethodGet
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return nil, err
		}
		method = http.MethodPost
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept-Encoding", "identity")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	switch {
	case strings.Contains(u.Hostname(), "bilibili"):
		req.Header.Set("Referer", "https://www.bilibili.com/")
	case strings.Contains(u.Hostname(), "qq.com"):
		req.Header.Set("Referer", "https://v.qq.com/")
		req.Header.Set("Origin", "https://v.qq.com")
	default:
		req.Header.Set("Referer", "https://www.iqiyi.com/")
		req.Header.Set("Origin", "https://www.iqiyi.com")
	}
	return c.readHTTP(ctx, req)
}

func (c *Client) readHTTP(ctx context.Context, req *http.Request) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, "弹幕平台暂时不可用")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		code := apperror.Unavailable
		switch resp.StatusCode {
		case 401:
			code = apperror.AuthenticationRequired
		case 403, 412:
			code = apperror.PermissionDenied
		case 404:
			code = apperror.NotFound
		case 429:
			code = apperror.RateLimited
		}
		seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		if seconds == 0 {
			if at, e := http.ParseTime(resp.Header.Get("Retry-After")); e == nil {
				seconds = max(1, int(time.Until(at).Seconds()))
			}
		}
		return nil, &apperror.Error{Code: code, Message: "弹幕平台请求失败", Cause: fmt.Errorf("danmaku upstream HTTP %d host=%s", resp.StatusCode, req.URL.Hostname()), RetryAfterSeconds: min(86400, max(0, seconds))}
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxPayload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxPayload {
		return nil, apperror.New(apperror.Corrupt, "弹幕响应超出大小限制")
	}
	return data, nil
}

func (c *Client) object(ctx context.Context, url string, body any) (map[string]any, error) {
	raw, err := c.read(ctx, url, body)
	if err != nil {
		return nil, err
	}
	return object(raw)
}
func object(raw []byte) (map[string]any, error) {
	var value map[string]any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil || value == nil {
		return nil, apperror.New(apperror.Corrupt, "弹幕平台返回的数据格式已变化")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, apperror.New(apperror.Corrupt, "弹幕平台返回的数据格式已变化")
	}
	return value, nil
}
func text(v any) string {
	switch v := v.(type) {
	case string:
		return v
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	return ""
}
func number(v any) int64 {
	n, _ := strconv.ParseFloat(text(v), 64)
	if math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 1e12 {
		return 0
	}
	return int64(n + .5)
}
func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }
func arr(v any) []any          { a, _ := v.([]any); return a }
func business(value any, success ...string) error {
	code := text(value)
	for _, ok := range success {
		if code == ok {
			return nil
		}
	}
	kind := apperror.Unavailable
	switch code {
	case "-101", "-403":
		kind = apperror.AuthenticationRequired
	case "-352", "-412", "-1":
		kind = apperror.PermissionDenied
	case "-509", "429":
		kind = apperror.RateLimited
	}
	return apperror.Wrap(kind, fmt.Errorf("upstream business code=%s", code), "弹幕平台暂时拒绝了请求")
}
