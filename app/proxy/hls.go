package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const maxPlaylistBytes = 8 << 20

var playlistURIAttribute = regexp.MustCompile(`URI="([^"]+)"`)

// HLSService is a loopback-only, streaming proxy for HLS playlists and media.
// It exists so the WebView never needs cross-origin access to an upstream CDN.
type HLSService struct {
	client *http.Client
}

// NewHLSService creates a handler mounted by Wails' same-origin asset server.
func NewHLSService() *HLSService {
	s := &HLSService{}
	s.client = &http.Client{
		Timeout: 45 * time.Second,
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
		if !IsPublicAddr(address) {
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
	return "/__cczj/hls?u=" + base64.RawURLEncoding.EncodeToString([]byte(raw))
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
	resp, finalURL, err := s.fetch(r.Context(), upstream, r.Header.Get("Range"))
	if err != nil {
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if isPlaylist(finalURL, resp.Header.Get("Content-Type")) && r.Method == http.MethodGet && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		body, err := io.ReadAll(io.LimitReader(resp.Body, maxPlaylistBytes+1))
		if err != nil || len(body) > maxPlaylistBytes {
			http.Error(w, "playlist response too large", http.StatusBadGateway)
			return
		}
		if bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U")) {
			body = []byte(s.rewritePlaylist(string(body), finalURL))
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.Header().Set("Content-Length", fmt.Sprint(len(body)))
			w.WriteHeader(resp.StatusCode)
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

func (s *HLSService) fetch(ctx context.Context, initial *url.URL, rangeHeader string) (*http.Response, *url.URL, error) {
	current := initial
	for redirects := 0; redirects <= 5; redirects++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36")
		req.Header.Set("Accept", "*/*")
		req.Header.Set("Accept-Encoding", "identity")
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

func (s *HLSService) rewritePlaylist(playlist string, base *url.URL) string {
	proxyURI := func(raw string) string {
		u, err := base.Parse(strings.TrimSpace(raw))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			return raw
		}
		return s.URL(u.String())
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
