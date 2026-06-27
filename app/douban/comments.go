package douban

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
)

// DoubanComment 豆瓣评论结构
type DoubanComment struct {
	ID          string `json:"id"`           // 评论 ID (data-cid)
	Avatar      string `json:"avatar"`       // 用户头像 URL
	Username    string `json:"username"`     // 用户名
	Profile     string `json:"profile"`      // 豆瓣主页链接
	Status      string `json:"status"`       // "看过" / "想看"
	Rating      int    `json:"rating"`       // 1-5 星
	RatingTitle string `json:"rating_title"` // "力荐"/"推荐"/"还行"/"较差"/"很差"
	Time        string `json:"time"`         // "2011-06-22 13:07:28"
	Location    string `json:"location"`     // 用户所在地
	Votes       int    `json:"votes"`        // "有用"票数
	Content     string `json:"content"`      // 评论内容
}

// DoubanCommentsResp 评论列表响应
type DoubanCommentsResp struct {
	Comments   []DoubanComment `json:"comments"`
	Total      int             `json:"total"`       // 评论总数（估算）
	Page       int             `json:"page"`        // 当前页码
	TotalPages int             `json:"total_pages"` // 总页数
}

// 评论缓存结构
type commentCacheEntry struct {
	data      *DoubanCommentsResp
	fetchedAt time.Time
}

var (
	commentsCache = struct {
		sync.RWMutex
		entries map[string]commentCacheEntry
	}{entries: make(map[string]commentCacheEntry)}

	cacheTTL = 24 * time.Hour // 24小时缓存

	// 评论请求独立速率限制（比爬虫短得多，因为是用户交互操作）
	commentMinInterval = 1 * time.Second
	commentMaxInterval = 3 * time.Second
	lastCommentTime    time.Time
	commentRateMu      sync.Mutex

	// 评论页 HTML 解析正则
	commentItemRegex = regexp.MustCompile(`<div class="comment-item"[^>]*data-cid="(\d+)"[\s\S]*?</div>\s*</div>`)
	avatarRegex      = regexp.MustCompile(`<div class="avatar">\s*<a[^>]*href="([^"]*)"[^>]*>\s*<img src="([^"]*)"`)
	usernameRegex    = regexp.MustCompile(`<span class="comment-info">\s*<a[^>]*href="([^"]*)"[^>]*>([^<]+)</a>`)
	ratingClassRegex = regexp.MustCompile(`<span class="allstar(\d+) rating" title="([^"]*)"`)
	statusRegex      = regexp.MustCompile(`<span class="comment-info">[\s\S]*?<span>([^<]+)</span>`)
	commentTimeRegex = regexp.MustCompile(`<a class="comment-time"[^>]*title="([^"]*)"`)
	locationRegex    = regexp.MustCompile(`<span class="comment-location">([^<]*)</span>`)
	voteCountRegex   = regexp.MustCompile(`<span class="votes vote-count">(\d+)</span>`)
	commentTextRegex = regexp.MustCompile(`<span class="short">([\s\S]*?)</span>`)

	// 分页信息正则
	paginatorRegex = regexp.MustCompile(`<div id="paginator"[\s\S]*?</div>`)
	nextPageRegex  = regexp.MustCompile(`start=(\d+)`)

	// 评论总数估算正则（从分页器提取）
	totalCommentsHintRegex = regexp.MustCompile(`(\d+)\s*条`)
)

// FetchComments 获取豆瓣评论（带 24h 缓存）
func FetchComments(doubanID string, page int, sort string) (*DoubanCommentsResp, error) {
	if doubanID == "" {
		return nil, fmt.Errorf("douban_id 不能为空")
	}
	if page < 1 {
		page = 1
	}
	if sort == "" {
		sort = "new_score"
	}

	// 检查缓存
	cacheKey := fmt.Sprintf("%s_%d_%s", doubanID, page, sort)
	commentsCache.RLock()
	if entry, ok := commentsCache.entries[cacheKey]; ok {
		if time.Since(entry.fetchedAt) < cacheTTL {
			commentsCache.RUnlock()
			applog.Info("[DoubanComments] 缓存命中: %s", cacheKey)
			return entry.data, nil
		}
	}
	commentsCache.RUnlock()

	// 缓存未命中，抓取数据
	resp, err := fetchCommentsFromWeb(doubanID, page, sort)
	if err != nil {
		return nil, err
	}

	// 存入缓存
	commentsCache.Lock()
	commentsCache.entries[cacheKey] = commentCacheEntry{
		data:      resp,
		fetchedAt: time.Now(),
	}
	commentsCache.Unlock()

	return resp, nil
}

