package proxy

import (
	"bytes"
	"cczjVideo/app/netpolicy"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/netbudget"
	"cczjVideo/app/netstats"
	"cczjVideo/app/workflight"
)

const (
	// Defaults for reading images; age is measured since last access.
	readingCacheTTL = 3 * 24 * time.Hour
	// readingCacheMaxBytes 比海报那份（80MB）大：单页体积差一到两个数量级，
	// 80MB 只装得下几十话，翻两下就全被挤出去了。
	readingCacheMaxBytes = 256 << 20
	// maxReadingImageBytes bounds transfer memory, independent of image validity.
	maxReadingImageBytes = 16 << 20
	// maxReadingRefBytes 限制引用页地址的长度。HLS 那边是 1024，
	// 对完整页面地址（含 query）偏紧，这里放宽到 4096。
	maxReadingRefBytes = 4096
	maxReadingRedirect = 5
)

// ImageProxy 是阅读模式的图片代理：漫画页、小说插图、以及任何需要
// **带完整 Referer 与源自定义头**才能取到的图片。
//
// 它不复用 HLS 那条路由（/__cczj/hls），理由不是怕撞车而是职责不同：
// hls.go 要改写播放列表里的分片地址、要对 .m3u8 屏蔽 Range，这些语义对图片全是负担；
// 而图片要的"完整 Referer"恰恰是 HLS 刻意不提供的——RefererOrigin（hls.go:110-116）
// 把引用页截成 scheme://host/，为的是让这个参数不能被用来冒充任意 URL 的引用页。
// 对播放列表那是对的，对漫画图片不行：reports/03:97 要求 source headers 一路保留到
// 实际资源请求，而真实漫画 CDN 校验的往往就是那个完整页面地址（见 ADR 0016）。
//
// 两条路由共用的部分仍然是共用的：SafeDialContext、ValidateTarget、netstats。
type imageRead struct {
	data        []byte
	contentType string
}

type ImageProxy struct {
	reads    *workflight.Group[imageRead]
	client   *http.Client
	store    *imageStore
	sessions imageSessions

	// headersFor 由服务层注入，按 source_key 取出这个源声明的请求头。
	//
	// 头不走 URL 参数：让 WebView 能在查询串里塞任意请求头，等于把"用本机的
	// 网络身份发任意请求"的能力开放给了页面脚本。按 source_key 反查，
	// 能发什么头由用户在加源时配置的内容决定，与海报代理的信任模型一致。
	headersFor func(sourceKey string) map[string]string
	modeFor    func(sourceKey string) string
	foreground func() func()
	observe    func(context.Context, string, string, string) error
}

// NewImageProxy 建一个阅读图片代理。cacheDir 传 nil 表示不落盘（单测用这条）。
func NewImageProxy(cacheDir func() string) *ImageProxy {
	return NewImageProxyWithRuntime(cacheDir, context.Background(), nil)
}

