package provider

import (
	"cczjVideo/app/netbudget"
	"cczjVideo/app/netpolicy"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/netstats"
	"cczjVideo/app/proxy"
)

// 请求侧的参数。文本响应（搜索/详情/目录/正文）都走这里，图片字节不走——
// 图片由 /__cczj/img 代理路由直接流给 WebView，不进这一层（ADR 0016）。
const (
	fetchTimeout     = 30 * time.Second
	maxFetchBytes    = 8 << 20
	maxFetchRedirect = 5
	defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0 Safari/537.36"
)

// Target 描述一次请求打到哪。
//
// URL 非空时以它为基准：详情、目录、正文、漫画图片列表这四步的地址都是
// **上一步从响应里取出来的**，不是 api_url 拼出来的。Legado 与 Mihon 都是这个形状
// （Book.tocUrl、Chapter.url、Page.url），reports/03:97 还专门强调图片是两步解析。
// URL 为空时用 api_url + Operation.Path 拼，这是搜索与浏览那一步。
//
// 两种情况下 Params 与 ParamNames 都照样生效：一个已经取到的目录地址
// 仍然可能需要追加 page 或 limit 参数。
type Target struct {
	URL  string
	Vars map[string]string
	// PreserveQuery follows an upstream continuation URL exactly. Its query may
	// contain an opaque cursor/signature and must not be rebuilt as a page query.
	PreserveQuery bool
}

// Response 是一次请求的结果。
type Response struct {
	StatusCode  int
	ContentType string
	// FinalURL 是重定向之后的最终地址。解析相对地址要以它为基准，
	// 不是以请求前的地址：规则里取出的相对链接是相对**它所在的那一页**的，
	// 而那一页可能就是重定向后的地址（Legado 两个基准并存，
	// AnalyzeRule.kt:105-115,319-325）。
	FinalURL string
	// Text 是按编码解码后的文本。
	Text string
	// Garbled 报告解码结果像是编码错了（大量 U+FFFD）。它只用于诊断提示，
	// 不用来自动改判编码——猜错的代价比不猜更高。
	Garbled bool
}

// Fetcher 执行源请求。
type Fetcher struct {
	client *http.Client
	mu     sync.Mutex
	scopes map[string]*Fetcher
	order  []string
}

