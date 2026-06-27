package douban

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

const (
	searchURL = "https://search.douban.com/movie/subject_search"
	detailURL = "https://movie.douban.com/subject/%s/"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0"
	referer   = "https://movie.douban.com/"
	cookie    = "ll=\"118123\"; bid=WBzHblgEgfs; ap_v=0,6.0; dbcl2=\"242252407:PrkTXHxDupE\"; ck=Ket5; frodotk_db=\"4d668806270f83b5e8339812752e8b1f\"; push_noty_num=0; push_doumail_num=0"
)

// 豆瓣请求间隔采用随机抖动（10~30 秒），避免固定节奏被识别为爬虫，
// 同时也不会对豆瓣服务器造成压力（每分钟最多约 2~6 次请求）。
const (
	minRequestInterval = 10 * time.Second
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

	// 豆瓣 ID 提取正则（支持三种格式）
	// 格式 1：常规搜索结果 - href="https://movie.douban.com/subject/35426411/"
	subjectIDRegex = regexp.MustCompile(`href=["']https?://movie\.douban\.com/subject/(\d+)/?["']`)
	// 格式 2：智能搜索结果 - href="https://www.douban.com/doubanapp/dispatch?uri=/tv/35426411"
	// 只匹配 movie 和 tv 类型，排除 book（图书）和 music（音乐）
	subjectIDRegexSmart = regexp.MustCompile(`href=["']https?://www\.douban\.com/doubanapp/dispatch\?uri=/(?:movie|tv)/(\d+)["']`)
	// 格式 3：JSON 转义的链接 - href=\"https://www.douban.com/doubanapp/dispatch?uri=/tv/35426411\"
	subjectIDRegexJSON = regexp.MustCompile(`href=\\"https?://www\.douban\.com/doubanapp/dispatch\?uri=/(?:movie|tv)/(\d+)\\"`)

	// 智能搜索结果中的标题提取 - class="DouWeb-SR-subject-info-name tv">万界独尊 第一季</a>
	smartTitleRegex = regexp.MustCompile(`class=["']DouWeb-SR-subject-info-name[^"]*["'][^>]*>([^<]+)</a>`)
	// 常规搜索结果中的标题提取（title属性格式） - <a href="..." title="万界独尊 第一季">
	regularTitleRegex = regexp.MustCompile(`<a[^>]*href=["']https?://movie\.douban\.com/subject/\d+/?["'][^>]*title=["']([^"']+)["']`)
	// 常规搜索结果中的标题提取（文本格式） - <a ... class="title-text">权力的游戏 第一季 Game of Thrones Season 1‎ (2011)</a>
	titleTextRegex = regexp.MustCompile(`<a[^>]*class=["']title-text["'][^>]*>([^<]+)</a>`)

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
	// 短评数量：豆瓣详情页中 "全部 12345 条" 或 "12345 条短评" 等格式
	shortCommentsRegex = regexp.MustCompile(`(\d[\d,]*)\s*条(?:短评|评论)?`)
	posterRegex       = regexp.MustCompile(`<img[^>]*src=["']([^"']*doubanio[^"']*\.(?:webp|jpe?g|png))["'][^>]*alt=["']([^"']+)`)
	// 标题提取：从 <h1> 中提取（作为 poster 提取失败时的兆底）
	titleRegex        = regexp.MustCompile(`<span\s+property="v:itemreviewed">([^<]+)</span>`)
	
	// ===== 新版搜索结果解析正则（2024+ HTML 结构）=====
	// subject ID from data-moreurl JS param: subject_id:'35861087'
	moreurlSubjectIDRegex = regexp.MustCompile(`subject_id:'(\d+)'`)
	// title from title-text class
	searchTitleTextRegex = regexp.MustCompile(`class="title-text">([^<]+)</a>`)
	// year from title suffix: (2025)
	searchYearRegex = regexp.MustCompile(`\((\d{4})\)\s*$`)
	// meta abstract divs
	searchMetaRegex = regexp.MustCompile(`class="meta abstract_?2?">([^<]*)</div>`)

	linkTextRegex = regexp.MustCompile(`<a[^>]*>([^<]+)</a>`)

	// 用于剥离季数/部数信息的正则，提高搜索命中率
	seasonStripRegex = regexp.MustCompile(`(?i)\s*第[一二三四五六七八九十\d]+[季部季]|\s*Season\s*\d+|\s*Part\s*\d+`)

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
	SubjectID     string
	Title         string
	Rating        string
	Votes         string
	Director      string
	Writer        string
	Actor         string
	Genre         string
	Country       string
	Language      string
	ReleaseDate   string
	SeasonCount   string
	EpisodeCount  string
	Duration      string
	Aka           string
	IMDb          string
	PosterURL     string
	ShortComments string // 短评数量
	Hotness       string // 计算热度: votes + short_comments + 7天内新片加权
}