func NewImageProxyWithRuntime(cacheDir func() string, owner context.Context, budget *netbudget.Budget) *ImageProxy {
	p := &ImageProxy{
		reads: workflight.New[imageRead](owner, 128),
		store: newBlobStore(cacheDir, "readingcache", readingCacheTTL, readingCacheMaxBytes),
		client: &http.Client{
			Timeout: 60 * time.Second,
			// 归到 reading 而不是 image：诊断页上"阅读模式用了多少流量"应当是一个桶，
			// 而 image 那一类记的是海报代理的 miss 次数，两者的量级与含义都不同。
			Transport: netpolicy.WrapTransport(string(netstats.CategoryReadingImage), netstats.WrapTransport(netstats.CategoryReadingImage, &http.Transport{
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 8,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
				DialContext:         SafeDialContext,
			})),
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
	if budget != nil {
		p.client.Transport = budget.Wrap("images", p.client.Transport)
	}
	return p
}

func (p *ImageProxy) Close(ctx context.Context) error {
	err := p.reads.Close(ctx)
	p.client.CloseIdleConnections()
	return err
}

func (p *ImageProxy) SetMaintenanceLauncher(launch func(string, func(context.Context)) bool) {
	p.store.maintenance = launch
}

// SetHeaderResolver 注入"按 source_key 取源声明的请求头"的回调。
func (p *ImageProxy) SetHeaderResolver(fn func(sourceKey string) map[string]string) {
	p.headersFor = fn
}

func (p *ImageProxy) SetModeResolver(fn func(string) string) { p.modeFor = fn }

func (p *ImageProxy) SetEvidenceRecorder(fn func(context.Context, string, string, string) error) {
	p.observe = fn
}

// ConfigureReadingRuntime runs only during application assembly, before any
// requests. Page images share the same reading gate and foreground priority.
func (p *ImageProxy) ConfigureReadingRuntime(wrap func(http.RoundTripper) http.RoundTripper, begin func() func()) {
	if wrap != nil {
		p.client.Transport = wrap(p.client.Transport)
	}
	p.foreground = begin
}

// ProbeReadingImage checks an actual image using the production redirect, size,
// network policy and content checks. It does not retain bytes in the cache.
func ProbeReadingImage(ctx context.Context, upstream, referer string, sourceHeaders map[string]string) error {
	return ProbeReadingImageWithTransport(ctx, upstream, referer, sourceHeaders, nil)
}
func ProbeReadingImageWithTransport(ctx context.Context, upstream, referer string, sourceHeaders map[string]string, wrap func(http.RoundTripper) http.RoundTripper) error {
	initial, err := parsePublicHTTPURL(upstream)
	if err != nil {
		return err
	}
	p := NewImageProxy(nil)
	if wrap != nil {
		p.client.Transport = wrap(p.client.Transport)
	}
	defer p.Close(ctx)
	headers := make(http.Header)
	headers.Set("User-Agent", "Mozilla/5.0")
	for key, value := range sourceHeaders {
		if !strings.EqualFold(key, "Referer") && !strings.EqualFold(key, "Accept-Encoding") {
			headers.Set(key, value)
		}
	}
	if referer != "" {
		headers.Set("Referer", referer)
	}
	_, _, err = p.fetch(ctx, initial, headers, p.client)
	return err
}

// URL 生成一个同源的图片代理地址。
//
// referer 必须是**完整的页面地址**，不要先过 RefererOrigin——截成 origin 正是
// 这条路由存在的原因。sourceKey 用来反查这个源声明的请求头。
func (p *ImageProxy) URL(upstream, referer, sourceKey string) string {
	out := "/__cczj/img?u=" + base64.RawURLEncoding.EncodeToString([]byte(upstream))
	if strings.TrimSpace(referer) != "" {
		out += "&ref=" + base64.RawURLEncoding.EncodeToString([]byte(referer))
	}
	if strings.TrimSpace(sourceKey) != "" {
		out += "&k=" + url.QueryEscape(strings.TrimSpace(sourceKey))
	}
	return out
}

// ServeHTTP 由 Wails 的同源 asset 中间件挂载（app.go）。
func (p *ImageProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		setReadingCORS(w, r)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	setReadingCORS(w, r)

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
	referer := decodeReferer(r.URL.Query().Get("ref"))
	sourceKey := r.URL.Query().Get("k")
	// Resolve headers once: the cache key and every redirect use this snapshot.
	request, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstream.String(), nil)
	if err != nil {
		http.Error(w, "invalid upstream URL", http.StatusBadRequest)
		return
	}
	p.applyHeaders(request, referer, sourceKey)
	profile := request.Header.Clone()
	profile.Del("Referer")
	session := p.sessions.get(imageContextKey(sourceKey, profile), p.client)
	cacheKey := imageContextKey(sourceKey, request.Header) + ":" + upstream.String()

	// 命中磁盘副本时不出网。ServeContent 会正确处理 Range 与 HEAD。
	if data, contentType, ok := p.store.read(cacheKey); ok && !session.stateful.Load() {
		if p.observe != nil && sourceKey != "" && referer != "" {
			if err := p.observe(r.Context(), sourceKey, referer, r.URL.Query().Get("rv")); err != nil && r.Context().Err() == nil {
				applog.Warn("[ReadingImage] 保存图片证据失败: %v", err)
			}
		}
		serveCachedImage(w, r, data, contentType)
		return
	}
	if !session.stateful.Load() && !p.store.allow(cacheKey) {
		// 冷却期内直接报 503，让前端显示"这一页取不到"而不是转圈：
		// 一张取不到的图会被每个渲染它的 <img> 反复要一遍。
		http.Error(w, "cooling down", http.StatusServiceUnavailable)
		return
	}

	flightKey := fmt.Sprintf("%s:session-%d", cacheKey, session.revision.Load())
	result, err := p.reads.Do(r.Context(), flightKey, func(ctx context.Context) (imageRead, error) {
		if data, contentType, ok := p.store.read(cacheKey); ok && !session.stateful.Load() {
			return imageRead{data, contentType}, nil
		}
		if p.modeFor != nil {
			ctx = netstats.WithReadingMode(ctx, p.modeFor(sourceKey))
		}
		data, contentType, err := p.fetch(ctx, upstream, request.Header, session.client)
		if err == nil && !session.stateful.Load() {
			p.store.write(cacheKey, data, contentType)
		}
		return imageRead{data, contentType}, err
	})
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			// 用户翻页/切章会取消上一批请求，这是正常路径：**不记失败冷却**
			// （否则翻一次页就把还没看过的那几页全打进 5 分钟冷却，翻回来时
			// 明明能取到却报"取不到"）、不刷警告日志，也不给一个已经没人听的
			// 响应写状态码。
			return
		}
		if !session.stateful.Load() {
			p.store.noteFailure(cacheKey)
		}
		applog.Warn("[ReadingImage] 取图失败 %s: %v", upstream.Host, err)
		http.Error(w, "upstream request failed", http.StatusBadGateway)
		return
	}

	if p.observe != nil && sourceKey != "" && referer != "" {
		if err := p.observe(r.Context(), sourceKey, referer, r.URL.Query().Get("rv")); err != nil && r.Context().Err() == nil {
			applog.Warn("[ReadingImage] 保存图片证据失败: %v", err)
		}
	}
	serveCachedImage(w, r, result.data, result.contentType)
}