// NewFetcher 建一个出网执行器。
//
// 四件事是刻意的：
//
//   - **cookiejar**：不少源在搜索那一跳下发会话 cookie，正文那一跳要校验它。
//     cookiejar 按标准域名作用域存取，不会把 A 站的 cookie 发给 B 站。
//   - **不设 Accept-Encoding**：Go 的 Transport 会自己加 gzip 并透明解压。
//     源文档里声明的这个头被忽略，因为手动解压意味着尺寸上限必须作用在
//     **解压后**的流上（否则一个压缩炸弹能撑爆内存），collect 那边已经为此
//     付过一遍复杂度（fetcher.go:355-368），不值得为一个几乎不影响成败的头再付一次。
//   - **SafeDialContext**：地址校验发生在拨号那一刻，堵住 ValidateTarget 与 DNS
//     解析之间的重绑定窗口。与 HLS/图片代理共用同一个实现。
//   - **不自己记 netstats**：Transport 已经被 WrapTransport 包了，它在 RoundTrip
//     里记次数与失败、在 countedBody.Close 里记实际读走的字节。这里再记一次
//     会让诊断页的读取类流量翻倍。
func NewFetcher() *Fetcher {
	jar, _ := cookiejar.New(nil)
	return &Fetcher{
		client: &http.Client{
			Timeout: fetchTimeout,
			Jar:     jar,
			Transport: netpolicy.WrapTransport(string(netstats.CategoryReading), netstats.WrapTransport(netstats.CategoryReading, &http.Transport{
				MaxIdleConns:        32,
				MaxIdleConnsPerHost: 6,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
				DialContext:         proxy.SafeDialContext,
			})),
			// 手动跟随重定向：每一跳都要重新过一遍地址校验，
			// 否则一个 302 就能把请求从公网地址带到内网。
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// WrapTransport configures a newly owned fetcher before any source scopes exist.
// Batch diagnostics uses this seam to share one five-slot HTTP gate with images.
func (f *Fetcher) WrapTransport(wrap func(http.RoundTripper) http.RoundTripper) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if wrap == nil || len(f.scopes) > 0 {
		return fmt.Errorf("transport must be configured before source scopes")
	}
	f.client.Transport = wrap(f.client.Transport)
	return nil
}

// ConfigureBudget must run before requests start; scoped sessions share this transport.
func (f *Fetcher) ConfigureBudget(budget *netbudget.Budget) {
	if budget != nil {
		f.client.Transport = budget.Wrap("reading", f.client.Transport)
	}
}
func (f *Fetcher) Close() { f.client.CloseIdleConnections() }

// Scope isolates cookies by source identity and configuration while sharing the
// transport. Bounded FIFO retention prevents abandoned configurations accumulating.
func (f *Fetcher) Scope(key string) *Fetcher { return f.ScopeMode(key, "") }

func (f *Fetcher) ScopeMode(key, mode string) *Fetcher {
	f.mu.Lock()
	defer f.mu.Unlock()
	if current := f.scopes[key]; current != nil {
		return current
	}
	if f.scopes == nil {
		f.scopes = make(map[string]*Fetcher)
	}
	if len(f.order) >= 128 {
		delete(f.scopes, f.order[0])
		f.order = f.order[1:]
	}
	client := *f.client
	client.Transport = &sourceScopeTransport{key: key, base: netstats.ForReadingMode(mode, client.Transport)}
	client.Jar, _ = cookiejar.New(nil)
	scoped := &Fetcher{client: &client}
	f.scopes[key] = scoped
	f.order = append(f.order, key)
	return scoped
}

type sourceScopeTransport struct {
	key  string
	base http.RoundTripper
}

func (t *sourceScopeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req.WithContext(netpolicy.WithScope(req.Context(), t.key)))
}
func (t *sourceScopeTransport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// BuildURL 拼出一次请求的完整地址。
//
// 占位符写作 {name}，从 Target.Vars 取值。Path 里的占位符做路径转义，
// Params 里的值交给 url.Values 做 query 转义——两种转义规则不同，
// 混用会让带斜杠的远端 ID 在一种位置能用、在另一种位置把路径切坏。
func BuildURL(apiURL string, op *Operation, target Target) (string, error) {
	return buildURL(context.Background(), apiURL, op, target)
}
func buildURL(ctx context.Context, apiURL string, op *Operation, target Target) (string, error) {
	vars := target.Vars
	if vars == nil {
		vars = map[string]string{}
	}

	var base string
	if trimmed := strings.TrimSpace(target.URL); trimmed != "" {
		base = trimmed
	} else {
		root := strings.TrimSpace(apiURL)
		if root == "" {
			return "", apperror.New(apperror.Validation, "源没有配置接口地址")
		}
		path := strings.TrimSpace(op.Path)
		if path == "" {
			base = root
		} else {
			// 占位符必须在 url.Parse **之前**展开：解析会把 { } 转义成 %7B %7D，
			// 之后就再也认不出这是一个没取到值的变量了。
			path = expandTemplate(path, vars, false)
			if err := requireExpanded(path); err != nil {
				return "", err
			}
			// Path 可以是绝对地址（源把搜索放在另一个域上），也可以是相对路径。
			rootURL, err := url.Parse(root)
			if err != nil {
				return "", apperror.Wrap(apperror.Validation, err, "源接口地址不是合法 URL")
			}
			resolved, err := rootURL.Parse(path)
			if err != nil {
				return "", apperror.Wrap(apperror.Validation, err,
					fmt.Sprintf("操作路径 %q 无法与源接口地址合并", path))
			}
			base = resolved.String()
		}
	}
	if err := requireExpanded(base); err != nil {
		return "", err
	}

	parsed, err := url.Parse(base)
	if err != nil {
		return "", apperror.Wrap(apperror.Validation, err, "请求地址不是合法 URL")
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", apperror.Newf(apperror.Validation, "请求地址 %q 缺少协议或主机", base)
	}
	if target.PreserveQuery && strings.TrimSpace(target.URL) != "" {
		if err := validatePublicURLContext(ctx, parsed); err != nil {
			return "", err
		}
		return parsed.String(), nil
	}

	query := parsed.Query()
	// 先把这一步要控制的参数名清掉：上一步遗留在 base 里的 page/wd/ids
	// 不能活到这一步来。collect.buildOperationURL 出于同样的理由做过这件事
	// （strategy.go:193-207）。
	for _, key := range []string{
		op.ParamNames.keywordParam(), op.ParamNames.pageParam(),
		op.ParamNames.idParam(), op.ParamNames.limitParam(),
	} {
		query.Del(key)
	}
	for key, value := range op.Params {
		if strings.TrimSpace(key) == "" {
			continue
		}
		query.Set(key, expandTemplate(value, vars, false))
	}
	// ParamNames 声明的是"某个变量在这个源的 query 里叫什么名字"。
	// 只有存在且非空时才写进 query：一个没有关键词的浏览请求不该带上空的 wd=，
	// 有些源会把它当成"搜索空字符串"而返回全库。
	setVar := func(name, value string) {
		if strings.TrimSpace(name) == "" || strings.TrimSpace(value) == "" {
			return
		}
		query.Set(name, value)
	}
	setVar(op.ParamNames.keywordParam(), vars[VarKeyword])
	setVar(op.ParamNames.idParam(), vars[VarID])
	setVar(op.ParamNames.pageParam(), vars[VarPage])
	setVar(op.ParamNames.limitParam(), vars[VarLimit])
	parsed.RawQuery = query.Encode()

	if err := validatePublicURLContext(ctx, parsed); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

// 变量名。Target.Vars 的键只用这几个加远端取出的原始字段，
// 拼错的变量名会被 BuildURL 静默忽略，所以它们集中定义在这里。
const (
	VarKeyword = "keyword"
	VarPage    = "page"
	VarID      = "id"
	VarLimit   = "limit"
	// VarChapterURL 与 VarPageURL 是漫画两步解析里的中间地址，
	// 供 Path 模板引用（有些源的图片地址是章节页地址加一个后缀）。
	VarChapterURL = "chapter_url"
	VarPageURL    = "page_url"
)

// expandTemplate 把 {name} 换成 Vars 里的值。
//
// escape 为真时做路径转义。**未定义的占位符原样保留**，由 requireExpanded 在
// 拼完地址之后统一报错：一个空的 id 参数拼出来的地址会打到源站的列表页，
// 返回一堆无关结果，那比一个明显坏掉的地址难查得多；而留着 {id} 让它被
// url.Parse 转义成 %7Bid%7D 更糟——请求会发出去，然后 404，看不出是配置漏了变量。
func expandTemplate(text string, vars map[string]string, escape bool) string {
	if text == "" || !strings.Contains(text, "{") {
		return text
	}
	var builder strings.Builder
	for i := 0; i < len(text); {
		if text[i] != '{' {
			builder.WriteByte(text[i])
			i++
			continue
		}
		end := strings.IndexByte(text[i:], '}')
		if end < 0 {
			builder.WriteString(text[i:])
			break
		}
		name := strings.TrimSpace(text[i+1 : i+end])
		value, ok := vars[name]
		if !ok {
			builder.WriteString(text[i : i+end+1])
		} else if escape {
			builder.WriteString(url.PathEscape(value))
		} else {
			builder.WriteString(value)
		}
		i += end + 1
	}
	return builder.String()
}

// unexpandedPlaceholder 找出地址里残留的 {name}。
var unexpandedPlaceholder = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// requireExpanded 在地址拼好之后检查还有没有没被替换掉的占位符。
//
// 这一步必须在 url.Parse 之前做：解析会把 `{` `}` 转义成 %7B %7D，
// 之后就再也认不出这是一个漏掉的变量了。
func requireExpanded(built string) error {
	match := unexpandedPlaceholder.FindStringSubmatch(built)
	if match == nil {
		return nil
	}
	return apperror.Newf(apperror.Validation,
		"地址里还有一个没有取到值的变量 {%s}；调用这一步时没有传它", match[1])
}

// Fetch 执行一次请求并解码成文本。
func (f *Fetcher) Fetch(ctx context.Context, doc *Document, op *Operation, apiURL string, target Target) (*Response, error) {
	return f.fetchLimited(ctx, doc, op, apiURL, target, maxFetchBytes)
}

// FetchSourceList accommodates published source collections (~21 MiB) while
// preserving the smaller content-page limit and the same request guardrails.
func (f *Fetcher) FetchSourceList(ctx context.Context, address string) (*Response, error) {
	// Published collections are about 21 MiB; the content-page timeout caused
	// valid libraries to time out mid-download. Keep a separate bounded deadline
	// without changing the shared client or ordinary reading requests.
	client := *f.client
	client.Timeout = 90 * time.Second
	reader := &Fetcher{client: &client}
	return reader.fetchLimited(ctx, &Document{}, &Operation{Format: "json"}, address, Target{}, 32<<20)
}

func (f *Fetcher) fetchLimited(ctx context.Context, doc *Document, op *Operation, apiURL string, target Target, maxBytes int64) (*Response, error) {
	requestURL, err := buildURL(ctx, apiURL, op, target)
	if err != nil {
		return nil, err
	}
	method := op.normalizedMethod()
	var body io.Reader
	if method == http.MethodPost && strings.TrimSpace(op.Body) != "" {
		body = strings.NewReader(expandTemplate(op.Body, target.Vars, false))
	}

	current := requestURL
	for hop := 0; hop <= maxFetchRedirect; hop++ {
		request, err := http.NewRequestWithContext(ctx, method, current, body)
		if err != nil {
			return nil, apperror.Wrap(apperror.Unavailable, err, "构造请求失败")
		}
		applyHeaders(request, doc, op)
		// 只有第一跳带正文：302 之后按 RFC 应当退化成 GET，
		// 继续带着 POST 正文重放会被不少源站拒掉。
		body = nil
		if method == http.MethodPost && hop > 0 {
			method = http.MethodGet
		}

		response, err := f.client.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return nil, apperror.Wrap(apperror.Cancelled, err, "请求已取消")
			}
			return nil, classifyTransportError(err)
		}
		if response.StatusCode >= 300 && response.StatusCode <= 399 {
			// response.Location() 已经把相对地址按当前请求解析好了。
			next, locErr := response.Location()
			response.Body.Close()
			if locErr != nil {
				return nil, apperror.Wrap(apperror.Corrupt, locErr,
					fmt.Sprintf("源站返回 %d 但没有给出可跟随的地址", response.StatusCode))
			}
			// 每一跳都重新校验：一个 302 就能把请求从公网地址带进内网，
			// 只在最初那一跳校验等于没校验。
			if err := validatePublicURLContext(ctx, next); err != nil {
				return nil, err
			}
			current = next.String()
			continue
		}

		defer response.Body.Close()
		switch response.StatusCode {
		case http.StatusTooManyRequests:
			return nil, &apperror.Error{Code: apperror.RateLimited, Message: "源站请求过于频繁，请稍后重试",
				RetryAfterSeconds: retryAfterSeconds(response.Header.Get("Retry-After"), time.Now())}
		case http.StatusUnauthorized:
			return nil, apperror.New(apperror.AuthenticationRequired, "源站要求登录，请检查来源配置")
		case http.StatusForbidden:
			return nil, apperror.New(apperror.PermissionDenied, "源站拒绝访问，请检查来源配置或验证要求")
		case http.StatusNotFound:
			return nil, apperror.New(apperror.NotFound, "源站没有这个地址（HTTP 404）")
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, apperror.Newf(apperror.Unavailable, "源站返回 HTTP %d", response.StatusCode)
		}

		raw, err := io.ReadAll(io.LimitReader(response.Body, maxBytes+1))
		if err != nil {
			return nil, apperror.Wrap(apperror.Unavailable, err, "读取响应失败")
		}
		if int64(len(raw)) > maxBytes {
			return nil, apperror.Newf(apperror.Corrupt,
				"响应超过 %d 字节上限", maxBytes)
		}

		text, err := Decode(raw, response, doc.Encoding)
		if err != nil {
			return nil, apperror.Wrap(apperror.Corrupt, err, "响应解码失败")
		}
		return &Response{
			StatusCode:  response.StatusCode,
			ContentType: response.Header.Get("Content-Type"),
			FinalURL:    current,
			Text:        text,
			Garbled:     LooksGarbled(text),
		}, nil
	}
	return nil, apperror.Newf(apperror.Unavailable, "重定向超过 %d 跳", maxFetchRedirect)
}

func retryAfterSeconds(raw string, now time.Time) int {
	raw = strings.TrimSpace(raw)
	if seconds, err := strconv.Atoi(raw); err == nil && seconds > 0 {
		return seconds
	}
	if date, err := http.ParseTime(raw); err == nil && date.After(now) {
		return int((date.Sub(now) + time.Second - 1) / time.Second)
	}
	return 0
}

// applyHeaders 把源级与操作级的头合并到请求上，操作级覆盖源级。
//
// Referer 不在这里被截断：reports/03:97 要求 source headers 一路保留到实际
// 资源请求，而真实漫画 CDN 校验的往往是**完整的页面地址**（含路径），
// 截成 origin 会直接判死。图片代理路由走的是同一份头，见 ADR 0016。
func applyHeaders(request *http.Request, doc *Document, op *Operation) {
	request.Header.Set("User-Agent", defaultUserAgent)
	request.Header.Set("Accept", "text/html,application/xhtml+xml,application/json;q=0.9,*/*;q=0.8")
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	for _, set := range []map[string]string{doc.Headers, op.Headers} {
		for name, value := range set {
			trimmed := strings.TrimSpace(name)
			if trimmed == "" {
				continue
			}
			// Accept-Encoding 刻意不接受覆盖：一旦由我们声明，Go 就不再透明解压，
			// 而手动解压要求尺寸上限作用在解压后的流上（见 NewFetcher 的说明）。
			if strings.EqualFold(trimmed, "Accept-Encoding") {
				continue
			}
			request.Header.Set(trimmed, value)
		}
	}
}

// validatePublicURL 拒绝内网与回环地址。
//
// 复用 proxy 的那一份实现而不是自己写一遍：SSRF 判据是那种"两份实现各自漂移、
// 其中一份出现洞"的代码，而 proxy.ValidateTarget 已经连着
// allowPrivateTargets 这个用户开关（自建源与 NAS 要它才连得上）。
func validatePublicURLContext(ctx context.Context, parsed *url.URL) error {
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return apperror.Newf(apperror.Validation, "不支持的地址协议 %q，只认 http 与 https", parsed.Scheme)
	}
	if err := proxy.ValidateTarget(ctx, parsed); err != nil {
		return apperror.Wrap(apperror.Validation, err, "请求地址被拒绝")
	}
	return nil
}

// classifyTransportError 把 net 层的错误翻成有编码的错误。
//
// 超时与连不上在界面上是两种不同的处置：超时值得重试一次，
// 连不上通常意味着源地址写错了或者被封了，重试只是浪费时间。
//
// 这里按错误的**类型**判而不是按文案：net 层给的是 *net.OpError /
// *url.Error 包着 os.ErrDeadlineExceeded 或 net.Error.Timeout()，
// 文案匹配会随着 Go 版本改字而静默失效。
func classifyTransportError(err error) error {
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return apperror.Wrap(apperror.Timeout, err, "请求超时")
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return apperror.Wrap(apperror.Timeout, err, "请求超时")
	}
	if errors.Is(err, context.Canceled) {
		return apperror.Wrap(apperror.Cancelled, err, "请求已取消")
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		if dnsErr.IsNotFound {
			return apperror.Wrap(apperror.NotFound, err, fmt.Sprintf("域名 %s 解析不到", dnsErr.Name))
		}
		return apperror.Wrap(apperror.Unavailable, err, "域名解析失败")
	}
	return apperror.Wrap(apperror.Unavailable, err, "请求失败")
}
