package douban

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
)

const (
	searchURL = "https://search.douban.com/movie/subject_search"
	detailURL = "https://movie.douban.com/subject/%s/"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0"
	referer   = "https://movie.douban.com/"
	cookie    = "ll=\"118123\"; bid=WBzHblgEgfs; ap_v=0,6.0; dbcl2=\"242252407:PrkTXHxDupE\"; ck=Ket5; frodotk_db=\"4d668806270f83b5e8339812752e8b1f\"; push_noty_num=0; push_doumail_num=0"
)

const minRequestInterval = 50 * time.Second

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

	subjectIDRegex = regexp.MustCompile(`href=["']https?://movie\.douban\.com/subject/(\d+)/?["']`)

	directorRegex     = regexp.MustCompile(`<span class="pl">导演</span>\s*:\s*<span class="attrs">([\s\S]*?)</span></span>`)
	writerRegex       = regexp.MustCompile(`<span class="pl">编剧</span>\s*:\s*<span class="attrs">([\s\S]*?)</span></span>`)
	actorRegex        = regexp.MustCompile(`<span class="pl">主演</span>\s*:\s*<span class="attrs">([\s\S]*?)</span></span>`)
	genreRegex        = regexp.MustCompile(`<span class="pl">类型:</span>\s*<span property="v:genre">([^<]+)</span>`)
	countryRegex      = regexp.MustCompile(`<span class="pl">制片国家/地区:</span>\s*([^<]+)`)
	languageRegex     = regexp.MustCompile(`<span class="pl">语言:</span>\s*([^<]+)`)
	releaseDateRegex  = regexp.MustCompile(`<span class="pl">首播:</span>\s*<span property="v:initialReleaseDate"[^>]*>([^<]+)</span>`)
	episodeCountRegex = regexp.MustCompile(`<span class="pl">集数:</span>\s*([^<]+)`)
	seasonCountRegex  = regexp.MustCompile(`<span class="pl">季数:</span>\s*([^<]+)`)
	durationRegex     = regexp.MustCompile(`<span class="pl">单集片长:</span>\s*([^<]+)`)
	akaRegex          = regexp.MustCompile(`<span class="pl">又名:</span>\s*([^<]+)`)
	imdbRegex         = regexp.MustCompile(`<span class="pl">IMDb:</span>\s*([^<]+)`)
	ratingRegex       = regexp.MustCompile(`<strong class="ll rating_num" property="v:average">([\d.]+)</strong>`)
	votesRegex        = regexp.MustCompile(`<span property="v:votes">(\d+)</span>`)
	posterRegex       = regexp.MustCompile(`<img[^>]*src=["']([^"']*doubanio[^"']*\.webp)["'][^>]*alt=["']([^"']+)`)

	linkTextRegex = regexp.MustCompile(`<a[^>]*>([^<]+)</a>`)

	antiCrawlPatterns = []string{
		"载入中...",
		"加载中",
		"验证码",
		"请登录",
		"403 Forbidden",
		"访问过于频繁",
		"您的访问请求被拒绝",
		"系统检测到异常请求",
	}
)

type DoubanInfo struct {
	SubjectID    string
	Title        string
	Rating       string
	Votes        string
	Director     string
	Writer       string
	Actor        string
	Genre        string
	Country      string
	Language     string
	ReleaseDate  string
	SeasonCount  string
	EpisodeCount string
	Duration     string
	Aka          string
	IMDb         string
	PosterURL    string
}

func waitRateLimit() {
	rateMu.Lock()
	defer rateMu.Unlock()

	elapsed := time.Since(lastRequestTime)
	if elapsed < minRequestInterval {
		waitTime := minRequestInterval - elapsed
		applog.Debug("[Douban] Rate limiting: waiting %.1fs before next request", waitTime.Seconds())
		time.Sleep(waitTime)
	}
	lastRequestTime = time.Now()
}

func checkAntiCrawl(html string) bool {
	for _, pattern := range antiCrawlPatterns {
		if strings.Contains(html, pattern) {
			applog.Warn("[Douban] Anti-crawl detected: pattern '%s' matched", pattern)
			return true
		}
	}
	return false
}