// fetchCommentsFromWeb 从豆瓣网页抓取评论
func fetchCommentsFromWeb(doubanID string, page int, sort string) (*DoubanCommentsResp, error) {
	offset := (page - 1) * 20
	url := fmt.Sprintf("https://movie.douban.com/subject/%s/comments?start=%d&limit=20&status=P&sort=%s",
		doubanID, offset, sort)

	applog.Info("[DoubanComments] 抓取评论: doubanID=%s, page=%d, url=%s", doubanID, page, url)

	// 评论请求独立速率限制（3~8秒，不影响爬虫的全局限制）
	waitCommentRateLimit()

	// 构造请求
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", fmt.Sprintf("https://movie.douban.com/subject/%s/", doubanID))
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	// 发送请求
	httpResp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP 状态码: %d", httpResp.StatusCode)
	}

	body, err := ioutil.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	html := string(body)

	// 解析评论
	comments := parseComments(html)

	// 解析分页信息
	totalPages := parseTotalPages(html, page)

	// 估算总数（从分页器或评论数推算）
	total := totalPages * 20

	resp := &DoubanCommentsResp{
		Comments:   comments,
		Total:      total,
		Page:       page,
		TotalPages: totalPages,
	}

	applog.Info("[DoubanComments] 抓取完成: doubanID=%s, page=%d, comments=%d, totalPages=%d",
		doubanID, page, len(comments), totalPages)

	return resp, nil
}

// parseComments 解析评论列表
func parseComments(html string) []DoubanComment {
	var comments []DoubanComment

	// 匹配所有评论项
	items := commentItemRegex.FindAllStringSubmatch(html, -1)

	for _, item := range items {
		if len(item) < 2 {
			continue
		}
		commentHTML := item[0]
		commentID := item[1]

		comment := DoubanComment{
			ID: commentID,
		}

		// 解析头像和主页
		if match := avatarRegex.FindStringSubmatch(commentHTML); len(match) >= 3 {
			comment.Profile = match[1]
			comment.Avatar = match[2]
		}

		// 解析用户名
		if match := usernameRegex.FindStringSubmatch(commentHTML); len(match) >= 3 {
			comment.Profile = match[1]
			comment.Username = strings.TrimSpace(match[2])
		}

		// 解析评分
		if match := ratingClassRegex.FindStringSubmatch(commentHTML); len(match) >= 3 {
			stars, _ := strconv.Atoi(match[1])
			comment.Rating = stars / 10 // allstar50 -> 5
			comment.RatingTitle = match[2]
		}

		// 解析状态（"看过"/"想看"）
		if match := statusRegex.FindStringSubmatch(commentHTML); len(match) >= 2 {
			comment.Status = strings.TrimSpace(match[1])
		}

		// 解析时间
		if match := commentTimeRegex.FindStringSubmatch(commentHTML); len(match) >= 2 {
			comment.Time = strings.TrimSpace(match[1])
		}

		// 解析位置
		if match := locationRegex.FindStringSubmatch(commentHTML); len(match) >= 2 {
			comment.Location = strings.TrimSpace(match[1])
		}

		// 解析投票数
		if match := voteCountRegex.FindStringSubmatch(commentHTML); len(match) >= 2 {
			votes, _ := strconv.Atoi(match[1])
			comment.Votes = votes
		}

		// 解析评论内容
		if match := commentTextRegex.FindStringSubmatch(commentHTML); len(match) >= 2 {
			comment.Content = strings.TrimSpace(match[1])
		}

		comments = append(comments, comment)
	}

	return comments
}

// parseTotalPages 解析总页数
func parseTotalPages(html string, currentPage int) int {
	// 查找分页器
	paginatorMatch := paginatorRegex.FindString(html)
	if paginatorMatch == "" {
		// 没有分页器，可能只有一页
		return 1
	}

	// 从分页器中提取下一页的 start 值
	nextPageMatch := nextPageRegex.FindAllStringSubmatch(paginatorMatch, -1)
	if len(nextPageMatch) == 0 {
		return currentPage
	}

	// 找最大的 start 值（最后一页的起始位置）
	maxStart := 0
	for _, match := range nextPageMatch {
		if len(match) >= 2 {
			start, _ := strconv.Atoi(match[1])
			if start > maxStart {
				maxStart = start
			}
		}
	}

	// 每页 20 条，计算总页数
	totalPages := (maxStart / 20) + 1
	if totalPages < currentPage {
		totalPages = currentPage
	}

	return totalPages
}

// waitCommentRateLimit 评论请求独立速率限制（3~8秒随机间隔）
// 不影响爬虫的全局 waitRateLimit，避免用户交互操作等待过久。
func waitCommentRateLimit() {
	commentRateMu.Lock()
	defer commentRateMu.Unlock()

	elapsed := time.Since(lastCommentTime)
	if elapsed < commentMinInterval {
		base := commentMinInterval - elapsed
		applog.Debug("[DoubanComments] Rate limiting: base wait %.1fs", base.Seconds())
		time.Sleep(base)
	}
	jitterRange := commentMaxInterval - commentMinInterval
	if jitterRange > 0 {
		jitter := time.Duration(rand.Int63n(int64(jitterRange)))
		applog.Debug("[DoubanComments] Rate limiting: jitter %.1fs", jitter.Seconds())
		time.Sleep(jitter)
	}
	lastCommentTime = time.Now()
}

// ClearCommentsCache 清除评论缓存（可选，用于调试）
func ClearCommentsCache() {
	commentsCache.Lock()
	commentsCache.entries = make(map[string]commentCacheEntry)
	commentsCache.Unlock()
	applog.Info("[DoubanComments] 缓存已清除")
}
