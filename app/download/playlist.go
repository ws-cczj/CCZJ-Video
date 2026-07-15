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
)

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

// parseM3u8Segments 解析 m3u8 文件，返回所有 .ts 切片 URL
// 双层广告过滤：第一层域名黑名单，第二层 DISCONTINUITY 分组保守移除小组
func ParseM3U8Segments(m3u8URL, content string) []string {
	lines := strings.Split(content, "\n")

	// 先收集所有片段行
	type segInfo struct {
		lineIdx int
		absURL  string
	}
	var allSegs []segInfo
	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allSegs = append(allSegs, segInfo{i, resolveURL(m3u8URL, line)})
	}

	// 第一层：域名黑名单
	adSet := make(map[int]bool)
	for _, s := range allSegs {
		host := extractHost(s.absURL)
		for _, d := range adDomainBlacklist {
			if strings.Contains(host, d) {
				adSet[s.lineIdx] = true
				break
			}
		}
	}
	// 黑名单命中了部分片段（非全部）→ 过滤掉广告片段
	if len(adSet) > 0 && len(adSet) < len(allSegs) {
		var result []string
		for _, s := range allSegs {
			if !adSet[s.lineIdx] {
				result = append(result, s.absURL)
			}
		}
		return result
	}

	// 第二层（兜底）：DISCONTINUITY 分组，保守移除小组（广告）
	const maxAdSegCount = 4
	const maxAdDuration = 25.0
	type group struct {
		segs     []string
		duration float64
	}
	var groups []group
	cur := group{}
	var lastDuration float64
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "#EXT-X-DISCONTINUITY" {
			if len(cur.segs) > 0 {
				groups = append(groups, cur)
				cur = group{}
			}
			continue
		}
		// 提取 #EXTINF 时长
		if strings.HasPrefix(line, "#EXTINF:") {
			parts := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.Index(parts, ","); idx >= 0 {
				parts = parts[:idx]
			}
			if d, err := strconv.ParseFloat(strings.TrimSpace(parts), 64); err == nil {
				lastDuration = d
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		cur.segs = append(cur.segs, resolveURL(m3u8URL, line))
		cur.duration += lastDuration
		lastDuration = 0
	}
	if len(cur.segs) > 0 {
		groups = append(groups, cur)
	}

	// 无分组或只有一组 → 全部保留
	if len(groups) <= 1 {
		var result []string
		for _, g := range groups {
			result = append(result, g.segs...)
		}
		return result
	}

	// 保守策略：只移除片段数 ≤ maxAdSegCount 且总时长 ≤ maxAdDuration 的小组
	isAdGroup := func(g group) bool {
		return len(g.segs) > 0 && len(g.segs) <= maxAdSegCount &&
			g.duration > 0 && g.duration <= maxAdDuration
	}
	// 统计非广告组数量
	contentCount := 0
	for _, g := range groups {
		if !isAdGroup(g) {
			contentCount++
		}
	}
	// 所有组都被判定为广告 → 全部保留（避免误删全部内容）
	if contentCount == 0 {
		var result []string
		for _, g := range groups {
			result = append(result, g.segs...)
		}
		return result
	}
	// 正常情况：只移除广告小组
	var result []string
	for _, g := range groups {
		if !isAdGroup(g) {
			result = append(result, g.segs...)
		}
	}
	return result
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

// httpGetText 简单的文本 GET
func HTTPGetText(ctx context.Context, client *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// downloadDirect 直接下载单个文件（支持断点续传、多连接并行下载、暂停）
// 核心改进：
//   - 检测服务器是否支持 Range 头；支持则开启 N 路并发连接（像 IDM 那样）
//   - 每个连接下载独立的字节区间，同时写入同一个文件的不同 offset
//   - 文件内容本身就是最终格式，不需要合并