// serveCachedImage 用 http.ServeContent 端出图片字节。
//
// 用它而不是直接 Write，是为了让 Range 与 HEAD 由标准库正确处理：
// 手写这两条分支很容易在 206 上忘了带 Content-Range，
// 而一份自相矛盾的响应会让 WebView 的图片解码器直接放弃。
func serveCachedImage(w http.ResponseWriter, r *http.Request, data []byte, contentType string) {
	w.Header().Set("Content-Type", contentType)
	// Browser URLs do not encode configuration/session revisions. Always enter
	// this handler so the server can resolve the current context and disk cache.
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
}

// decodeReferer 解出完整的引用页地址。
//
// 与 HLS 那条路由的差别就在这里：**不截成 origin**。这里只校验它是一个
// http(s) 地址并限长——Referer 是一个纯字符串头，不会被用来发请求，
// 所以它不需要经过 ValidateTarget；真正被请求的地址是 u，那一个照旧严格校验。
func decodeReferer(encoded string) string {
	if encoded == "" || len(encoded) > maxReadingRefBytes {
		return ""
	}
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return ""
	}
	parsed, err := url.Parse(string(decoded))
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return ""
	}
	return parsed.String()
}

// fetch 取一张图，逐跳校验重定向，并在**写缓存之前**确认它真的是图片。
func (p *ImageProxy) fetch(ctx context.Context, initial *url.URL, headers http.Header, client *http.Client) ([]byte, string, error) {
	if p.foreground != nil {
		release := p.foreground()
		defer release()
	}
	current := initial
	for hop := 0; hop <= maxReadingRedirect; hop++ {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
		if err != nil {
			return nil, "", err
		}
		request.Header = headers.Clone()
		response, err := client.Do(request)
		if err != nil {
			return nil, "", err
		}
		if response.StatusCode >= 300 && response.StatusCode <= 399 {
			location, locErr := response.Location()
			response.Body.Close()
			if locErr != nil {
				return nil, "", fmt.Errorf("源站返回 %d 但没有给出可跟随的地址", response.StatusCode)
			}
			// 每一跳都重新校验：一个 302 就能把请求从公网带进内网。
			next, parseErr := parsePublicHTTPURL(location.String())
			if parseErr != nil {
				return nil, "", parseErr
			}
			current = next
			continue
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("源站返回 HTTP %d", response.StatusCode)
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, maxReadingImageBytes+1))
		if err != nil {
			return nil, "", err
		}
		if len(data) > maxReadingImageBytes {
			return nil, "", fmt.Errorf("响应超过图片传输上限 %d 字节", maxReadingImageBytes)
		}
		contentType, err := verifyImageBytes(data, response.Header.Get("Content-Type"))
		if err != nil {
			return nil, "", err
		}
		return data, contentType, nil
	}
	return nil, "", fmt.Errorf("重定向超过 %d 跳", maxReadingRedirect)
}

