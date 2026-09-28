// Package download owns download transports and playlist parsing.
package download

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// downloaderUA 是下载侧所有上游请求共用的 UA。播放代理用的是浏览器 UA，因为那边
// 要骗过「只收 WebView 请求」的站点；下载是显式用户动作，标清自己更好排查。
const downloaderUA = "Mozilla/5.0 CCZJ-Video-Downloader/1.0"

func IsHLSURL(u string) bool {
	trimmed := u
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	return strings.Contains(strings.ToLower(trimmed), ".m3u8")
}

// resolveURL 相对路径转绝对
func resolveURL(base, ref string) string {
	if ref == "" {
		return base
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refParsed, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	resolved := baseParsed.ResolveReference(refParsed)
	return resolved.String()
}

// adDomainBlacklist 已知广告域名关键字（匹配 hostname 子串）
var adDomainBlacklist = []string{
	"dcs-vod.", "vod-dcs.",
	"ads.", "ad.", "advert",
	"dsp.", "doubleclick",
	"googlesyndication", "googleads",
}

// extractHost 从 URL 中提取 hostname
func extractHost(rawURL string) string {
	// 快速提取 host，避免完整 url.Parse 开销
	s := rawURL
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "@"); idx >= 0 {
		s = s[idx+1:]
	}
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// ByteRange 是 #EXT-X-BYTERANGE：一个片段可以只是某个大文件里的一段字节。
// Start 为 -1 表示「接着上一个区间在这个文件里的尾部」，由解析器补全。
type ByteRange struct {
	Length int64 `json:"length"`
	Start  int64 `json:"start"`
}

// Header 转成下载用的 Range 请求头；区间不完整时返回空串，让整文件下载兜底而不是
// 猜一个区间。
func (b *ByteRange) Header() string {
	if b == nil || b.Length <= 0 || b.Start < 0 {
		return ""
	}
	return fmt.Sprintf("bytes=%d-%d", b.Start, b.Start+b.Length-1)
}

// Key 是 #EXT-X-KEY 描述的分片加密方式。Method 为 AES-128 时 URI 必须能取到 16
// 字节密钥；IV 缺省时按 RFC 8216 用片段序号推导。
type Key struct {
	Method string `json:"method"`
	URI    string `json:"uri"`
	IV     []byte `json:"iv,omitempty"`
}

// Segment 是播放列表里的一次下载。Init 为真表示这条是 #EXT-X-MAP 指向的初始化段，
// 由下载序列插入，不是列表里的 URI 行。
type Segment struct {
	URL           string     `json:"url"`
	Range         *ByteRange `json:"range,omitempty"`
	Key           *Key       `json:"key,omitempty"`
	MapURL        string     `json:"map_url,omitempty"`
	Seq           int64      `json:"seq"`
	Duration      float64    `json:"duration"`
	Discontinuous bool       `json:"discontinuous,omitempty"`
	Init          bool       `json:"init,omitempty"`
}

// Playlist 是解析加广告过滤之后的结果。Unsupported 记的是本下载器处理不了的标签：
// 下载前必须看一眼，否则会产出一个「大小正常但放不出来」的文件，比直接报错难查得多。
type Playlist struct {
	Segments    []Segment
	Unsupported []string
}

// ParsePlaylist 解析媒体播放列表，保留 EXT-X-KEY / EXT-X-MAP / EXT-X-BYTERANGE，
// 然后做双层广告过滤。过滤是按片段记录做的，所以留下的片段仍然带着自己的密钥和区间。
func ParsePlaylist(m3u8URL, content string) *Playlist {
	segs, issues := parseSegments(m3u8URL, content)
	segs = filterAdSegments(segs)
	return &Playlist{Segments: segs, Unsupported: dedupeStrings(issues)}
}

// ParseM3U8Segments 只取片段 URL：给不需要加密信息的调用方，以及老调用点用。
func ParseM3U8Segments(m3u8URL, content string) []string {
	segs, _ := parseSegments(m3u8URL, content)
	var result []string
	for _, seg := range filterAdSegments(segs) {
		result = append(result, seg.URL)
	}
	return result
}

// SegmentsFromURLs 把只剩 URL 的老列表（断点续传自旧版本）还原成片段记录。
// 密钥和初始化段已经丢了，所以只能按明文整文件处理。
func SegmentsFromURLs(urls []string) []Segment {
	if len(urls) == 0 {
		return nil
	}
	out := make([]Segment, 0, len(urls))
	for i, raw := range urls {
		out = append(out, Segment{URL: raw, Seq: int64(i)})
	}
	return out
}

// WriteSequence 把播放列表展开成落盘顺序：#EXT-X-MAP 的初始化段要插在它服务的第一个
// 片段前面，同一个初始化段只写一次。fMP4 分片单独拼接是不可放的，缺了 init 就是缺了
// 解码所需的 moov。
func WriteSequence(segs []Segment) ([]Segment, []string) {
	out := make([]Segment, 0, len(segs))
	var issues []string
	seen := make(map[string]bool)
	for _, seg := range segs {
		if seg.MapURL != "" && !seen[seg.MapURL] {
			seen[seg.MapURL] = true
			init := Segment{URL: seg.MapURL, Init: true, Seq: seg.Seq}
			if seg.Key != nil {
				// 加密的初始化段只有显式 IV 才能确定地解出来：规范没规定它的默认
				// IV 取哪个序号。
				if len(seg.Key.IV) == 0 {
					issues = append(issues, "EXT-X-MAP 处于 AES-128 密钥下但没有显式 IV")
					continue
				}
				init.Key = seg.Key
			}
			out = append(out, init)
		}
		out = append(out, seg)
	}
	return out, issues
}

// parseSegments 单趟扫描播放列表，把标签状态挂到它作用的片段上。
// EXT-X-KEY、EXT-X-MAP、EXT-X-BYTERANGE 都是「对后续片段生效」的状态，不能读完就丢。
func parseSegments(m3u8URL, content string) ([]Segment, []string) {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var (
		segs   []Segment
		issues []string
		// 生效状态
		key          *Key
		mapURL       string
		pending      *ByteRange
		mediaSeq     int64
		index        int64
		discont      bool
		duration     float64
		lastRange    *ByteRange
		lastRangeURL string
	)
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "#EXT-X-MEDIA-SEQUENCE:"):
			if v, err := strconv.ParseInt(strings.TrimSpace(strings.SplitN(line, ":", 2)[1]), 10, 64); err == nil {
				mediaSeq = v
			}
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			parsed, err := parseKey(m3u8URL, line)
			if err != nil {
				issues = append(issues, err.Error())
			}
			key = parsed
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			mapURL = resolveURL(m3u8URL, strings.Trim(parseAttributes(line)[`URI`], `"`))
		case strings.HasPrefix(line, "#EXT-X-DISCONTINUITY"):
			discont = true
		case strings.HasPrefix(line, "#EXT-X-BYTERANGE:"):
			rng, err := parseByteRange(strings.SplitN(line, ":", 2)[1])
			if err != nil {
				issues = append(issues, err.Error())
				pending = nil
			} else {
				pending = rng
			}
		case strings.HasPrefix(line, "#EXTINF:"):
			duration = parseExtinfDuration(line)
		case strings.HasPrefix(line, "#"):
			continue
		default:
			absURL := resolveURL(m3u8URL, line)
			seg := Segment{
				URL:           absURL,
				Key:           key,
				MapURL:        mapURL,
				Seq:           mediaSeq + index,
				Duration:      duration,
				Discontinuous: discont,
			}
			index++
			discont = false
			duration = 0
			if pending != nil {
				rng := *pending
				if rng.Start < 0 {
					switch {
					case lastRangeURL == absURL && lastRange != nil:
						rng.Start = lastRange.Start + lastRange.Length
					default:
						issues = append(issues, "EXT-X-BYTERANGE 没有起点，也接不上前一个区间")
					}
				}
				if rng.Start >= 0 && rng.Length > 0 {
					seg.Range = &rng
					lastRange = &rng
					lastRangeURL = absURL
				} else {
					lastRange = nil
					lastRangeURL = ""
				}
				pending = nil
			}
			segs = append(segs, seg)
		}
	}
	return segs, issues
}

