package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxPlaylistBytes = 8 << 20

// maxRefBytes 限制调用方塞进来的引用页长度。超长直接忽略，退回按上游 URL 自己推导。
const maxRefBytes = 1024

var playlistURIAttribute = regexp.MustCompile(`URI="([^"]+)"`)

// HLSService is a loopback-only, streaming proxy for HLS playlists and media.
// It exists so the WebView never needs cross-origin access to an upstream CDN.
type HLSService struct {
	client *http.Client
}

// NewHLSService creates a handler mounted by Wails' same-origin asset server.
func NewHLSService() *HLSService {
	// jar 让「发片段的第二跳」带上第一跳拿到的 Set-Cookie：不少 CDN 先在同一批
	// 请求里下个会话 cookie，再在后续片段上校验它。cookiejar 按标准域名作用域
	// 存取消协，不会把 A 站的 cookie 发给 B 站；而响应侧永远不把上游的 Set-Cookie
	// 转回 WebView（见 copyStreamHeaders 的白名单），避免上游把 cookie 写进本地。
	jar, _ := cookiejar.New(nil)
	s := &HLSService{}
	s.client = &http.Client{
		Timeout: 45 * time.Second,
		Jar:     jar,
		Transport: &http.Transport{
			MaxIdleConns:        64,
			MaxIdleConnsPerHost: 12,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
			DialContext:         safeDialContext,
		},
		CheckRedirect: func(req *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return s
}

// safeDialContext validates the address at the moment it is dialled. This
// closes the DNS-rebinding gap between ValidateTarget and Transport's lookup.
// It is shared by both HLS and image proxy transports.
func safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve proxy upstream: %w", err)
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("proxy upstream has no address")
	}
	for _, address := range addresses {
		if !allowPrivateTargets.Load() && !IsPublicAddr(address) {
			return nil, fmt.Errorf("proxy upstream resolved to a non-public address")
		}
	}
	dialer := &net.Dialer{Timeout: 20 * time.Second, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, address := range addresses {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(address.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// URL returns a stable same-origin proxy URL for an upstream HLS resource.
func (s *HLSService) URL(raw string) string {
	return s.urlFor(raw, "")
}

// urlFor 把引用页一起编进代理地址：改写播放列表时，列表里每个子资源都要知道
// 「是谁把我拉下来的」，否则重写成裸 URL 后只能拿子资源自己的域当 Referer，
// 而鉴权看的通常是父播放列表的域。
func (s *HLSService) urlFor(raw, referer string) string {
	out := "/__cczj/hls?u=" + base64.RawURLEncoding.EncodeToString([]byte(raw))
	if referer != "" {
		out += "&ref=" + base64.RawURLEncoding.EncodeToString([]byte(referer))
	}
	return out
}

// RefererOrigin 只保留 scheme://host/。路径、query、任何 userinfo 一律丢掉：
// 域正是 CDN 引用页校验要看的部分，而截断成 origin 让这个参数不能被用来
// 冒充任意 URL 的引用页。
func RefererOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/"
}

// ServeHTTP serves playlists and segments through the Wails asset middleware.
func (s *HLSService) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		setHLSCORS(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	setHLSCORS(w, r)
	raw, err := base64.RawURLEncoding.DecodeString(r.URL.Query().Get("u"))
	if err != nil || len(raw) == 0 || len(raw) > 8192 {
		http.Error(w, "invalid upstream URL", http.StatusBadRequest)
		return
	}
	upstream, err := parsePublicHTTPURL(string(raw))
	if err != nil {
		http.Error(w, "invalid upstream URL", http.StatusBadRequest)
		return
	}
	referer := ""
	if enc := r.URL.Query().Get("ref"); enc != "" && len(enc) <= maxRefBytes {
		if decoded, decErr := base64.RawURLEncoding.DecodeString(enc); decErr == nil {
			referer = RefererOrigin(string(decoded))
		}
	}
	if referer == "" {
		referer = RefererOrigin(upstream.String())
	}
	resp, finalURL, err := s.fetch(r.Context(), upstream, segmentRange(r, upstream), referer)
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	rewrites := isPlaylist(finalURL, resp.Header.Get("Content-Type")) && r.Method == http.MethodGet &&
		resp.StatusCode >= 200 && resp.StatusCode < 300
	if rewrites && resp.StatusCode != http.StatusOK {
		// 上游按 Range 回了半张播放列表。重写必须基于完整列表，否则会发布一张
		// 尾部集数被截掉的「合法」列表，用户看到的是能放但少几集——比直接报错更难查。
		// 不带 Range 重取一次；再拿不到 200 就放弃重写，走下面的原样转发。
		resp.Body.Close()
		resp, finalURL, err = s.fetch(r.Context(), upstream, "", referer)
		if err != nil {
			http.Error(w, "upstream request failed", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		rewrites = resp.StatusCode == http.StatusOK &&
			isPlaylist(finalURL, resp.Header.Get("Content-Type"))
	}
	if rewrites {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaylistBytes+1))
		if err != nil || len(body) > maxPlaylistBytes {
			http.Error(w, "playlist response too large", http.StatusBadGateway)
			return
		}
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U")) {
			body = []byte(s.rewritePlaylist(string(body), finalURL, referer))
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			// 重写过就一定是完整体，状态必须回到 200：留着 206 而不带
			// Content-Range，是一份自相矛盾的响应。
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(body)
			return
		}
	}
	copyStreamHeaders(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	if r.Method != http.MethodHead {
		_, _ = io.Copy(w, resp.Body)
	}
}

// segmentRange 取出客户端要的字节区间。播放列表刻意忽略它：一张列表动辄几十 KB，
// 分段取只会换来「截断的列表」，没有收益。
func segmentRange(r *http.Request, upstream *url.URL) string {
	if strings.HasSuffix(strings.ToLower(upstream.Path), ".m3u8") {
		return ""
	}
	return r.Header.Get("Range")
}

func (s *HLSService) fetch(ctx context.Context, initial *url.URL, rangeHeader, referer string) (*http.Response, *url.URL, error) {
	current := initial
	for redirects := 0; redirects <= 5; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Encoding", "identity")
		if referer != "" {
			req.Header.Set("Referer", referer)
		}
		if rangeHeader != "" {
			req.Header.Set("Range", rangeHeader)
		}
		resp, err := s.client.Do(req)
		if err != nil {
			return nil, nil, err
		}
		if resp.StatusCode < 300 || resp.StatusCode > 399 {
			return resp, current, nil
		}
		location, err := resp.Location()
		resp.Body.Close()
		if err != nil {
			return nil, nil, err
		}
		next, err := parsePublicHTTPURL(location.String())
		if err != nil {
			return nil, nil, err
		}
		current = next
	}
	return nil, nil, fmt.Errorf("too many redirects")
}

func (s *HLSService) rewritePlaylist(playlist string, base *url.URL, referer string) string {
	proxyURI := func(raw string) string {
		u, err := base.Parse(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return raw
		}
		return s.urlFor(u.String(), referer)
	}
	lines := strings.Split(playlist, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = playlistURIAttribute.ReplaceAllStringFunc(line, func(match string) string {
				parts := playlistURIAttribute.FindStringSubmatch(match)
				return `URI="` + proxyURI(parts[1]) + `"`
			})
			continue
		}
		lines[i] = proxyURI(trimmed)
	}
	return strings.Join(lines, "\n")
}

func parsePublicHTTPURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme")
	}
	if err := ValidateTarget(context.Background(), u); err != nil {
		return nil, err
	}
	return u, nil
}

func isPlaylist(u *url.URL, contentType string) bool {
	path := strings.ToLower(u.Path)
	return strings.HasSuffix(path, ".m3u8") || strings.Contains(strings.ToLower(contentType), "mpegurl")
}

func setHLSCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	u, err := url.Parse(origin)
	if err != nil {
		return
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || host == "127.0.0.1" || strings.HasSuffix(host, ".localhost") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Range")
	}
}

func copyStreamHeaders(dst, src http.Header) {
	for _, key := range []string{"Content-Type", "Content-Length", "Content-Range", "Accept-Ranges", "Cache-Control", "ETag", "Last-Modified"} {
		if value := src.Get(key); value != "" {
			dst.Set(key, value)
		}
	}
}