// verifyImageBytes 在写缓存之前确认拿到的是图片。
//
// 这一条是刻意的、且与两个参照项目都不同：Mihon 的下载/缓存层**不做任何内容类型
// 校验**，HTML 错误页原样写盘然后标 Page.State.Ready，mime 不认识且魔数嗅探失败时
// 还默认当 "jpg"（ChapterCache.kt:157-191、ImageUtil.kt:68-71），于是错误被推迟到
// 解码期才在阅读器里暴露，而那份坏字节已经进了缓存、下次还是它。
// reports/00:146 明确要求"检查响应类型/解码，不缓存错误页为图片"。
//
// 声明的类型与字节识别结果任一方认定是图片就通过：
// 只信声明会被 Content-Type: application/octet-stream 的 CDN 卡住，
// 只信魔数会漏掉 DetectContentType 认不出的格式。但 text/html 一票否决——
// 那就是错误页/登录页，没有例外。
func verifyImageBytes(data []byte, declared string) (string, error) {
	if len(data) == 0 {
		return "", fmt.Errorf("响应是空的")
	}
	detected := http.DetectContentType(data)
	declaredType := strings.TrimSpace(declared)
	if i := strings.IndexByte(declaredType, ';'); i >= 0 {
		declaredType = strings.TrimSpace(declaredType[:i])
	}
	declaredType = strings.ToLower(declaredType)
	detected = strings.ToLower(detected)

	if strings.HasPrefix(declaredType, "text/html") || strings.HasPrefix(detected, "text/html") {
		return "", fmt.Errorf("源站返回的是一个 HTML 页面而不是图片；多半是防盗链拦下了、或者这一页需要登录")
	}
	switch {
	case strings.HasPrefix(declaredType, "image/"):
		return declaredType, nil
	case strings.HasPrefix(detected, "image/"):
		return detected, nil
	case declaredType == "" || declaredType == "application/octet-stream":
		// 没有可用声明时退回魔数结果。DetectContentType 认不出的格式
		// （例如某些 WebP 变体与 SVG）会落到这里被拒——宁可这一页显示不出来
		// 并报清原因，也不要把一段未知字节当图片缓存下来。
		return "", fmt.Errorf("响应类型是 %q，不是图片", detectedOrUnknown(detected))
	default:
		return "", fmt.Errorf("源站返回的类型是 %q，不是图片", declaredType)
	}
}

func detectedOrUnknown(detected string) string {
	if detected == "" {
		return "未知"
	}
	return detected
}

// applyHeaders 把源声明的头加上，再补 Referer 与常规浏览器头。
//
// 顺序是有意的：Referer 与 User-Agent 放在最后写，所以源声明里的同名头会被覆盖。
// Referer 用调用方给的完整页面地址（不是 origin），因为图片请求的引用页
// 就是它所在的那一页；UA 与页面请求保持一致，不少 CDN 会比对两者。
func (p *ImageProxy) applyHeaders(request *http.Request, referer, sourceKey string) {
	request.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	request.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36")
	if p.headersFor != nil && strings.TrimSpace(sourceKey) != "" {
		for name, value := range p.headersFor(sourceKey) {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" || strings.ContainsAny(trimmed, ":\r\n") || strings.ContainsAny(value, "\r\n") {
				continue
			}
			// 这两个头由本函数按当前请求决定，不接受源配置覆盖：
			// Accept-Encoding 一旦被声明，Go 就不再透明解压；
			// Referer 必须是这一页自己的地址，源里写死一个值会让整本漫画都用错引用页。
			if strings.EqualFold(trimmed, "Accept-Encoding") || strings.EqualFold(trimmed, "Referer") {
				continue
			}
			request.Header.Set(trimmed, value)
		}
	}
	if referer != "" {
		request.Header.Set("Referer", referer)
	}
}

func setReadingCORS(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || host == "127.0.0.1" || strings.HasSuffix(host, ".localhost") {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Vary", "Origin")
		w.Header().Set("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Range")
	}
}