// SearchMeta 搜索时的视频元数据，用于智能匹配最佳候选
type SearchMeta struct {
	VodName   string
	Year      string
	VodType   string // 视频分类名（如"动漫"、"电视剧"、"电影"）
	Director  string // 逗号分隔的导演列表
	Actor     string // 逗号分隔的演员列表
}

// SearchCandidate 搜索结果中的单个候选项
type SearchCandidate struct {
	SubjectID string
	Title     string
	Year      int
	IsSeries  bool
	Meta      string // 第一行摘要（国家/类型/时长等）
	Director  string // 逗号分隔
	Actor     string // 逗号分隔
}

// waitRateLimit 在每次请求前调用，确保两次请求之间的间隔落在
// [minRequestInterval, maxRequestInterval] 区间内（随机抖动）。
// 算法：先保证距上次请求至少 minRequestInterval，再叠加一个
// [0, maxRequestInterval-minRequestInterval) 的随机抖动。
func waitRateLimit() {
	rateMu.Lock()
	defer rateMu.Unlock()

	elapsed := time.Since(lastRequestTime)
	// 基础等待：补齐到下限
	if elapsed < minRequestInterval {
		base := minRequestInterval - elapsed
		applog.Debug("[Douban] Rate limiting: base wait %.1fs", base.Seconds())
		time.Sleep(base)
	}
	// 随机抖动：在 [0, max-min) 之间取一个值，避免固定节奏
	jitterRange := maxRequestInterval - minRequestInterval
	if jitterRange > 0 {
		jitter := time.Duration(rand.Int63n(int64(jitterRange)))
		applog.Debug("[Douban] Rate limiting: random jitter %.1fs", jitter.Seconds())
		time.Sleep(jitter)
	}
	lastRequestTime = time.Now()
}

func checkAntiCrawl(html string) bool {
	// 精确反爬检测：真正的反爬/验证页面有明确特征。
	// 之前的"加载中"误判率很高——豆瓣搜索页初始 HTML（smart-box 占位）
	// 确实包含"加载中"，但那不是反爬，而是 JS 占位文本。
	// 改为：只有当页面既包含反爬关键词，又没有任何 subject/tv/movie 链接时，
	// 才判定为反爬（真正反爬页不会带正常结果链接）。
	hasResultLink := subjectIDRegex.MatchString(html) ||
		subjectIDRegexSmart.MatchString(html) ||
		subjectIDRegexJSON.MatchString(html) ||
		moreurlSubjectIDRegex.MatchString(html) ||
		strings.Contains(html, "movie.douban.com/subject/") ||
		strings.Contains(html, "doubanapp/dispatch?uri=/tv/") ||
		strings.Contains(html, "doubanapp/dispatch?uri=/movie/")
	if hasResultLink {
		return false
	}
	for _, pattern := range antiCrawlPatterns {
		if strings.Contains(html, pattern) {
			applog.Warn("[Douban] Anti-crawl detected: pattern '%s' matched (page len=%d)", pattern, len(html))
			return true
		}
	}
	return false
}

