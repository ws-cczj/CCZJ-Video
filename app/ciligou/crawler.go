package ciligou

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
)

const (
	baseURL  = "https://www.ciligou.date"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0"
)

const (
	minRequestInterval = 8 * time.Second
	maxRequestInterval = 30 * time.Second
)

var (
	client = &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		},
	}

	lastRequestTime time.Time
	rateMu          sync.Mutex

	// 搜索结果列表正则
	searchResultRegex = regexp.MustCompile(`<li>\s*<div class="Search_title_wrapper">[\s\S]*?<a[^>]*href="(/magnet_download/[^"]+\.html)"[^>]*class="SearchListTitle_result_title"[^>]*>([\s\S]*?)</a>[\s\S]*?<span class="Search_result_type">\s*<i class="iconfont icon-citie Search_icon_citie"></i>(\d+)</span>[\s\S]*?<em>文件类型：</em>([^<]+)</li>`)

	// 详情页磁力链接正则
	magnetRegex = regexp.MustCompile(`(magnet:\?xt=urn:btih:[a-zA-Z0-9]+)`)

	// 文件列表正则 - 提取文件名
	fileListRegex = regexp.MustCompile(`<div class="File_list_info">([^<]+)<div`)

	// 可播放文件扩展名
	playableExts = []string{".mp4", ".mkv", ".avi", ".rmvb", ".wmv", ".flv", ".mov", ".ts", ".m4v", ".webm", ".mpg", ".mpeg", ".3gp", ".rm"}
)

// SearchResult 搜索结果
type SearchResult struct {
	URL      string
	Title    string
	Visits   int
	FileType string
}

// waitRateLimit 请求频率限制
func waitRateLimit() {
	rateMu.Lock()
	defer rateMu.Unlock()

	elapsed := time.Since(lastRequestTime)
	if elapsed < minRequestInterval {
		base := minRequestInterval - elapsed
		time.Sleep(base)
	}
	jitterRange := maxRequestInterval - minRequestInterval
	if jitterRange > 0 {
		jitter := time.Duration(rand.Int63n(int64(jitterRange)))
		time.Sleep(jitter)
	}
	lastRequestTime = time.Now()
}

// fetchHTML 获取页面 HTML
func fetchHTML(urlStr string) (string, error) {
	waitRateLimit()

	applog.Info("[Ciligou] Fetching URL: %s", urlStr)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	return string(body), nil
}

// urlEncodeKeyword 将关键词 URL 编码为 UTF-8 十六进制格式
func urlEncodeKeyword(keyword string) string {
	encoded := ""
	for _, r := range keyword {
		b := []byte(string(r))
		for _, by := range b {
			encoded += fmt.Sprintf("%02x", by)
		}
	}
	return encoded
}

// SearchMagnet 搜索磁力链接
func SearchMagnet(keyword string) ([]SearchResult, error) {
	encoded := urlEncodeKeyword(keyword)
	searchURL := fmt.Sprintf("%s/magnet_search/%s-1-length.html", baseURL, encoded)

	applog.Info("[Ciligou] Searching magnet for: %s", keyword)

	html, err := fetchHTML(searchURL)
	if err != nil {
		return nil, err
	}

	return parseSearchResults(html)
}

// parseSearchResults 解析搜索结果页面
func parseSearchResults(html string) ([]SearchResult, error) {
	var results []SearchResult
	seen := make(map[string]bool)

	// 使用改进的正则逐个匹配搜索结果
	// 先找到所有 <li> 块
	liRegex := regexp.MustCompile(`<li>\s*<div class="Search_title_wrapper">([\s\S]*?)</li>`)
	liMatches := liRegex.FindAllStringSubmatch(html, -1)

	for _, liMatch := range liMatches {
		if len(liMatch) < 2 {
			continue
		}
		block := liMatch[1]

		// 提取链接
		linkMatch := regexp.MustCompile(`href="(/magnet_download/[^"]+\.html)"`).FindStringSubmatch(block)
		if len(linkMatch) < 2 {
			// 可能在 li 的外部
			fullBlock := liMatch[0]
			linkMatch = regexp.MustCompile(`href="(/magnet_download/[^"]+\.html)"`).FindStringSubmatch(fullBlock)
		}
		if len(linkMatch) < 2 {
			continue
		}
		link := linkMatch[1]

		if seen[link] {
			continue
		}
		seen[link] = true

		// 提取标题
		titleMatch := regexp.MustCompile(`class="SearchListTitle_result_title"[^>]*>([\s\S]*?)</a>`).FindStringSubmatch(block)
		title := ""
		if len(titleMatch) >= 2 {
			title = cleanHTML(titleMatch[1])
		}

		// 提取访问量
		visitsMatch := regexp.MustCompile(`icon-citie Search_icon_citie"></i>(\d+)</span>`).FindStringSubmatch(liMatch[0])
		visits := 0
		if len(visitsMatch) >= 2 {
			visits, _ = strconv.Atoi(visitsMatch[1])
		}

		// 提取文件类型
		fileTypeMatch := regexp.MustCompile(`<em>文件类型：</em>([^<]+)</div>`).FindStringSubmatch(liMatch[0])
		fileType := ""
		if len(fileTypeMatch) >= 2 {
			fileType = strings.TrimSpace(fileTypeMatch[1])
		}

		results = append(results, SearchResult{
			URL:      link,
			Title:    title,
			Visits:   visits,
			FileType: fileType,
		})
	}

	// 按访问量降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].Visits > results[j].Visits
	})

	return results, nil
}