func fetchHTML(urlStr string) (string, error) {
	startTime := time.Now()
	waitRateLimit()

	applog.Info("[Douban] Fetching URL: %s", urlStr)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		applog.Error("[Douban] Failed to create request: %v", err)
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		applog.Error("[Douban] HTTP request failed: %v", err)
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)
	applog.Info("[Douban] HTTP status: %d, duration: %.2fs", resp.StatusCode, duration.Seconds())

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		if len(body) > 0 {
			snippet := string(body)
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			applog.Warn("[Douban] HTTP %d response snippet: %s", resp.StatusCode, snippet)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			location := resp.Header.Get("Location")
			applog.Warn("[Douban] Redirect detected: %d -> %s", resp.StatusCode, location)
			return "", fmt.Errorf("HTTP %d redirect to %s", resp.StatusCode, location)
		}
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		applog.Error("[Douban] Failed to read response body: %v", err)
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	html := string(body)

	if len(html) < 100 {
		applog.Warn("[Douban] Suspiciously short response (len=%d): %s", len(html), html)
	}

	if checkAntiCrawl(html) {
		applog.Warn("[Douban] Anti-crawl triggered, response length: %d", len(html))
		return "", fmt.Errorf("anti-crawl detected")
	}

	return html, nil
}

func SearchSubjectID(keyword string) (string, error) {
	applog.Info("[Douban] Searching for keyword: %s", keyword)

	params := url.Values{}
	params.Set("search_text", keyword)

	fullURL := searchURL + "?" + params.Encode()

	html, err := fetchHTML(fullURL)
	if err != nil {
		applog.Error("[Douban] Search fetch failed for '%s': %v", keyword, err)
		return "", err
	}

	matches := subjectIDRegex.FindStringSubmatch(html)
	if len(matches) >= 2 {
		subjectID := matches[1]
		applog.Info("[Douban] Found subject ID: %s for keyword: %s", subjectID, keyword)
		return subjectID, nil
	}

	applog.Warn("[Douban] No subject ID found for keyword: %s (HTML length: %d)", keyword, len(html))

	if len(html) > 1000 {
		applog.Debug("[Douban] Response snippet (first 500 chars): %s", html[:500])
	}

	return "", fmt.Errorf("no subject ID found for keyword: %s", keyword)
}

func ExtractLinkTexts(html string) string {
	links := linkTextRegex.FindAllStringSubmatch(html, -1)
	var names []string
	for _, link := range links {
		if len(link) >= 2 {
			name := strings.TrimSpace(link[1])
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return strings.Join(names, " / ")
}

func ParseDetail(subjectID string) (*DoubanInfo, error) {
	applog.Info("[Douban] Parsing detail for subject ID: %s", subjectID)

	urlStr := fmt.Sprintf(detailURL, subjectID)

	html, err := fetchHTML(urlStr)
	if err != nil {
		applog.Error("[Douban] Detail fetch failed for %s: %v", subjectID, err)
		return nil, err
	}

	info := &DoubanInfo{
		SubjectID: subjectID,
	}

	if matches := ratingRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Rating = strings.TrimSpace(matches[1])
	}

	if matches := votesRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Votes = strings.TrimSpace(matches[1])
	}

	if matches := directorRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Director = ExtractLinkTexts(matches[1])
	}

	if matches := writerRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Writer = ExtractLinkTexts(matches[1])
	}

	if matches := actorRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Actor = ExtractLinkTexts(matches[1])
	}

	if matches := genreRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Genre = strings.TrimSpace(matches[1])
	}

	if matches := countryRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Country = strings.TrimSpace(matches[1])
	}

	if matches := languageRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Language = strings.TrimSpace(matches[1])
	}

	if matches := releaseDateRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.ReleaseDate = strings.TrimSpace(matches[1])
	}

	if matches := episodeCountRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.EpisodeCount = strings.TrimSpace(matches[1])
	}

	if matches := seasonCountRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.SeasonCount = strings.TrimSpace(matches[1])
	}

	if matches := durationRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Duration = strings.TrimSpace(matches[1])
	}

	if matches := akaRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Aka = strings.TrimSpace(matches[1])
	}

	if matches := imdbRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.IMDb = strings.TrimSpace(matches[1])
	}

	if matches := posterRegex.FindStringSubmatch(html); len(matches) >= 3 {
		info.PosterURL = strings.TrimSpace(matches[1])
		info.Title = strings.TrimSpace(matches[2])
	}

	applog.Info("[Douban] Parsed detail for %s: Title='%s', Rating='%s', Votes='%s', Director='%s', Actor='%s', Genre='%s'",
		subjectID, info.Title, info.Rating, info.Votes, truncate(info.Director, 30), truncate(info.Actor, 30), info.Genre)

	return info, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func FetchDoubanInfo(keyword string) (*DoubanInfo, error) {
	applog.Info("[Douban] Fetching complete info for keyword: %s", keyword)

	subjectID, err := SearchSubjectID(keyword)
	if err != nil {
		applog.Error("[Douban] FetchDoubanInfo failed at search step for '%s': %v", keyword, err)
		return nil, err
	}

	return ParseDetail(subjectID)
}