// extractAllSearchTitles 从搜索结果 HTML 中提取所有视频标题
func extractAllSearchTitles(html string) []string {
	var titles []string
	seen := make(map[string]bool)
	
	// 从智能搜索结果中提取
	smartMatches := smartTitleRegex.FindAllStringSubmatch(html, -1)
	for _, match := range smartMatches {
		if len(match) >= 2 {
			title := strings.TrimSpace(match[1])
			if title != "" && !seen[title] {
				titles = append(titles, title)
				seen[title] = true
			}
		}
	}
	
	// 从常规搜索结果中提取（title属性格式）
	regularMatches := regularTitleRegex.FindAllStringSubmatch(html, -1)
	for _, match := range regularMatches {
		if len(match) >= 2 {
			title := strings.TrimSpace(match[1])
			if title != "" && !seen[title] {
				titles = append(titles, title)
				seen[title] = true
			}
		}
	}
	
	// 从常规搜索结果中提取（title-text文本格式）
	textMatches := titleTextRegex.FindAllStringSubmatch(html, -1)
	for _, match := range textMatches {
		if len(match) >= 2 {
			title := strings.TrimSpace(match[1])
			if title != "" && !seen[title] {
				titles = append(titles, title)
				seen[title] = true
			}
		}
	}
	
	return titles
}

// normalizeTitle 标准化标题用于比较（去除空格、季数后缀等）
func normalizeTitle(title string) string {
	title = cleanTitle(title)
	title = strings.ToLower(title)
	return title
}

// cleanTitle 去除标题中的不可见字符和年份后缀
func cleanTitle(title string) string {
	// 去除不可见 Unicode 字符（LTR mark、RTL mark 等）
	title = strings.Map(func(r rune) rune {
		if r == '\u200E' || r == '\u200F' || r == '\u200B' || r == '\uFEFF' {
			return -1 // 删除
		}
		return r
	}, title)
	// 去除年份后缀
	title = searchYearRegex.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	return title
}

// splitItemBlocks 将搜索结果 HTML 拆分为独立的候选项块
func splitItemBlocks(html string) []string {
	marker := `<div class="item-root">`
	parts := strings.Split(html, marker)
	var blocks []string
	for i := 1; i < len(parts); i++ {
		blocks = append(blocks, parts[i])
	}
	return blocks
}

// parseSearchCandidates 解析搜索结果 HTML，提取所有候选项及其元数据
func parseSearchCandidates(html string) []SearchCandidate {
	blocks := splitItemBlocks(html)
	if len(blocks) == 0 {
		return nil
	}

	var candidates []SearchCandidate
	for _, block := range blocks {
		// 提取 subject ID（从 data-moreurl 的 JS 参数中）
		idMatch := moreurlSubjectIDRegex.FindStringSubmatch(block)
		if len(idMatch) < 2 {
			continue
		}

		// 提取标题
		titleMatch := searchTitleTextRegex.FindStringSubmatch(block)
		if len(titleMatch) < 2 {
			continue
		}
		rawTitle := titleMatch[1]

		// 提取年份
		year := 0
		if ym := searchYearRegex.FindStringSubmatch(rawTitle); len(ym) >= 2 {
			year, _ = strconv.Atoi(ym[1])
		}
		title := cleanTitle(rawTitle)

		// 提取所有 meta abstract 内容
		metaMatches := searchMetaRegex.FindAllStringSubmatch(block, -1)
		var meta1, meta2 string
		if len(metaMatches) >= 1 {
			meta1 = strings.TrimSpace(metaMatches[0][1])
		}
		if len(metaMatches) >= 2 {
			meta2 = strings.TrimSpace(metaMatches[1][1])
		}

		// 判断是否为剧集
		isSeries := strings.Contains(block, `[剧集]`) || strings.Contains(block, `is_tv:'1'`)

		// 解析导演/演员（meta2 中导演在前、演员在后，用 / 分隔）
		director, actor := parseDirectorActor(meta2)

		candidates = append(candidates, SearchCandidate{
			SubjectID: idMatch[1],
			Title:     title,
			Year:      year,
			IsSeries:  isSeries,
			Meta:      meta1,
			Director:  director,
			Actor:     actor,
		})
	}
	return candidates
}

