package douban

import (
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/applog"
)

// ==================== 反爬熔断 ====================
// 拿到 sec.douban.com 验证跳转说明这个 IP 已经被临时封禁，继续按原节奏重试
// 只会不断续封。命中后进入静默期，静默期内所有豆瓣请求直接快速失败（不排队等待），
// 静默时长按 5/15/60 分钟递增，任何一次正常响应立即解除。
var (
	blockMu      sync.Mutex
	blockUntil   time.Time
	blockStrikes int
)

var blockBackoffSteps = []time.Duration{
	5 * time.Minute,
	15 * time.Minute,
	time.Hour,
}

// remainingBlock 返回仍在静默期内的剩余时长；不在静默期返回 0。
// 顺带把到期的静默期清零，避免下一次 noteDoubanSuccess 之前一直误判。
func remainingBlock() time.Duration {
	blockMu.Lock()
	defer blockMu.Unlock()
	if blockUntil.IsZero() {
		return 0
	}
	if left := time.Until(blockUntil); left > 0 {
		return left
	}
	blockUntil = time.Time{}
	blockStrikes = 0
	return 0
}

// AntiCrawlState 供诊断页只读查看熔断静默期：剩余时长与连续命中次数。
// 与 remainingBlock 不同，它不会顺手清掉到期状态，读一次不会改变抓取行为。
func AntiCrawlState() (time.Duration, int) {
	blockMu.Lock()
	defer blockMu.Unlock()
	if blockUntil.IsZero() {
		return 0, blockStrikes
	}
	if left := time.Until(blockUntil); left > 0 {
		return left, blockStrikes
	}
	return 0, blockStrikes
}

// noteAntiCrawl 记录一次反爬命中并把静默期推高一档。
func noteAntiCrawl() {
	blockMu.Lock()
	step := blockStrikes
	if step >= len(blockBackoffSteps) {
		step = len(blockBackoffSteps) - 1
	}
	silent := blockBackoffSteps[step]
	blockStrikes++
	strikes := blockStrikes
	blockUntil = time.Now().Add(silent)
	blockMu.Unlock()
	applog.Warn("[Douban] 触发反爬熔断：静默 %s 后再尝试（连续命中 %d 次）", silent, strikes)
}

// noteDoubanSuccess 在拿到正常响应后解除熔断。
func noteDoubanSuccess() {
	blockMu.Lock()
	had := blockStrikes > 0 || !blockUntil.IsZero()
	blockStrikes = 0
	blockUntil = time.Time{}
	blockMu.Unlock()
	if had {
		applog.Info("[Douban] 反爬熔断解除，恢复正常抓取节奏")
	}
}

// blockedError 本地闸门拦下请求时构造统一的失败原因：静默期未过，或者限速排队等不起。
// reason 必须写实际原因——这两种情况在诊断里是不同的处置方式，不能都印成"熔断静默中"。
// AntiCrawl 恒为 true：这不是"这个关键词查无此条"，记录要保持可重试。
// Local 同时置位，让调用方分清"我们没问过豆瓣"和"豆瓣回了反爬页"。
func blockedError(urlStr, reason string) error {
	return &doubanFetchError{
		URL:        urlStr,
		AntiCrawl:  true,
		Local:      true,
		StatusCode: http.StatusTooManyRequests,
		Location:   reason,
	}
}

// detailChallengedUntil 记录“详情页网页连验证题都过不去”的截止时间（unix nano）。
// 正常情况 sec.douban.com 的 proof-of-work 能被本地解掉，网页直接返回全字段；
// 只有解题失败（豆瓣调高难度或改版）才需要退避：命中后改走 JSON 兜底，每 30
// 分钟才重新试探一次网页，一旦放开就自动回到全字段路径。
var detailChallengedUntil atomic.Int64

const detailProbeInterval = 30 * time.Minute

func detailProbeAllowed() bool {
	return time.Now().UnixNano() >= detailChallengedUntil.Load()
}

// isRemoteAntiCrawl 只有「豆瓣自己回了反爬页」才算。本地闸门（静默期未过、限速
// 排队等不起）拦下的请求根本没发出去，把它当成被封会产出一个自锁：队列越闲、
// 越没人真的问过豆瓣，详情页却被降档 30 分钟。
func isRemoteAntiCrawl(err error) bool {
	var fetchErr *doubanFetchError
	return errors.As(err, &fetchErr) && fetchErr.AntiCrawl && !fetchErr.Local
}

func markDetailChallenged() {
	detailChallengedUntil.Store(time.Now().Add(detailProbeInterval).UnixNano())
}

func clearDetailChallenged() {
	detailChallengedUntil.Store(0)
}

func checkAntiCrawl(html string) bool {
	// 精确反爬检测：真正的反爬/验证页面有明确特征。
	// 之前的"加载中"误判率很高——豆瓣搜索页初始 HTML（smart-box 占位）
	// 确实包含"加载中"，但那不是反爬，而是 JS 占位文本。
	// 改为：只有当页面既包含反爬关键词，又没有任何 subject/tv/movie 链接时，
	// 才判定为反爬（真正反爬页不会带正常结果链接）。
	//
	// 新版搜索页把候选全塞进 __DATA__ 的内嵌 JSON，并且整页非 ASCII 都转义成 \u、
	// 斜杠转义成 \/：上面的链接正则和下面的明文中文关键词对这种页都是瞎的。
	// JSON 里有候选就一定是正常页——反爬页不会替你准备好搜索结果。
	if payload, ok := decodeSearchPage(html); ok && len(payload.Items) > 0 {
		return false
	}
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
