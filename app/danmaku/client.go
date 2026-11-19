package danmaku

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

var allowedHosts = func() map[string]bool {
	hosts := make(map[string]bool)
	for _, source := range builtinProviders {
		for _, host := range source.hosts {
			hosts[host] = true
		}
	}
	return hosts
}()

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
	case u.Hostname() == mangoSearchHost:
		req.Header.Set("User-Agent", mangoSearchAgent)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Referer", mangoWeb)
	case u.Hostname() == mangoCatalogHost || u.Hostname() == mangoBarrageHost || isMangoCDN(u.Hostname()):
		req.Header.Set("Referer", mangoWeb)
		req.Header.Set("Accept", "application/json")
	case u.Hostname() == miguSearchHost || u.Hostname() == miguCatalogHost || u.Hostname() == miguBarrageHost:
		req.Header.Set("Referer", miguWeb+"/")
		req.Header.Set("Accept", "application/json")
		if u.Hostname() == miguSearchHost {
			req.Header.Set("Origin", miguWeb)
			req.Header.Set("appId", "miguvideo")
			req.Header.Set("terminalId", "www")
		}
		if u.Hostname() == miguBarrageHost {
			req.Header.Set("appCode", "miguvideo_default_h5")
		}
	case u.Hostname() == youkuSearchHost || u.Hostname() == youkuCatalogHost:
		req.Header.Set("Referer", "https://www.youku.com/")
		req.Header.Set("User-Agent", youkuAgent)
		req.Header.Set("Accept", "application/json")
	case u.Hostname() == sohuSearchHost || u.Hostname() == sohuCatalogHost || u.Hostname() == sohuBarrageHost:
		req.Header.Set("Referer", sohuWeb+"/")
		req.Header.Set("Origin", sohuWeb)
		req.Header.Set("Accept", "application/json")
	case u.Hostname() == leshiSearchHost || u.Hostname() == leshiCatalogHost || u.Hostname() == leshiVideoHost || u.Hostname() == leshiBarrageHost:
		req.Header.Set("Referer", "https://www.le.com/")
		if u.Hostname() == leshiSearchHost || u.Hostname() == leshiVideoHost {
			req.Header.Del("Accept-Encoding")
			req.Header.Set("Accept", "text/html")
		}
	case u.Hostname() == xiguaWebHost || u.Hostname() == xiguaBarrageHost:
		req.Header.Set("Referer", "https://"+xiguaWebHost+"/")
		req.Header.Set("User-Agent", xiguaAgent)
		req.Header.Del("Accept-Encoding")
	case u.Hostname() == bahamutAPIHost:
		req.Header.Set("User-Agent", bahamutAgent)
		req.Header.Set("Accept", "application/json")
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
	return c.readHTTPWithCookies(ctx, req, nil)
}

func (c *Client) readHTTPWithCookies(ctx context.Context, req *http.Request, capture func([]*http.Cookie)) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := c.http.Do(req.WithContext(ctx))
	if err != nil {
		var requestError *url.Error
		if req.URL.Hostname() == youkuMTOPHost && errors.As(err, &requestError) {
			err = requestError.Err
		}
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, apperror.Wrap(apperror.Timeout, err, "弹幕平台请求超时")
		}
		if errors.Is(err, context.Canceled) {
			return nil, err
		}
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
	if capture != nil {
		capture(resp.Cookies())
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