// cleanHTML 去除 HTML 标签
func cleanHTML(s string) string {
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	result := tagRegex.ReplaceAllString(s, "")
	return strings.TrimSpace(result)
}

// GetDetailPage 获取详情页 HTML
func GetDetailPage(detailURL string) (string, error) {
	fullURL := detailURL
	if !strings.HasPrefix(detailURL, "http") {
		fullURL = baseURL + detailURL
	}
	return fetchHTML(fullURL)
}

// ExtractMagnetLink 从详情页提取磁力链接
func ExtractMagnetLink(html string) string {
	match := magnetRegex.FindStringSubmatch(html)
	if len(match) >= 2 {
		return match[1]
	}
	return ""
}

// IsPlayableContent 检查文件列表中是否包含可播放的文件
func IsPlayableContent(html string) bool {
	// 提取文件列表
	fileMatches := fileListRegex.FindAllStringSubmatch(html, -1)
	for _, match := range fileMatches {
		if len(match) >= 2 {
			fileName := strings.ToLower(strings.TrimSpace(match[1]))
			for _, ext := range playableExts {
				if strings.Contains(fileName, ext) {
					return true
				}
			}
		}
	}

	// 也检查文件类型标签
	if strings.Contains(strings.ToLower(html), ".mp4") ||
		strings.Contains(strings.ToLower(html), ".mkv") ||
		strings.Contains(strings.ToLower(html), ".avi") {
		return true
	}

	return false
}

// FetchMagnetForVideo 为视频获取磁力链接
// 返回磁力链接字符串，如果失败返回空字符串
func FetchMagnetForVideo(keyword string) (string, error) {
	applog.Info("[Ciligou] Fetching magnet for: %s", keyword)

	// 步骤1: 搜索
	results, err := SearchMagnet(keyword)
	if err != nil {
		return "", fmt.Errorf("search failed: %w", err)
	}

	if len(results) == 0 {
		return "", fmt.Errorf("no search results for '%s'", keyword)
	}

	applog.Info("[Ciligou] Found %d search results for '%s'", len(results), keyword)

	// 步骤2: 按访问量从高到低逐个尝试
	for i, result := range results {
		applog.Info("[Ciligou] [%d/%d] Trying: %s (visits=%d)", i+1, len(results), result.Title, result.Visits)

		html, err := GetDetailPage(result.URL)
		if err != nil {
			applog.Warn("[Ciligou] Failed to fetch detail page: %v", err)
			continue
		}

		// 步骤3: 判断是否是可播放文件
		if !IsPlayableContent(html) {
			applog.Info("[Ciligou] Result '%s' is not playable content, trying next", result.Title)
			continue
		}

		// 步骤4: 提取磁力链接
		magnetLink := ExtractMagnetLink(html)
		if magnetLink != "" {
			applog.Info("[Ciligou] Found magnet link for '%s': %s", keyword, magnetLink[:60]+"...")
			return magnetLink, nil
		}

		applog.Info("[Ciligou] No magnet link found in result '%s', trying next", result.Title)
	}

	return "", fmt.Errorf("no playable magnet found for '%s' after checking %d results", keyword, len(results))
}

// URLDecodeKeyword 解码关键词（用于调试）
func URLDecodeKeyword(encoded string) string {
	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		return encoded
	}
	return decoded
}