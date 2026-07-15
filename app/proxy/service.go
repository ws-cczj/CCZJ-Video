// Package proxy provides constrained remote image fetching for the UI.
package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	minRequestGap = 300 * time.Millisecond
	maxImageBytes = 16 << 20
)

// Service owns proxy networking policy and request pacing.
type Service struct {
	client   *http.Client
	mu       sync.Mutex
	lastCall time.Time
}

// NewService creates an image proxy with conservative transport timeouts.
func NewService() *Service {
	return &Service{client: &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        20,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
	}}
}

// Image fetches a public HTTP(S) image and returns it as a data URL.
func (s *Service) Image(urlStr string) (string, error) {
	return s.ImageContext(context.Background(), urlStr)
}

// ImageContext fetches an image using the caller's cancellation context.
func (s *Service) ImageContext(ctx context.Context, urlStr string) (string, error) {
	if strings.TrimSpace(urlStr) == "" {
		return "", fmt.Errorf("url is empty")
	}
	originalURL := urlStr
	if strings.Contains(urlStr, "s_ratio_poster") {
		urlStr = strings.Replace(urlStr, "s_ratio_poster", "l_ratio_poster", 1)
	}
	result, err := s.withRetry(ctx, urlStr)
	if err != nil && originalURL != urlStr {
		if fallback, fallbackErr := s.withRetry(ctx, originalURL); fallbackErr == nil {
			return fallback, nil
		}
	}
	return result, err
}

func (s *Service) withRetry(ctx context.Context, urlStr string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := waitContext(ctx, 500*time.Millisecond); err != nil {
				return "", err
			}
		}
		if err := s.waitForTurn(ctx); err != nil {
			return "", err
		}
		result, err := s.fetch(ctx, urlStr)
		if err == nil {
			return result, nil
		}
		lastErr = err
		errText := err.Error()
		if !strings.Contains(errText, "deadline") && !strings.Contains(errText, "text/html") && !strings.Contains(errText, "HTTP 4") {
			return "", err
		}
	}
	return "", lastErr
}

func (s *Service) waitForTurn(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if elapsed := time.Since(s.lastCall); elapsed < minRequestGap {
		if err := waitContext(ctx, minRequestGap-elapsed); err != nil {
			return err
		}
	}
	s.lastCall = time.Now()
	return nil
}

func waitContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (s *Service) fetch(ctx context.Context, urlStr string) (string, error) {
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}
	if err := ValidateTarget(ctx, u); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("build request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0")
	req.Header.Set("Accept", "image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "identity")
	if strings.Contains(u.Host, "doubanio.com") || strings.Contains(u.Host, "douban.com") {
		req.Header.Set("Referer", "https://movie.douban.com/")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch image failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("not an image: %s", contentType)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
	if err != nil {
		return "", fmt.Errorf("read image failed: %w", err)
	}
	if len(data) > maxImageBytes {
		return "", fmt.Errorf("image response exceeds %d bytes", maxImageBytes)
	}
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// ValidateTarget rejects loopback, private, link-local and CGNAT targets.
func ValidateTarget(ctx context.Context, u *url.URL) error {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("proxy target host is not allowed")
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !IsPublicAddr(ip) {
			return fmt.Errorf("proxy target address is not public")
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return fmt.Errorf("resolve proxy target: %w", err)
	}
	if len(addresses) == 0 {
		return fmt.Errorf("proxy target has no address")
	}
	for _, address := range addresses {
		if !IsPublicAddr(address) {
			return fmt.Errorf("proxy target resolves to a non-public address")
		}
	}
	return nil
}

// IsPublicAddr reports whether addr is safe for the UI image proxy.
func IsPublicAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	if addr.Is4() {
		ip := addr.As4()
		return !(ip[0] == 100 && ip[1]&0xc0 == 0x40)
	}
	return true
}
