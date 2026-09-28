package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	// lineProbeTimeout 是单条线路的探测上限。测速是并发跑的，单个慢节点不能把整轮拖死。
	lineProbeTimeout = 8 * time.Second
	// lineSampleBudget 是取样字节数：够区分 CDN 快慢，又不至于下载半部剧。
	lineSampleBudget = 1 << 20
	lineMaxRedirects = 5
	// lineMaxPlaylistHop 限制「列表套列表」的下钻层数，畸形主列表不能让我们无限请求。
	lineMaxPlaylistHop = 3
)

const lineProbeUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/131.0 Safari/537.36"

// PlayURLSample 是一个播放地址的探测结果。
//
// Error 只进日志，不进界面：测速面板按 OK 显示「可用/不可用」，把上游英文报错直出到
// 中文界面上既不可读，也绕过了 i18n。
type PlayURLSample struct {
	OK          bool    `json:"ok"`
	LatencyMS   int64   `json:"latency_ms"`
	Bytes       int64   `json:"bytes"`
	BytesPerSec float64 `json:"bytes_per_sec"`
	ProbedURL   string  `json:"probed_url,omitempty"`
	Error       string  `json:"error,omitempty"`
}

// lineProbeClient 与 HLS 代理共用拨号策略：safeDialContext 在拨号瞬间复核地址，
// 私网放行开关因此对测速同样生效。不复用 HLSService.client，因为测速要并发打多个域，
// 共享连接池和 cookie jar 只会让线路之间互相污染。
var lineProbeClient = &http.Client{
	Timeout: lineProbeTimeout,
	Transport: &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 8 * time.Second,
		DialContext:         safeDialContext,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

type lineRead struct {
	status   int
	body     []byte
	bytes    int64
	latency  time.Duration
	elapsed  time.Duration
	finalURL *url.URL
}

// ProbePlayURL 量一个播放地址的真实速度。
//
// m3u8 地址本身只有一张几十 KB 的列表，按列表测出来的是「取列表」的速度，几条线路几乎
// 并列、排不出快慢；所以命中列表时顺着找它的第一个分片，用分片重新取样。直连媒体地址
// 就地取样即可。探测全程走 Go 侧出网，与播放代理同一套地址校验，不因测速放开私网。
func ProbePlayURL(ctx context.Context, rawURL string) PlayURLSample {
	ctx, cancel := context.WithTimeout(ctx, lineProbeTimeout)
	defer cancel()

	sample := PlayURLSample{}
	target, err := parsePublicHTTPURL(rawURL)
	if err != nil {
		sample.Error = fmt.Sprintf("resolve probe target: %v", err)
		return sample
	}
	current, referer := target, RefererOrigin(rawURL)

	for hop := 0; ; hop++ {
		read, fetchErr := lineFetch(ctx, current, referer)
		if read != nil {
			sample.LatencyMS = read.latency.Milliseconds()
		}
		if fetchErr != nil {
			sample.Error = fmt.Sprintf("probe %s: %v", current, fetchErr)
			return sample
		}
		next := (*url.URL)(nil)
		if hop < lineMaxPlaylistHop {
			next = firstPlaylistURI(read.body, read.finalURL)
		}
		if next == nil {
			// 取不到分片地址（直连媒体、加密列表、纯注释行或已达下钻上限）时，
			// 本次样本就是终值：列表样本仍是有效排序依据，只是区分度低。
			return finishSample(sample, read)
		}
		current, referer = next, RefererOrigin(read.finalURL.String())
	}
}

func finishSample(sample PlayURLSample, read *lineRead) PlayURLSample {
	sample.Bytes = read.bytes
	sample.ProbedURL = read.finalURL.String()
	if read.status < 200 || read.status >= 300 {
		sample.Error = fmt.Sprintf("probe %s: status %d", read.finalURL, read.status)
		return sample
	}
	if read.bytes <= 0 {
		sample.Error = fmt.Sprintf("probe %s: empty response", read.finalURL)
		return sample
	}
	// elapsed 为 0 说明这次取样快过本机时钟精度，量不出吞吐。它依然是一条能放的线路，
	// 只是没有可比较的速度值：BytesPerSec 留 0，由排序方按「快过量级」处理。
	if read.elapsed > 0 {
		sample.BytesPerSec = float64(read.bytes) / read.elapsed.Seconds()
	}
	sample.OK = true
	return sample
}

// lineFetch 取回地址的前 lineSampleBudget 字节。逐跳重新校验重定向目标，跟 HLS 代理
// 一致：否则一次 302 到内网的跳板就能绕过地址白名单。
func lineFetch(ctx context.Context, initial *url.URL, referer string) (*lineRead, error) {
	current := initial
	for hops := 0; hops <= lineMaxRedirects; hops++ {
		read, location, err := lineGet(ctx, current, referer, lineSampleBudget)
		if err != nil {
			return nil, err
		}
		if read == nil {
			next, parseErr := parsePublicHTTPURL(location.String())
			if parseErr != nil {
				return nil, parseErr
			}
			current = next
			continue
		}
		if read.status == http.StatusBadRequest || read.status == http.StatusRequestedRangeNotSatisfiable {
			// 不接受 Range 的源不是「线路坏了」：去掉 Range 重取一次，否则测速会把好线路
			// 标成不可选，用户被告知要换源而其实播放正常。
			if plain, _, plainErr := lineGet(ctx, current, referer, 0); plainErr == nil && plain != nil {
				return plain, nil
			}
		}
		return read, nil
	}
	return nil, fmt.Errorf("too many redirects")
}

// lineGet 发一次取样请求。maxBytes<=0 表示不带 Range；3xx 返回 (nil, location, nil)，
// 由调用方校验并跟进下一跳。
func lineGet(ctx context.Context, current *url.URL, referer string, maxBytes int64) (*lineRead, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, current.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("User-Agent", lineProbeUserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Encoding", "identity")
	if maxBytes > 0 {
		req.Header.Set("Range", "bytes=0-"+strconv.FormatInt(maxBytes-1, 10))
	}
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	sent := time.Now()
	resp, err := lineProbeClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	latency := time.Since(sent)
	if resp.StatusCode >= 300 && resp.StatusCode <= 399 {
		location, locErr := resp.Location()
		resp.Body.Close()
		if locErr != nil {
			return nil, nil, locErr
		}
		return nil, location, nil
	}
	out := &lineRead{status: resp.StatusCode, latency: latency, finalURL: current}
	var limit int64 = lineSampleBudget
	if maxBytes > 0 {
		limit = maxBytes
	}
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, limit))
	out.elapsed = time.Since(sent)
	resp.Body.Close()
	out.body = body
	out.bytes = int64(len(body))
	if readErr != nil && out.bytes == 0 {
		return nil, nil, readErr
	}
	return out, nil, nil
}

// firstPlaylistURI 取列表里第一个非注释地址，并按列表自身的地址解析相对路径。
// 不是播放列表（直连 mp4 等）时返回 nil，调用方就用当前样本收尾。
func firstPlaylistURI(body []byte, base *url.URL) *url.URL {
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("#EXTM3U")) {
		return nil
	}
	for _, line := range bytes.Split(body, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || bytes.HasPrefix(trimmed, []byte("#")) {
			continue
		}
		resolved, err := base.Parse(string(trimmed))
		if err != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") {
			continue
		}
		if err := ValidateTarget(context.Background(), resolved); err != nil {
			continue
		}
		return resolved
	}
	return nil
}