// parseDirectorActor 从 meta2 字符串中分离导演和演员
// 豆瓣搜索结果 meta2 格式：导演 / 导演 / 演员 / 演员 / ...
// 第一个 / 之前的部分是导演，其余是演员
func parseDirectorActor(meta2 string) (string, string) {
	if meta2 == "" {
		return "", ""
	}
	parts := strings.Split(meta2, "/")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// 豆瓣搜索页 meta2 中导演通常在前，用第一个非空分隔
	// 实际观察：导演有1-2个，后面全是演员
	// 简单策略：前2个为导演，其余为演员（如果总数<=2则全是导演）
	if len(parts) <= 2 {
		return strings.Join(parts, ","), ""
	}
	// 取前2个为导演，其余为演员
	return strings.Join(parts[:2], ","), strings.Join(parts[2:], ",")
}

// scoreCandidate 计算候选项与搜索元数据的匹配分数
// 注意：豆瓣的「剧集/电影」标签不属于类型，类型比较由 global_types 层负责
func scoreCandidate(c *SearchCandidate, meta SearchMeta) int {
	score := 0

	normTitle := normalizeTitle(c.Title)
	normKeyword := normalizeTitle(meta.VodName)

	// 标题匹配（最高权重）
	if normTitle == normKeyword {
		score += 100
	} else if strings.Contains(normTitle, normKeyword) || strings.Contains(normKeyword, normTitle) {
		score += 50
	}

	// 年份匹配
	if meta.Year != "" && c.Year > 0 {
		if strconv.Itoa(c.Year) == meta.Year {
			score += 30
		} else {
			score -= 15 // 年份不匹配扣分
		}
	}

	// 导演匹配（豆瓣用 / 分隔，本地数据可能用 , 或 、 分隔）
	if meta.Director != "" && c.Director != "" {
		metaDirectors := splitCSV(meta.Director)
		cDirectors := splitCSV(c.Director)
		for _, md := range metaDirectors {
			for _, cd := range cDirectors {
				if md != "" && cd != "" && md == cd {
					score += 20
				}
			}
		}
	}

	// 演员匹配（最多 +15 分）
	if meta.Actor != "" && c.Actor != "" {
		metaActors := splitCSV(meta.Actor)
		cActors := splitCSV(c.Actor)
		actorScore := 0
		for _, ma := range metaActors {
			for _, ca := range cActors {
				if ma != "" && ca != "" && ma == ca {
					actorScore += 5
					if actorScore >= 15 {
						break
					}
				}
			}
			if actorScore >= 15 {
				break
			}
		}
		score += actorScore
	}

	return score
}

