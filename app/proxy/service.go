// Package proxy provides constrained remote image fetching for the UI.
package proxy

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/netstats"
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
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
	images   *imageStore
}

// NewService creates an image proxy with conservative transport timeouts.
// imageCacheDir 给出海报磁盘副本的根目录；传 nil 表示不落盘（单测用的就是这条）。
func NewService(imageCacheDir func() string) *Service {
	return &Service{images: newImageStore(imageCacheDir), client: &http.Client{
		Timeout: 30 * time.Second,
		// 只统计真的出了网的图片：命中磁盘缓存和前端 imageProxyCache 时根本不会走到这里，
		// 所以这一类的次数是「图片 miss」而不是「图片请求总数」。
		Transport: netstats.WrapTransport(netstats.CategoryImage, &http.Transport{
			MaxIdleConns:        20,
			IdleConnTimeout:     60 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
			DialContext:         safeDialContext,
		}),
		// Redirects must be validated one hop at a time. Let fetch do that
		// explicitly rather than allowing net/http to follow an unchecked URL.
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

// ImageContext fetches an image using the caller's cancellation context.
func (s *Service) ImageContext(ctx context.Context, urlStr string) (string, error) {
	if strings.TrimSpace(urlStr) == "" {
		return "", apperror.New(apperror.Validation, "url is empty")
	}
	originalURL := urlStr
	if strings.Contains(urlStr, "s_ratio_poster") {
		urlStr = strings.Replace(urlStr, "s_ratio_poster", "l_ratio_poster", 1)
	}
	// 请求发出去的是加工过的 URL（高清、可抓的镜像），磁盘副本仍然按前端给的那个 URL 记账：
	// 同一个 URL 拿到的图不会自己变，换封面是换 URL，所以命中即可直接端回去，一次网都不出。
	if data, contentType, ok := s.images.read(originalURL); ok {
		return imageDataURL(contentType, data), nil
	}
	if !s.images.allow(originalURL) {
		return "", apperror.New(apperror.Unavailable, "image fetch is cooling down after a previous failure")
	}
	data, contentType, err := s.fetchPoster(ctx, urlStr, originalURL)
	if err != nil {
		s.images.noteFailure(originalURL)
		return "", err
	}
	s.images.write(originalURL, data, contentType)
	return imageDataURL(contentType, data), nil
}

// fetchPoster 按「最可能一次成功」的顺序试几个 URL 变体，第一个拿到图的即返回。
//
// 顺序要紧是因为这些请求全串在 minRequestGap 这把全局锁上：一张走不通的海报不是慢一点，
// 而是把整条图片队列一起拖住。豆瓣 img9 就是这种（见 doubanPosterMirror），旧代码在它身上
// 要烧掉四次注定失败的请求才落到占位图。
func (s *Service) fetchPoster(ctx context.Context, requestURL, originalURL string) ([]byte, string, error) {
	var lastErr error
	for _, candidate := range posterCandidates(requestURL, originalURL) {
		data, contentType, err := s.withRetry(ctx, candidate)
		if err == nil {
			return data, contentType, nil
		}
		lastErr = err
	}
	return nil, "", lastErr
}

// posterCandidates 列出该按什么顺序试。requestURL 是加工后的（高清），originalURL 是
// 前端给的那个；两者相同（非豆瓣海报）时就只有一个候选，行为跟以前完全一致。
func posterCandidates(requestURL, originalURL string) []string {
	candidates := make([]string, 0, 3)
	if mirror := doubanPosterMirror(requestURL); mirror != "" {
		candidates = append(candidates, mirror)
	}
	candidates = append(candidates, requestURL)
	if originalURL != requestURL {
		candidates = append(candidates, originalURL)
	}
	return candidates
}

// doubanPosterMirror 把豆瓣海报里唯一抓不通的那台图片主机换成同路径的镜像主机；
// 不是 img9.doubanio.com 下的 /view/photo/ 就返回空串，调用方保持原有行为。
//
// img9 对本机的 Go HTTP 客户端固定回 200 + text/html，body 是一段写着 EO_Bot_Ssid 的
// JS 挑战页——按 TLS 指纹判定，不是限流也不是瞬时抖动，重试同一台永远还是那张挑战页
// （curl 带同样的头却能拿到图，差别就在指纹）。同一句路径在 img1/img2/img3 上返回的是
// 逐字节相同的 JPEG，所以换主机是拿回原图，不是拿替代品。换掉之后仍然把 img9 留在候选里：
// 豆瓣哪天把图收回 img9，多花的是一次请求而不是坏图。
func doubanPosterMirror(candidate string) string {
	parsed, err := url.Parse(candidate)
	if err != nil {
		return ""
	}
	if !strings.EqualFold(parsed.Host, "img9.doubanio.com") || !strings.HasPrefix(parsed.Path, "/view/photo/") {
		return ""
	}
	parsed.Host = "img1.doubanio.com"
	return parsed.String()
}

func imageDataURL(contentType string, data []byte) string {
	return "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func (s *Service) withRetry(ctx context.Context, urlStr string) ([]byte, string, error) {
	var lastErr error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			if err := waitContext(ctx, 500*time.Millisecond); err != nil {
				return nil, "", err
			}
		}
		if err := s.waitForTurn(ctx); err != nil {
			return nil, "", err
		}
		data, contentType, err := s.fetch(ctx, urlStr)
		if err == nil {
			return data, contentType, nil
		}
		lastErr = err
		if !shouldRetryFetch(err) {
			return nil, "", err
		}
	}
	return nil, "", lastErr
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

func (s *Service) fetch(ctx context.Context, urlStr string) ([]byte, string, error) {
	current, err := url.Parse(urlStr)
	if err != nil {
		return nil, "", apperror.Wrap(apperror.Validation, err, "invalid url")
	}
	for redirects := 0; redirects <= 5; redirects++ {
		if current.Scheme != "http" && current.Scheme != "https" {
			return nil, "", apperror.Newf(apperror.Unsupported, "unsupported scheme: %s", current.Scheme)
		}
		if err := ValidateTarget(ctx, current); err != nil {
			return nil, "", err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, "", apperror.Wrap(apperror.Internal, err, "build request failed")
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0")
		req.Header.Set("Accept", "image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
		req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
		req.Header.Set("Accept-Encoding", "identity")
		if strings.Contains(current.Host, "doubanio.com") || strings.Contains(current.Host, "douban.com") {
			req.Header.Set("Referer", "https://movie.douban.com/")
		}

		resp, err := s.client.Do(req)
		if err != nil {
			// 底层形状（*url.Error / ctx 到期）留在 Cause 里，重试判据才不用读文案。
			return nil, "", apperror.Wrap(apperror.Unavailable, err, "fetch image failed")
		}
		if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
			location, locationErr := resp.Location()
			resp.Body.Close()
			if locationErr != nil {
				return nil, "", apperror.Wrap(apperror.Unavailable, locationErr, "read redirect location")
			}
			next, parseErr := current.Parse(location.String())
			if parseErr != nil {
				return nil, "", apperror.Wrap(apperror.Unavailable, parseErr, "parse redirect location")
			}
			current = next
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, "", apperror.Wrap(apperror.Unavailable, &statusError{statusCode: resp.StatusCode}, "")
		}
		contentType := resp.Header.Get("Content-Type")
		if !strings.HasPrefix(contentType, "image/") {
			_, _ = io.Copy(io.Discard, resp.Body)
			return nil, "", apperror.Wrap(apperror.Unsupported, &notImageError{contentType: contentType}, "")
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes+1))
		if err != nil {
			return nil, "", apperror.Wrap(apperror.Unavailable, err, "read image failed")
		}
		if len(data) > maxImageBytes {
			return nil, "", apperror.Newf(apperror.Unsupported, "image response exceeds %d bytes", maxImageBytes)
		}
		return data, contentType, nil
	}
	return nil, "", apperror.New(apperror.Unavailable, "too many redirects")
}

// allowPrivateTargets 是「放行私网」开关，默认关闭。
//
// 关掉时代理只能访问公网：这是一台桌面应用里的开放代理（前端给什么 URL 就取什么），
// 不挡住回环/私网就等于让本机任何能访问这个 loopback 端口的进程借它去够内网服务。
// 打开它又是真实需求：片源放在 NAS、局域网自建的缓存代理、本地起的 m3u8 调试服务，
// 地址全在私网段内，一律会被默认策略拒掉。
//
// 所以这必须是一个由用户显式打开、且默认关闭的开关，不能在「探测到播放失败」时自动放开。
var allowPrivateTargets atomic.Bool

// SetAllowPrivateTargets 由设置层在启动时和每次改动时调用。图片代理和 HLS 代理共用
// 这一份判定：两者的目标地址来自同一处前端可写的 URL，放开一半只会让行为更难解释。
func SetAllowPrivateTargets(allow bool) { allowPrivateTargets.Store(allow) }

// AllowPrivateTargets 暴露当前取值，供诊断页回显「实际生效的策略」。
func AllowPrivateTargets() bool { return allowPrivateTargets.Load() }

// ValidateTarget rejects loopback, private, link-local and CGNAT targets.
func ValidateTarget(ctx context.Context, u *url.URL) error {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return targetNotAllowed(nil)
	}
	if allowPrivateTargets.Load() {
		// 地址范围不再是拒绝理由，但 scheme 仍由调用方（parsePublicHTTPURL /
		// Service.fetch）限死在 http/https，所以放开私网不会顺带打开 file://。
		return nil
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return targetNotAllowed(u)
	}
	if ip, err := netip.ParseAddr(host); err == nil {
		if !IsPublicAddr(ip) {
			return apperror.Newf(apperror.Validation, "proxy target address is not public: %s", host)
		}
		return nil
	}
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return apperror.Wrap(apperror.Unavailable, err, "resolve proxy target")
	}
	if len(addresses) == 0 {
		return apperror.Newf(apperror.NotFound, "proxy target has no address: %s", host)
	}
	for _, address := range addresses {
		if !IsPublicAddr(address) {
			return apperror.Newf(apperror.Validation, "proxy target resolves to a non-public address: %s (%s)", host, address)
		}
	}
	return nil
}

// targetNotAllowed 说清被的是哪条策略：这句会进日志和界面，光一句 "not allowed"
// 排不出是自己填了个内网地址还是被默认策略拦下。
func targetNotAllowed(u *url.URL) error {
	if u == nil {
		return apperror.New(apperror.Validation, "proxy target host is not allowed")
	}
	return apperror.Newf(apperror.Validation, "proxy target host is not allowed: %s", u.Host)
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