func parseKey(m3u8URL, line string) (*Key, error) {
	attrs := parseAttributes(line)
	method := strings.ToUpper(strings.TrimSpace(attrs["METHOD"]))
	if method == "" || method == "NONE" {
		return nil, nil
	}
	key := &Key{Method: method, URI: resolveURL(m3u8URL, strings.Trim(attrs["URI"], `"`))}
	switch method {
	case "AES-128":
		if key.URI == "" {
			return key, fmt.Errorf("EXT-X-KEY METHOD=AES-128 但没有 URI")
		}
	default:
		// SAMPLE-AES / AES-256 / CENC 等：解不了就要在下载前说清楚，
		// 不要产出一个放不出来的文件。
		return key, fmt.Errorf("不支持的 EXT-X-KEY METHOD=%s", method)
	}
	if rawIV := strings.TrimSpace(attrs["IV"]); rawIV != "" {
		iv, err := decodeHexIV(rawIV)
		if err != nil {
			return key, fmt.Errorf("EXT-X-KEY IV 无法解析: %w", err)
		}
		key.IV = iv
	}
	return key, nil
}

func decodeHexIV(raw string) ([]byte, error) {
	hexPart := strings.TrimPrefix(strings.TrimPrefix(raw, "0x"), "0X")
	if len(hexPart) != 32 {
		return nil, fmt.Errorf("IV 长度应为 32 位十六进制，实际 %d", len(hexPart))
	}
	var out []byte
	for i := 0; i < len(hexPart); i += 2 {
		v, err := strconv.ParseUint(hexPart[i:i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out = append(out, byte(v))
	}
	return out, nil
}

func parseByteRange(raw string) (*ByteRange, error) {
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return nil, fmt.Errorf("空的 EXT-X-BYTERANGE")
	}
	lengthPart, startPart := spec, ""
	if i := strings.Index(spec, "@"); i >= 0 {
		lengthPart, startPart = spec[:i], spec[i+1:]
	}
	length, err := strconv.ParseInt(strings.TrimSpace(lengthPart), 10, 64)
	if err != nil || length <= 0 {
		return nil, fmt.Errorf("EXT-X-BYTERANGE 长度无效: %s", spec)
	}
	out := &ByteRange{Length: length, Start: -1}
	if startPart != "" {
		start, startErr := strconv.ParseInt(strings.TrimSpace(startPart), 10, 64)
		if startErr != nil || start < 0 {
			return nil, fmt.Errorf("EXT-X-BYTERANGE 起点无效: %s", spec)
		}
		out.Start = start
	}
	return out, nil
}

func parseExtinfDuration(line string) float64 {
	parts := strings.TrimPrefix(line, "#EXTINF:")
	if idx := strings.Index(parts, ","); idx >= 0 {
		parts = parts[:idx]
	}
	if d, err := strconv.ParseFloat(strings.TrimSpace(parts), 64); err == nil {
		return d
	}
	return 0
}

// parseAttributes 拆 "#TAG:K=V,K=\"a,b\""。逗号在引号内时不是分隔符。
func parseAttributes(line string) map[string]string {
	out := make(map[string]string)
	colon := strings.Index(line, ":")
	if colon < 0 {
		return out
	}
	body := line[colon+1:]
	var parts []string
	var current strings.Builder
	inQuote := false
	for _, r := range body {
		switch {
		case r == '"':
			inQuote = !inQuote
			current.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(r)
		}
	}
	parts = append(parts, current.String())
	for _, part := range parts {
		i := strings.Index(part, "=")
		if i <= 0 {
			continue
		}
		out[strings.ToUpper(strings.TrimSpace(part[:i]))] = strings.TrimSpace(part[i+1:])
	}
	return out
}

func dedupeStrings(in []string) []string {
	var out []string
	seen := make(map[string]bool, len(in))
	for _, v := range in {
		if seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// filterAdSegments 双层广告过滤：第一层域名黑名单，第二层按 DISCONTINUITY 分组，
// 保守移除小组（广告）。判定全部命中时一律保留，宁可留下广告也不要产出空文件。
func filterAdSegments(segs []Segment) []Segment {
	if len(segs) == 0 {
		return segs
	}

	// 第一层：域名黑名单
	adSet := make(map[int]bool)
	for i, seg := range segs {
		host := extractHost(seg.URL)
		for _, d := range adDomainBlacklist {
			if strings.Contains(host, d) {
				adSet[i] = true
				break
			}
		}
	}
	if len(adSet) > 0 && len(adSet) < len(segs) {
		var result []Segment
		for i, seg := range segs {
			if !adSet[i] {
				result = append(result, seg)
			}
		}
		return result
	}

	// 第二层（兜底）：DISCONTINUITY 分组，保守移除小组
	const maxAdSegCount = 4
	const maxAdDuration = 25.0
	type group struct {
		segs     []Segment
		duration float64
	}
	var groups []group
	cur := group{}
	for _, seg := range segs {
		if seg.Discontinuous && len(cur.segs) > 0 {
			groups = append(groups, cur)
			cur = group{}
		}
		cur.segs = append(cur.segs, seg)
		cur.duration += seg.Duration
	}
	if len(cur.segs) > 0 {
		groups = append(groups, cur)
	}
	if len(groups) <= 1 {
		return segs
	}

	isAdGroup := func(g group) bool {
		return len(g.segs) > 0 && len(g.segs) <= maxAdSegCount &&
			g.duration > 0 && g.duration <= maxAdDuration
	}
	contentCount := 0
	for _, g := range groups {
		if !isAdGroup(g) {
			contentCount++
		}
	}
	if contentCount == 0 {
		return segs
	}
	var result []Segment
	for _, g := range groups {
		if isAdGroup(g) {
			continue
		}
		result = append(result, g.segs...)
	}
	return result
}

// Get 取一个小资源（播放列表、密钥、单个分片），带退避重试。
//
// 4xx 里只有 408/425/429 值得重试：其余是「地址过期/没权限」这类永久状况，重试只会
// 把一次失败的下载拖成四次。maxBytes 是硬上限，超了直接判定失败，不去读完整响应体。
func Get(ctx context.Context, client *http.Client, rawURL, referer, rangeHeader string, maxBytes int64) ([]byte, error) {
	const attempts = 4
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(200<<uint(attempt-1)) * time.Millisecond
			if backoff > 2*time.Second {
				backoff = 2 * time.Second
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		data, retryable, err := getOnce(ctx, client, rawURL, referer, rangeHeader, maxBytes)
		if err == nil {
			return data, nil
		}
		lastErr = err
		if !retryable || ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("%s: 重试 %d 次仍失败", lastErr, attempts)
}

func getOnce(ctx context.Context, client *http.Client, rawURL, referer, rangeHeader string, maxBytes int64) ([]byte, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", downloaderUA)
	req.Header.Set("Accept", "*/*")
	// identity：分片要按字节算区间，中间层 gzip 一下 Content-Range 就对不上了。
	req.Header.Set("Accept-Encoding", "identity")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	if rangeHeader != "" {
		req.Header.Set("Range", rangeHeader)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, permanentStatus(resp.StatusCode), fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if rangeHeader != "" && resp.StatusCode != http.StatusPartialContent {
		// 上游收了 Range 却回了 200：整份资源会被当成一个片段写进文件，后面的字节全
		// 错位。重试也只会拿到同样的整份响应，所以直接判定失败。
		return nil, false, fmt.Errorf("range request returned HTTP %d, upstream ignores Range", resp.StatusCode)
	}
	if maxBytes > 0 && resp.ContentLength > maxBytes {
		return nil, false, fmt.Errorf("exceeds size limit of %d bytes", maxBytes)
	}
	if maxBytes > 0 {
		data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
		if readErr != nil {
			return nil, true, readErr
		}
		if int64(len(data)) > maxBytes {
			return nil, false, fmt.Errorf("exceeds size limit of %d bytes", maxBytes)
		}
		return data, false, nil
	}
	data, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, true, readErr
	}
	return data, false, nil
}

func permanentStatus(code int) bool {
	switch code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	default:
		return code >= 500
	}
}

// HTTPGetText 取文本资源（播放列表），失败按 Get 的策略重试。
func HTTPGetText(ctx context.Context, client *http.Client, rawURL, referer string) (string, error) {
	data, err := Get(ctx, client, rawURL, referer, "", 8<<20)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