// splitCSV 将各种分隔符（逗号、斜杠、顿号、全角逗号等）拆分为列表
func splitCSV(s string) []string {
	// 统一分隔符：将 / 、 ， 全部替换为英文逗号
	s = strings.ReplaceAll(s, "/", ",")
	s = strings.ReplaceAll(s, "、", ",")
	s = strings.ReplaceAll(s, "\uff0c", ",") // 全角逗号
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// bestMatch 从候选列表中选择最佳匹配
func bestMatch(candidates []SearchCandidate, meta SearchMeta) (*SearchCandidate, int) {
	if len(candidates) == 0 {
		return nil, 0
	}
	best := &candidates[0]
	bestScore := scoreCandidate(&candidates[0], meta)
	for i := 1; i < len(candidates); i++ {
		s := scoreCandidate(&candidates[i], meta)
		if s > bestScore {
			bestScore = s
			best = &candidates[i]
		}
	}
	return best, bestScore
}

// isTitleMatch 检查搜索结果标题列表中是否有任何一个与原始关键词匹配
// 返回 true 表示匹配，false 表示不匹配
func isTitleMatch(searchTitles []string, originalKeyword string) bool {
	if len(searchTitles) == 0 || originalKeyword == "" {
		return false
	}
	
	normalizedKeyword := normalizeTitle(originalKeyword)
	
	for _, title := range searchTitles {
		normalizedSearch := normalizeTitle(title)
		
		// 完全匹配
		if normalizedSearch == normalizedKeyword {
			return true
		}
		
		// 搜索结果包含关键词（处理"万界独尊 第一季"包含"万界独尊"的情况）
		if strings.Contains(normalizedSearch, normalizedKeyword) {
			return true
		}
		
		// 关键词包含搜索结果（处理"万界独尊"包含"万界独尊 第一季"的情况）
		if strings.Contains(normalizedKeyword, normalizedSearch) {
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

// stripSeasonInfo 移除关键词中的季数/部数信息，提高豆瓣搜索命中率。
// 例如 "权力的游戏 第八季" → "权力的游戏"
func stripSeasonInfo(keyword string) string {
	s := seasonStripRegex.ReplaceAllString(keyword, "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "")
	return s
}

func SearchSubjectID(keyword string, meta SearchMeta) (string, error) {
	// 检查是否在搜索冷却期
	if db.IsDoubanOnCooldown(keyword) {
		applog.Debug("[Douban] Skipping search for '%s' (in cooldown period)", keyword)
		return "", fmt.Errorf("search cooldown active for '%s'", keyword)
	}

	// 多层搜索策略：先用原始关键词，失败后尝试去掉季数信息
	keywords := []string{keyword}
	stripped := stripSeasonInfo(keyword)
	if stripped != "" && stripped != keyword {
		keywords = append(keywords, stripped)
	}

	var lastErr error
	for _, kw := range keywords {
		applog.Info("[Douban] Searching for keyword: %s (meta: year=%s type=%s director=%s)",
			kw, meta.Year, meta.VodType, meta.Director)

		params := url.Values{}
		params.Set("search_text", kw)
		params.Set("cat", "1002")

		fullURL := searchURL + "?" + params.Encode()

		html, err := fetchHTML(fullURL)
		if err != nil {
			applog.Error("[Douban] Search fetch failed for '%s': %v", kw, err)
			lastErr = err
			continue
		}

		if checkAntiCrawl(html) {
			applog.Warn("[Douban] Anti-crawl triggered for '%s' (HTML len=%d)", kw, len(html))
			lastErr = fmt.Errorf("anti-crawl detected")
			continue
		}

		// 解析所有候选结果
		candidates := parseSearchCandidates(html)

		if len(candidates) == 0 {
			// 兆底：尝试旧版正则提取
			fallbackIDs := subjectIDRegex.FindAllStringSubmatch(html, 3)
			if len(fallbackIDs) > 0 {
				applog.Info("[Douban] New parser found 0 candidates, fallback regex found %d IDs for '%s'", len(fallbackIDs), kw)
				db.ClearSearchFailures(keyword)
				return fallbackIDs[0][1], nil
			}
			applog.Warn("[Douban] No candidates parsed for '%s' (HTML len=%d)", kw, len(html))
			if len(html) > 500 {
				snippet := html[:500]
				if len(html) > 500 {
					snippet += "..."
				}
				applog.Debug("[Douban] HTML snippet: %s", snippet)
			}
			lastErr = fmt.Errorf("no candidates parsed for keyword: %s", kw)
			continue
		}

		// 打印所有候选项（诊断日志）
		for i, c := range candidates {
			applog.Info("[Douban] Candidate[%d]: id=%s title=%q year=%d series=%v director=%s actor=%s",
				i, c.SubjectID, c.Title, c.Year, c.IsSeries, c.Director, c.Actor)
		}

		// 智能匹配：根据元数据评分选择最佳候选
		match, score := bestMatch(candidates, meta)
		if match != nil {
			applog.Info("[Douban] Best match for '%s': id=%s title=%q year=%d score=%d",
				keyword, match.SubjectID, match.Title, match.Year, score)

			// 分数 >= 40 表示有足够的匹配度
			if score >= 40 {
				db.ClearSearchFailures(keyword)
				return match.SubjectID, nil
			}

			// 分数低但仍选择最佳候选（避免永远失败）
			applog.Warn("[Douban] Low confidence match for '%s': best score=%d (title=%q), accepting anyway",
				keyword, score, match.Title)
			db.ClearSearchFailures(keyword)
			return match.SubjectID, nil
		}

		lastErr = fmt.Errorf("no subject ID found for keyword: %s", kw)
	}

	// 所有搜索策略都失败
	db.IncrementSearchFailures(keyword)
	return "", lastErr
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

	// 解析短评数量
	if matches := shortCommentsRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.ShortComments = strings.ReplaceAll(strings.TrimSpace(matches[1]), ",", "")
	}
	// 短评数量备选：从 "全部 XX 条短评" 格式提取
	if info.ShortComments == "" {
		if matches := regexp.MustCompile(`全部\s*(\d[\d,]*)\s*条`).FindStringSubmatch(html); len(matches) >= 2 {
			info.ShortComments = strings.ReplaceAll(strings.TrimSpace(matches[1]), ",", "")
		}
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

	// 兜底标题提取：从 <h1> 中的 <span property="v:itemreviewed"> 提取
	if info.Title == "" {
		if matches := titleRegex.FindStringSubmatch(html); len(matches) >= 2 {
			info.Title = strings.TrimSpace(matches[1])
		}
	}

	// 兜底解析：如果以上正则都没匹配到关键字段，尝试从 <div id="info"> 中逐行解析
	parseInfoDivFallback(html, info)

	// 计算热度：votes + short_comments + 7天内新片加权
	info.Hotness = computeHotness(info.Votes, info.ShortComments, info.ReleaseDate)

	applog.Info("[Douban] Parsed detail for %s: Title='%s', Rating='%s', Votes='%s', ShortComments='%s', Hotness='%s', Director='%s', Actor='%s', Genre='%s'",
		subjectID, info.Title, info.Rating, info.Votes, info.ShortComments, info.Hotness, truncate(info.Director, 30), truncate(info.Actor, 30), info.Genre)

	return info, nil
}

// computeHotness 计算热度分数
// 公式：votes + short_comments_count + 7天内新片加杈100 + 30天内新片加权50
func computeHotness(votesStr, shortCommentsStr, releaseDateStr string) string {
	hotness := 0
	if v, err := strconv.Atoi(votesStr); err == nil {
		hotness += v
	}
	if sc, err := strconv.Atoi(shortCommentsStr); err == nil {
		hotness += sc
	}
	// 时间加权：解析首播日期判断是否为新片
	if releaseDateStr != "" {
		// 尝试解析日期：格式如 "2026-06-20(中国大陆)" 或 "2026-05-15"
		dateStr := releaseDateStr
		// 取第一个日期
		if idx := strings.Index(dateStr, "("); idx > 0 {
			dateStr = dateStr[:idx]
		}
		dateStr = strings.TrimSpace(dateStr)
		// 尝试多种日期格式
		for _, layout := range []string{"2006-01-02", "2006/01/02", "2006.01.02"} {
			if t, err := time.Parse(layout, dateStr); err == nil {
				days := time.Since(t).Hours() / 24
				if days < 7 {
					hotness += 100
				} else if days < 30 {
					hotness += 50
				}
				break
			}
		}
	}
	return strconv.Itoa(hotness)
}

// parseInfoDivFallback 从 <div id="info"> 中逐行解析，处理嵌套 span 标签等复杂结构
func parseInfoDivFallback(html string, info *DoubanInfo) {
	idx := strings.Index(html, `<div id="info">`)
	if idx < 0 {
		return
	}
	// 找到 info div 的结束位置
	endMarkers := []string{`<div id="interest_sectl">`, `<script type="text/javascript">`}
	endIdx := len(html)
	for _, marker := range endMarkers {
		if i := strings.Index(html[idx:], marker); i > 0 && idx+i < endIdx {
			endIdx = idx + i
		}
	}
	block := html[idx:endIdx]

	// 按 <span class="pl"> 分割，逐个处理每个标签-值对
	plPattern := regexp.MustCompile(`<span class="pl">([^<]+)</span>`)
	plMatches := plPattern.FindAllStringSubmatchIndex(block, -1)

	for i, m := range plMatches {
		if len(m) < 4 {
			continue
		}
		label := strings.TrimSpace(block[m[2]:m[3]])
		// 值的起始位置：当前 span 结束之后
		valueStart := m[1]
		// 值的结束位置：下一个 <span class="pl"> 或 <br> 或下一个 <span 
		var valueEnd int
		if i+1 < len(plMatches) {
			valueEnd = plMatches[i+1][0]
		} else {
			valueEnd = len(block)
		}
		rawValue := block[valueStart:valueEnd]

		// 截断到第一个 <br> 或 <script 标签
		if brIdx := strings.Index(rawValue, "<br"); brIdx >= 0 {
			rawValue = rawValue[:brIdx]
		}
		if scriptIdx := strings.Index(rawValue, "<script"); scriptIdx >= 0 {
			rawValue = rawValue[:scriptIdx]
		}

		// 剥离所有 HTML 标签，提取纯文本值
		value := stripHTMLTags(rawValue)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		switch {
		case strings.Contains(label, "导演") && info.Director == "":
			info.Director = value
		case strings.Contains(label, "编剧") && info.Writer == "":
			info.Writer = value
		case strings.Contains(label, "主演") && info.Actor == "":
			info.Actor = value
		case strings.Contains(label, "类型") && info.Genre == "":
			info.Genre = value
		case strings.Contains(label, "制片国家/地区") && info.Country == "":
			info.Country = value
		case strings.Contains(label, "语言") && info.Language == "":
			info.Language = value
		case strings.Contains(label, "首播") && info.ReleaseDate == "":
			info.ReleaseDate = value
		case strings.Contains(label, "集数") && info.EpisodeCount == "":
			info.EpisodeCount = value
		case strings.Contains(label, "季数") && info.SeasonCount == "":
			info.SeasonCount = value
		case strings.Contains(label, "单集片长") && info.Duration == "":
			info.Duration = value
		case strings.Contains(label, "又名") && info.Aka == "":
			info.Aka = value
		case strings.Contains(label, "IMDb") && info.IMDb == "":
			info.IMDb = value
		}
	}
}

// stripHTMLTags 去除字符串中的所有 HTML 标签，返回纯文本
func stripHTMLTags(s string) string {
	// 移除所有 <...> 标签
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	result := tagRegex.ReplaceAllString(s, "")
	// 将多个空白字符压缩为一个空格
	spaceRegex := regexp.MustCompile(`\s+`)
	result = spaceRegex.ReplaceAllString(result, " ")
	return strings.TrimSpace(result)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func FetchDoubanInfo(keyword string, meta SearchMeta) (*DoubanInfo, error) {
	if db.IsDoubanOnCooldown(keyword) {
		applog.Debug("[Douban] Skipping fetch for '%s' (in cooldown period)", keyword)
		return nil, fmt.Errorf("search cooldown active for '%s'", keyword)
	}

	applog.Info("[Douban] Fetching complete info for keyword: %s", keyword)

	subjectID, err := SearchSubjectID(keyword, meta)
	if err != nil {
		applog.Error("[Douban] FetchDoubanInfo failed at search step for '%s': %v", keyword, err)
		return nil, err
	}

	return ParseDetail(subjectID)
}
