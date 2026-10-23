package douban

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
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

	// commentStaleCeiling 是「过期的评论页还值得继续展示多久」。超过 24h 不代表内容
	// 不能看，只代表该换个新的了；在此之前一律先把旧数据交给界面，另起一次后台刷新。
	// 只有连这个上限都过了（或首次访问）才会让人等网络。
	commentStaleCeiling = 7 * 24 * time.Hour

	// commentRefreshBackoff 是一次后台刷新失败后的退避。没有它的话，每次打开详情页
	// 都会再撞一次豆瓣闸门——失败了还反复重试，等于给限速添乱。
	commentRefreshBackoff = 30 * time.Minute

	// 每条缓存是一整页评论（含正文），长时间翻页只增不减会一直占着内存。
	maxCommentCacheEntries = 200

	commentSubjectIDRegex = regexp.MustCompile(`^[0-9]{1,16}$`)

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

// commentRefresh 给后台静默刷新去重：running 保证同一页同时只有一在飞，
// blocked 记下失败退避到什么时候。两者都只活几小时，随缓存条目淘汰一起收掉。
var commentRefresh = struct {
	sync.Mutex
	running map[string]bool
	blocked map[string]time.Time
}{
	running: make(map[string]bool),
	blocked: make(map[string]time.Time),
}

// FetchComments 获取豆瓣评论：24 小时内的缓存直接用，过期的先返回旧数据再后台刷新，
// 内存未命中先读持久快照，只有两者都不可用时才等待网络。
func FetchComments(doubanID string, page int, sort string) (*DoubanCommentsResp, error) {
	return FetchCommentsContext(context.Background(), doubanID, page, sort)
}

// FetchCommentsContext preserves cache semantics while making cold reads cancellable.
func FetchCommentsContext(ctx context.Context, doubanID string, page int, sort string) (*DoubanCommentsResp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !commentSubjectIDRegex.MatchString(doubanID) {
		return nil, apperror.New(apperror.Validation, "douban_id 必须是数字")
	}
	if page < 1 || page > 10000 {
		page = 1
	}
	if sort != "time" && sort != "new_score" {
		sort = "new_score"
	}

	cacheKey := fmt.Sprintf("%s_%d_%s", doubanID, page, sort)
	commentsCache.RLock()
	entry, has := commentsCache.entries[cacheKey]
	commentsCache.RUnlock()

	if !has {
		loadCommentSnapshots()
		commentsCache.RLock()
		entry, has = commentsCache.entries[cacheKey]
		commentsCache.RUnlock()
	}

	if has && entry.data != nil {
		switch age := time.Since(entry.fetchedAt); {
		case age < cacheTTL:
			commentsCounters.hits.Add(1)
			applog.Info("[DoubanComments] 缓存命中: %s", cacheKey)
			return entry.data, nil
		case age < commentStaleCeiling:
			// 已经能看了，所以不必等：旧评论先上屏，后台悄悄换新的，
			// 下次进来（或前端重新拉取）才看到变化。
			commentsCounters.staleHits.Add(1)
			applog.Info("[DoubanComments] 返回过期缓存并安排后台刷新: %s (已 %s)", cacheKey, age.Round(time.Hour))
			startCommentRefresh(cacheKey, doubanID, page, sort)
			return entry.data, nil
		}
	}

	commentsCounters.misses.Add(1)
	return requestCommentsPage(ctx, cacheKey, doubanID, page, sort)
}

// fetchCommentsPage 是「抓一页评论」的替身位，理由同热榜的 fetchChartPage。
var fetchCommentsPage = fetchCommentsFromWeb

// startCommentRefresh 后台刷新一页评论。抢不到豆瓣闸门时 fetchCommentsFromWeb 自己
// 会快速失败（批量模式间隔远大于交互等待预算），这里只负责不去重复占坑。
func startCommentRefresh(cacheKey, doubanID string, page int, sort string) {
	commentRefresh.Lock()
	if commentRefresh.running[cacheKey] {
		commentsCounters.skipped.Add(1)
		commentRefresh.Unlock()
		return
	}
	if until, ok := commentRefresh.blocked[cacheKey]; ok && time.Now().Before(until) {
		commentsCounters.skipped.Add(1)
		commentRefresh.Unlock()
		applog.Debug("[DoubanComments] %s 在退避期内，跳过后台刷新", cacheKey)
		return
	}
	commentRefresh.running[cacheKey] = true
	commentRefresh.Unlock()

	accepted := launchCommentRefresh(func(ctx context.Context) {
		resp, err := requestCommentsPage(ctx, cacheKey, doubanID, page, sort)

		commentRefresh.Lock()
		delete(commentRefresh.running, cacheKey)
		if err != nil && apperror.CodeOf(err) != apperror.Cancelled {
			commentRefresh.blocked[cacheKey] = time.Now().Add(commentRefreshBackoff)
		} else {
			delete(commentRefresh.blocked, cacheKey)
		}
		pruneCommentRefreshStateLocked()
		commentRefresh.Unlock()

		if err != nil {
			if apperror.CodeOf(err) == apperror.Cancelled {
				return
			}
			// 失败就继续展示旧数据，但不给旧数据续期：fetchedAt 一旦刷新，过期上限
			// 就被无限推后，一个早已抓不到的页面会永远停留在「静默失败」。退避
			// 已经由 blocked 管住（30 分钟内不再撞闸门），这里什么都不用做。
			applog.Warn("[DoubanComments] 后台刷新失败，继续展示旧数据: %v", err)
			return
		}
		applog.Info("[DoubanComments] 后台刷新完成: %s (%d 条)", cacheKey, len(resp.Comments))
	})
	if !accepted {
		commentRefresh.Lock()
		delete(commentRefresh.running, cacheKey)
		commentRefresh.Unlock()
	}
}

// storeCommentsPage 写入一页评论并维持容量上限。
func storeCommentsPage(cacheKey string, resp *DoubanCommentsResp) {
	commentsCache.Lock()
	commentsCache.entries[cacheKey] = commentCacheEntry{data: resp, fetchedAt: time.Now()}
	evictOldestCommentsLocked()
	commentsCache.Unlock()
	saveCommentSnapshots()
}

// pruneCommentRefreshStateLocked 收掉退避到期的键；调用方需持有 commentRefresh 锁。
// running 里的键总会在 goroutine 结束时删掉，所以只有 blocked 会长期增长。
func pruneCommentRefreshStateLocked() {
	if len(commentRefresh.blocked) <= maxCommentCacheEntries {
		return
	}
	now := time.Now()
	for key, until := range commentRefresh.blocked {
		if now.After(until) {
			delete(commentRefresh.blocked, key)
		}
	}
	for len(commentRefresh.blocked) > maxCommentCacheEntries {
		var oldest string
		var at time.Time
		for key, until := range commentRefresh.blocked {
			if oldest == "" || until.Before(at) {
				oldest, at = key, until
			}
		}
		delete(commentRefresh.blocked, oldest)
	}
}

// evictOldestCommentsLocked 超出容量时淘汰最早抓取的一页；调用方需持有 commentsCache 写锁。
// 过期项的 fetchedAt 同样最旧，所以淘汰顺带把 24h 之外的条目清掉。
func evictOldestCommentsLocked() {
	for len(commentsCache.entries) > maxCommentCacheEntries {
		var oldest string
		var at time.Time
		for key, entry := range commentsCache.entries {
			if oldest == "" || entry.fetchedAt.Before(at) {
				oldest, at = key, entry.fetchedAt
			}
		}
		delete(commentsCache.entries, oldest)
	}
}

// fetchCommentsFromWeb 从豆瓣网页抓取评论
func fetchCommentsFromWeb(ctx context.Context, doubanID string, page int, sort string) (*DoubanCommentsResp, error) {
	offset := (page - 1) * 20
	url := fmt.Sprintf("https://movie.douban.com/subject/%s/comments?start=%d&limit=20&status=P&sort=%s",
		doubanID, offset, sort)

	applog.Info("[DoubanComments] 抓取评论: doubanID=%s, page=%d, url=%s", doubanID, page, url)

	html, err := fetchInteractiveDoubanContext(ctx, url, fmt.Sprintf("https://movie.douban.com/subject/%s/", doubanID), "评论")
	if err != nil {
		return nil, err
	}
	// A login/error/redesigned page is not a valid empty comments page.
	if !strings.Contains(html, `id="comments"`) && !strings.Contains(html, `class="comment-item"`) {
		return nil, apperror.New(apperror.Unavailable, "豆瓣评论页面暂不可用")
	}

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

// ClearCommentsCache 清除评论缓存（可选，用于调试）
func ClearCommentsCache() {
	commentsCache.Lock()
	commentsCache.entries = make(map[string]commentCacheEntry)
	commentsCache.Unlock()
	clearCommentSnapshots()
	applog.Info("[DoubanComments] 缓存已清除")
}
