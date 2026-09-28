package douban

import (
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

const (
	searchURL = "https://search.douban.com/movie/subject_search"
	detailURL = "https://movie.douban.com/subject/%s/"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36"
	referer   = "https://movie.douban.com/"

	// subjectAbstractURL 是详情页的兜底数据源。正常路径是解掉 sec.douban.com 的
	// 验证题直接拿网页全量字段；只有在验证题解不出来（难度被调高、页面改版）时，
	// 才退回这个 JSON 接口——它不受该防护影响，实测 200，但缺评分人数/又名/IMDb。
	subjectAbstractURL = "https://movie.douban.com/j/subject_abstract?subject_id=%s"
)

// 豆瓣请求间隔采用随机抖动，避免固定节奏被识别为爬虫。
//
// 10~30 秒的旧节奏实测会把 97% 的请求打到 sec.douban.com 验证页（2026-09-22
// 单日 284 次抓取中 276 次被 302），说明节奏本身就在触发反爬，因此整体放缓。
// 后台批量补全是无人值守任务，用宽间隔；用户主动点击的交互请求用窄间隔，
// 但两者共用同一个 lastRequestTime，所以交互请求也无法插队到窄间隔以内。
const (
	minRequestInterval = 60 * time.Second
	maxRequestInterval = 150 * time.Second

	interactiveMinRequestInterval = 15 * time.Second
	interactiveMaxRequestInterval = 40 * time.Second
)

// 等待预算。搜索/详情属于「这次不发出去任务就完不成」，所以等到底；评论和热榜结果
// 摆在界面上，等不起就快速失败，让上层用缓存或下次刷新补上。后台批量补全期间闸门会
// 被推到 60~150 秒，那时后两者直接放弃——给批量任务让路正是应该发生的事。
// uiWaitBudget 略高于交互上限，保证纯交互时永远只需要等、不需要失败。
const (
	crawlerWaitBudget = 30 * time.Minute
	uiWaitBudget      = 45 * time.Second
)

// batchMode 为 true 表示 Updater 正在跑无人值守的批量补全。
var batchMode atomic.Bool

var (
	// doubanJar 保存验证通过后发放的 dbsawcv1 放行 Cookie（Max-Age 只有 120 秒）
	// 以及常规的匿名 bid Cookie。浏览器也是这么做的：解一次题，两分钟内所有
	// 豆瓣子域都免检；不共享 jar 的话每条请求都要重解一遍。
	doubanJar, _ = cookiejar.New(nil)

	client = &http.Client{
		Timeout: 15 * time.Second,
		Jar:     doubanJar,
		// 不跟随登录/验证重定向，否则 302 会被伪装成看似正常的 200 页面。
		// 验证跳转由 solveDoubanChallenge 显式处理。
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
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
	subjectIDRegex = regexp.MustCompile(`(?i)(?:https?://(?:www\.)?movie\.douban\.com/subject/|/subject/)(\d+)`)
	// 格式 2：智能搜索结果 - href="https://www.douban.com/doubanapp/dispatch?uri=/tv/35426411"
	// 只匹配 movie 和 tv 类型，排除 book（图书）和 music（音乐）
	subjectIDRegexSmart = regexp.MustCompile(`(?i)(?:https?://(?:www\.)?douban\.com/doubanapp/dispatch\?uri=/(?:movie|tv)/|doubanapp/dispatch\?uri=/(?:movie|tv)/)(\d+)`)
	// 格式 3：JSON 转义的链接 - href=\"https://www.douban.com/doubanapp/dispatch?uri=/tv/35426411\"
	subjectIDRegexJSON = regexp.MustCompile(`(?i)doubanapp/dispatch\?uri=/(?:movie|tv)/(\d+)`)

	// 智能搜索结果中的标题提取 - class="DouWeb-SR-subject-info-name tv">万界独尊 第一季</a>
	smartTitleRegex = regexp.MustCompile(`(?is)<a\b[^>]*\bclass\s*=\s*["'][^"']*\bDouWeb-SR-subject-info-name\b[^"']*["'][^>]*>(.*?)</a>`)
	// 常规搜索结果中的标题提取（title属性格式） - <a href="..." title="万界独尊 第一季">
	regularTitleRegex = regexp.MustCompile(`(?is)<a\b[^>]*\bhref\s*=\s*["'][^"']*(?:movie\.douban\.com/subject/|/subject/)\d+[^"']*["'][^>]*\btitle\s*=\s*["']([^"']+)["']`)
	// 常规搜索结果中的标题提取（文本格式） - <a ... class="title-text">权力的游戏 第一季 Game of Thrones Season 1‎ (2011)</a>
	titleTextRegex = regexp.MustCompile(`(?is)<a\b[^>]*\bclass\s*=\s*["'][^"']*\btitle-text\b[^"']*["'][^>]*>(.*?)</a>`)

	directorRegex     = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>导演</span>\s*:\s*<span\b[^>]*\bclass\s*=\s*["'][^"']*\battrs\b[^"']*["'][^>]*>([\s\S]*?)</span></span>`)
	writerRegex       = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>编剧</span>\s*:\s*<span\b[^>]*\bclass\s*=\s*["'][^"']*\battrs\b[^"']*["'][^>]*>([\s\S]*?)</span></span>`)
	actorRegex        = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>主演</span>\s*:\s*<span\b[^>]*\bclass\s*=\s*["'][^"']*\battrs\b[^"']*["'][^>]*>([\s\S]*?)</span></span>`)
	genreRegex        = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>类型\s*:</span>\s*<span\b[^>]*\bproperty\s*=\s*["']v:genre["'][^>]*>([^<]+)</span>`)
	countryRegex      = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>制片国家/地区\s*:</span>\s*([^<]+)`)
	languageRegex     = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>语言\s*:</span>\s*([^<]+)`)
	releaseDateRegex  = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>首播\s*:</span>\s*<span\b[^>]*\bproperty\s*=\s*["']v:initialReleaseDate["'][^>]*>([^<]+)</span>`)
	episodeCountRegex = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>集数\s*:</span>\s*([^<]+)`)
	seasonCountRegex  = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>季数\s*:</span>\s*([^<]+)`)
	durationRegex     = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>单集片长\s*:</span>\s*([^<]+)`)
	akaRegex          = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>又名\s*:</span>\s*([^<]+)`)
	imdbRegex         = regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>IMDb\s*:</span>\s*([^<]+)`)
	ratingRegex       = regexp.MustCompile(`<strong\b[^>]*\bproperty\s*=\s*["']v:average["'][^>]*>([\d.]+)</strong>`)
	votesRegex        = regexp.MustCompile(`<span\b[^>]*\bproperty\s*=\s*["']v:votes["'][^>]*>(\d+)</span>`)
	// 短评数量：豆瓣详情页中 "全部 12345 条" 或 "12345 条短评" 等格式
	shortCommentsRegex = regexp.MustCompile(`(?:全部\s*)?(\d[\d,]*)\s*条\s*(?:短评|评论)`)
	posterRegex        = regexp.MustCompile(`(?is)<img\b[^>]*\bsrc\s*=\s*["']([^"']*doubanio[^"']*)["'][^>]*\balt\s*=\s*["']([^"']+)`)
	// 标题提取：从 <h1> 中提取（作为 poster 提取失败时的兆底）
	titleRegex   = regexp.MustCompile(`<span\s+property="v:itemreviewed">([^<]+)</span>`)
	ogTitleRegex = regexp.MustCompile(`(?is)<meta\b[^>]*\bproperty\s*=\s*["']og:title["'][^>]*\bcontent\s*=\s*["']([^"']+)`)

	// ===== 新版搜索结果解析正则（2024+ HTML 结构）=====
	// subject ID from data-moreurl JS param: subject_id:'35861087'
	moreurlSubjectIDRegex = regexp.MustCompile(`(?i)\bsubject_id\s*[:=]\s*(?:\\?["'])?(\d+)`)
	// title from title-text class
	searchTitleTextRegex = regexp.MustCompile(`(?is)<a\b[^>]*\bclass\s*=\s*["'][^"']*\btitle-text\b[^"']*["'][^>]*>(.*?)</a>`)
	// year from title suffix: (2025)
	searchYearRegex = regexp.MustCompile(`\((\d{4})\)\s*$`)
	// 采集源里也有把年份直接跟在片名后面的：「战狼 2015」。豆瓣不给这种写法，
	// 所以两边都要在归一化前摘掉，否则它会被后面的规则当成季号。
	bareYearSuffixRegex = regexp.MustCompile(`\s+(?:19|20)\d{2}\s*$`)
	// meta abstract divs
	searchMetaRegex = regexp.MustCompile(`(?is)<(?:div|span)\b[^>]*\bclass\s*=\s*["'][^"']*\bmeta\b[^"']*["'][^>]*>(.*?)</(?:div|span)>`)

	linkTextRegex     = regexp.MustCompile(`(?is)<a\b[^>]*>([^<]+)</a>`)
	itemRootOpenRegex = regexp.MustCompile(`(?is)<div\b[^>]*\bclass\s*=\s*["'][^"']*\bitem-root\b[^"']*["'][^>]*>`)
	anchorRegex       = regexp.MustCompile(`(?is)<a\b[^>]*>.*?</a>`)
	htmlAttrRegex     = regexp.MustCompile(`(?i)([a-z_:][a-z0-9_.:-]*)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+))`)

	// 用于剥离季数/部数信息的正则，提高搜索命中率
	seasonStripRegex = regexp.MustCompile(`(?i)\s*第[一二三四五六七八九十\d]+[季部季]|\s*Season\s*\d+|\s*Part\s*\d+`)

	antiCrawlPatterns = []string{
		"验证码",
		"请完成安全验证",
		"Please verify",
		"403 Forbidden",
		"访问过于频繁",
		"您的访问请求被拒绝",
		"系统检测到异常请求",
	}
)

// doubanFetchError 保留 HTTP 层的失败类型，避免把一次临时的 403/验证页
// 当成“搜索无结果”写入数据库冷却状态。
//
// Local 表示这次请求根本没有发出去——是本地闸门（静默期或限速排队）拦下的。
// 它和豆瓣真的回了反爬页是两回事：前者要的是等，后者才需要把详情页路径降档。
type doubanFetchError struct {
	URL        string
	StatusCode int
	AntiCrawl  bool
	Local      bool
	Location   string
}

func (e *doubanFetchError) Error() string {
	if e.Local {
		return fmt.Sprintf("douban request skipped locally: %s", e.Location)
	}
	if e.AntiCrawl && e.Location != "" {
		return fmt.Sprintf("douban anti-crawl redirect: HTTP %d -> %s", e.StatusCode, e.Location)
	}
	if e.Location != "" {
		return fmt.Sprintf("douban request failed: HTTP %d redirect to %s", e.StatusCode, e.Location)
	}
	if e.AntiCrawl {
		return fmt.Sprintf("douban anti-crawl page detected (HTTP %d)", e.StatusCode)
	}
	return fmt.Sprintf("douban request failed: HTTP %d", e.StatusCode)
}

func isDoubanChallengeURL(location string) bool {
	lower := strings.ToLower(location)
	return strings.Contains(lower, "sec.douban.com") ||
		strings.Contains(lower, "captcha") ||
		strings.Contains(lower, "accounts.douban.com")
}

// secFetchSite 按 Fetch Metadata 规范算出 Referer 与目标地址的关系。
// 规范以注册域（douban.com）为界：movie.douban.com → search.douban.com 是
// same-site，不是 cross-site；写错反而比不带这组头更像脚本。
func secFetchSite(referer, host string) string {
	if referer == "" {
		return "none"
	}
	ref, err := url.Parse(referer)
	if err != nil || ref.Host == "" {
		return "none"
	}
	if strings.EqualFold(ref.Host, host) {
		return "same-origin"
	}
	if registerDomain(ref.Host) == registerDomain(host) {
		return "same-site"
	}
	return "cross-site"
}

// registerDomain 取主机名的后两段作为注册域近似值。豆瓣全站都是 *.douban.com，
// 这里不需要处理 co.uk 之类的多段公共后缀。
func registerDomain(host string) string {
	domain := strings.ToLower(host)
	if i := strings.LastIndexByte(domain, ':'); i >= 0 {
		domain = domain[:i]
	}
	parts := strings.Split(domain, ".")
	if len(parts) <= 2 {
		return domain
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

func applyDoubanHeaders(req *http.Request, requestReferer string) {
	req.Header.Set("User-Agent", userAgent)
	if requestReferer != "" {
		req.Header.Set("Referer", requestReferer)
	}
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	// 浏览器发起文档导航时一定会带这组头，缺了就是脚本流量的明显特征。
	req.Header.Set("Upgrade-Insecure-Requests", "1")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Sec-Fetch-Site", secFetchSite(req.Header.Get("Referer"), req.URL.Host))
	// Cookie 必须是可选的。仓库里原先硬编码的登录 Cookie 会过期、泄露，
	// 还可能把所有请求导向登录/验证页；需要时由部署环境显式提供。
	cookieValue := strings.TrimSpace(os.Getenv("CCZJ_DOUBAN_COOKIE"))
	if cookieValue == "" {
		cookieValue = strings.TrimSpace(os.Getenv("DOUBAN_COOKIE"))
	}
	if cookieValue != "" {
		req.Header.Set("Cookie", cookieValue)
	}
}

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
	// GlobalID 是 global_video 的行 ID，冷却/失败计数只按它读写本行。
	// 手动搜索没有对应行，留 0 即天然不受冷却约束（见 db.IsDoubanSearchOnCooldown）。
	GlobalID int
	VodName  string
	Year     string
	VodType  string // 视频分类名（如"动漫"、"电视剧"、"电影"）
	Director string // 逗号分隔的导演列表
	Actor    string // 逗号分隔的演员列表
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

// reserveDoubanSlot 是所有豆瓣 HTTP 请求唯一的限速入口：在锁内算出「这次最早可以
// 出发的时刻」并把它登记为下一次间隔的基准，调用方自己决定等到那时候还是放弃。
//
// 以前这里有三套时钟：搜索/详情走 lastRequestTime（15~40s / 批量 60~150s）、评论走
// 独立的 lastCommentTime（8~20s）、热榜完全不限速（只尊重静默期）。豆瓣按 IP 计数，
// 不区分这三条路径，于是实际间隔是三者取最小——同一秒里搜索、评论、热榜各发一次
// 完全合法，这正是要出「搜索访问太频繁」的节奏。合并成一个基准之后，任何两次豆瓣
// 请求之间都至少隔一个 minGap；抖动的用意是打散连续请求的节奏，所以照旧无条件叠加，
// 空闲很久之后也不抹掉它（宁可等，不可抢）。
func reserveDoubanSlot() time.Time {
	rateMu.Lock()
	defer rateMu.Unlock()

	minGap, maxGap := interactiveMinRequestInterval, interactiveMaxRequestInterval
	if batchMode.Load() {
		minGap, maxGap = minRequestInterval, maxRequestInterval
	}
	jitter := time.Duration(0)
	if jitterRange := maxGap - minGap; jitterRange > 0 {
		jitter = time.Duration(rand.Int63n(int64(jitterRange)))
	}

	next := time.Now()
	if !lastRequestTime.IsZero() {
		// 基准取「上一次出发时刻 + 最小间隔」和「现在」的较大者，再无条件叠一次抖动，
		// 与旧实现逐位等价：空闲很久也不会把抖动省掉。
		if earliest := lastRequestTime.Add(minGap); earliest.After(next) {
			next = earliest
		}
		next = next.Add(jitter)
	}
	lastRequestTime = next
	return next
}

// awaitDoubanSlot 睡到领到的时间槽，budget 是这次愿意等的上限。
// 返回的时长是这次实际需要等多久（负数表示无需等待），供调用方写进失败原因。
//
// ok 为 false 表示等不起：直接放弃这次请求，而不是绕过闸门硬打——退让本身就是让限速生效。
// 注意放弃时槽位已经占掉了，下一次请求会因此等得更久，这个方向是安全的。
func awaitDoubanSlot(caller string, budget time.Duration) (wait time.Duration, ok bool) {
	wait = time.Until(reserveDoubanSlot())
	if wait > budget {
		applog.Warn("[Douban] %s 需要等 %s，超出可等待的 %s，本次放弃",
			caller, wait.Round(time.Second), budget)
		return wait, false
	}
	if wait > 0 {
		applog.Debug("[Douban] %s 限速等待 %.1fs", caller, wait.Seconds())
		time.Sleep(wait)
	}
	return wait, true
}

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

// cleanTitle 去除标题中的不可见字符和年份后缀
func cleanTitle(title string) string {
	title = html.UnescapeString(stripHTMLTags(title))
	// 去除不可见 Unicode 字符（LTR mark、RTL mark 等）
	title = strings.Map(func(r rune) rune {
		if r == '\u200E' || r == '\u200F' || r == '\u200B' || r == '\uFEFF' {
			return -1 // 删除
		}
		return r
	}, title)
	// 去除年份后缀
	title = searchYearRegex.ReplaceAllString(title, "")
	title = bareYearSuffixRegex.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	return title
}

// splitItemBlocks 将搜索结果 HTML 拆分为独立的候选项块
func splitItemBlocks(html string) []string {
	var blocks []string
	markers := itemRootOpenRegex.FindAllStringIndex(html, -1)
	for i, marker := range markers {
		start := marker[1]
		end := len(html)
		if i+1 < len(markers) {
			end = markers[i+1][0]
		}
		if start < end {
			blocks = append(blocks, html[start:end])
		}
	}
	return blocks
}

func parseHTMLAttributes(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range htmlAttrRegex.FindAllStringSubmatch(tag, -1) {
		if len(match) < 5 {
			continue
		}
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value == "" {
			value = match[4]
		}
		attrs[strings.ToLower(match[1])] = html.UnescapeString(value)
	}
	return attrs
}

func hasHTMLClass(classes, className string) bool {
	for _, class := range strings.Fields(classes) {
		if class == className {
			return true
		}
	}
	return false
}

func extractSubjectID(text string) string {
	for _, pattern := range []*regexp.Regexp{
		moreurlSubjectIDRegex,
		subjectIDRegex,
		subjectIDRegexSmart,
		subjectIDRegexJSON,
	} {
		if match := pattern.FindStringSubmatch(text); len(match) >= 2 {
			return match[1]
		}
	}
	return ""
}

func extractCandidateTitle(block string) string {
	for _, anchor := range anchorRegex.FindAllString(block, -1) {
		openEnd := strings.Index(anchor, ">")
		if openEnd < 0 {
			continue
		}
		attrs := parseHTMLAttributes(anchor[:openEnd+1])
		closeStart := strings.LastIndex(strings.ToLower(anchor), "</a>")
		if closeStart < openEnd {
			continue
		}
		text := strings.TrimSpace(html.UnescapeString(stripHTMLTags(anchor[openEnd+1 : closeStart])))
		classes := attrs["class"]
		if (hasHTMLClass(classes, "title-text") || hasHTMLClass(classes, "DouWeb-SR-subject-info-name")) && text != "" {
			return text
		}
		if extractSubjectID(anchor) != "" && attrs["title"] != "" {
			return strings.TrimSpace(html.UnescapeString(attrs["title"]))
		}
	}
	return ""
}

func parseSearchCandidatesFromAnchors(html string) []SearchCandidate {
	var candidates []SearchCandidate
	seen := make(map[string]bool)
	for _, anchor := range anchorRegex.FindAllString(html, -1) {
		subjectID := extractSubjectID(anchor)
		if subjectID == "" || seen[subjectID] {
			continue
		}
		title := extractCandidateTitle(anchor)
		if title == "" {
			continue
		}
		seen[subjectID] = true
		year := 0
		if match := searchYearRegex.FindStringSubmatch(title); len(match) >= 2 {
			year, _ = strconv.Atoi(match[1])
		}
		candidates = append(candidates, SearchCandidate{SubjectID: subjectID, Title: cleanTitle(title), Year: year})
	}
	return candidates
}

// parseSearchCandidates 解析搜索结果，提取所有候选项及其元数据。
// 优先读页面内嵌的 window.__DATA__（当前豆瓣搜索页唯一的候选来源），
// 拿不到再退回去 node 节点的老 HTML 路径。
func parseSearchCandidates(html string) []SearchCandidate {
	if candidates := parseSearchCandidatesFromJSON(html); len(candidates) > 0 {
		return candidates
	}
	blocks := splitItemBlocks(html)
	if len(blocks) == 0 {
		return parseSearchCandidatesFromAnchors(html)
	}

	var candidates []SearchCandidate
	for _, block := range blocks {
		// 提取 subject ID（从 data-moreurl 的 JS 参数中）
		subjectID := extractSubjectID(block)
		if subjectID == "" {
			continue
		}

		// 提取标题
		rawTitle := extractCandidateTitle(block)
		if rawTitle == "" {
			continue
		}

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
			meta1 = stripHTMLTags(metaMatches[0][1])
		}
		if len(metaMatches) >= 2 {
			meta2 = stripHTMLTags(metaMatches[1][1])
		}

		// 判断是否为剧集
		isSeries := strings.Contains(block, `[剧集]`) ||
			strings.Contains(block, `is_tv:'1'`) ||
			strings.Contains(block, `is_tv:"1"`) ||
			strings.Contains(block, `is_tv=\"1\"`)

		// 解析导演/演员（meta2 中导演在前、演员在后，用 / 分隔）
		director, actor := parseDirectorActor(meta2)

		candidates = append(candidates, SearchCandidate{
			SubjectID: subjectID,
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
	normTitle := normalizeTitle(c.Title)
	normKeyword := normalizeTitle(meta.VodName)
	candBase, candSeason, candPart := splitSeasonTag(normTitle)
	metaBase, metaSeason, metaPart := splitSeasonTag(normKeyword)

	// 片名是门槛，不是加分项：对不上就直接零分，年份/导演/演员都不再参与。
	// 否则「同年 + 同导演×2 + 同演员」足以把一部片名无关的候选抬过阈值。
	score := titleMatchScore(normTitle, normKeyword, candBase, metaBase)
	if score == 0 {
		return 0
	}
	// 季号冲突走同一条路：短路，而不是扣分。同一部剧各季的片名、导演、演员几乎
	// 全都相同，扣掉的分能被旁证加回来，而挂错季会连带污染评分、海报和兄弟继承。
	delta, conflict := seasonMatchDelta(metaSeason, metaPart, candSeason, candPart)
	if conflict {
		return seasonConflictScore
	}
	score += delta

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

func fetchHTML(urlStr string) (string, error) {
	return fetchDouban(urlStr, false)
}

// fetchDouban 抓取一个豆瓣地址。ignoreBlock 只跳过“本地静默期”这一道闸门：
// 兜底的 JSON 接口是另一套防护、实测不受影响；若连它也拦掉，兜底就永远是死代码。
// 限速和反爬计数照常生效，所以 JSON 接口一旦被封同样会续上静默期。
//
// 命中 sec.douban.com 验证跳转时先自解 proof-of-work 再重试，解成了就不算被封。
func fetchDouban(urlStr string, ignoreBlock bool) (string, error) {
	if !ignoreBlock {
		if left := remainingBlock(); left > 0 {
			applog.Warn("[Douban] 反爬静默中（剩余 %s），跳过请求: %s", left.Round(time.Second), urlStr)
			return "", blockedError(urlStr, fmt.Sprintf("本地反爬熔断静默中，剩余 %s", left.Round(time.Second)))
		}
	}
	if wait, ok := awaitDoubanSlot("搜索/详情", crawlerWaitBudget); !ok {
		return "", blockedError(urlStr, fmt.Sprintf(
			"本地限速需等待 %s，超过本次可等待的 %s", wait.Round(time.Second), crawlerWaitBudget))
	}

	applog.Info("[Douban] Fetching URL: %s", urlStr)

	html, challenge, err := fetchDoubanOnce(urlStr, referer)
	if challenge != "" {
		// 这是一道算力题而不是登录墙，所以先解题重试，只有解不出来才计入熔断。
		// 把"能过的题"也当成封禁，会白白静默 5~60 分钟，字段也就永远补不全。
		if solveErr := solveDoubanChallenge(challenge, urlStr); solveErr != nil {
			applog.Warn("[Douban] 自动通过验证失败：%v", solveErr)
			noteAntiCrawl()
			return "", err
		}
		// 放行 Cookie 已在 jar 里；浏览器解题后也是带着它重新导航一次。
		var again string
		html, again, err = fetchDoubanOnce(urlStr, challenge)
		if again != "" {
			// 解完题仍被踢回验证页，说明放行没被接受（IP 被更硬地标记了）。
			// 继续逐条解题只会把配额烧光，这里必须收手。
			applog.Warn("[Douban] 解题后仍被导向验证页，进入反爬静默")
			noteAntiCrawl()
			return "", err
		}
	}
	if err != nil {
		var httpErr *doubanFetchError
		if !errors.As(err, &httpErr) {
			// 超时/连接失败同样该触发静默期：豆瓣对被标记的 IP 会接受连接但
			// 一直不给响应，不收手的话每条请求都要白烧两次 15s 超时。
			applog.Warn("[Douban] 请求未得到响应，进入反爬静默：%v", err)
			noteAntiCrawl()
		}
		return "", err
	}

	// 兜底接口成功不代表详情页解封了：解除熔断会让下一条又去打一次注定被挡的
	// 网页，静默期也就失去了省请求的意义。这里只在正常路径上解除。
	if !ignoreBlock {
		noteDoubanSuccess()
	}
	return html, nil
}

// fetchDoubanOnce 发一次 GET 并做状态/反爬判定。返回的 challenge 非空表示被
// sec.douban.com 验证页挡住，调用方可解题后重试；此时 err 是解题失败时要
// 原样上报的错误。
func fetchDoubanOnce(urlStr, requestReferer string) (doc string, challenge string, err error) {
	startTime := time.Now()

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		applog.Error("[Douban] Failed to create request: %v", err)
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	applyDoubanHeaders(req, requestReferer)

	resp, err := client.Do(req)
	if err != nil {
		applog.Error("[Douban] HTTP request failed: %v", err)
		return "", "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(startTime)
	applog.Info("[Douban] HTTP status: %d, duration: %.2fs", resp.StatusCode, duration.Seconds())

	if resp.StatusCode != http.StatusOK {
		location := resp.Header.Get("Location")
		challenged := isDoubanChallengeURL(location)
		solvable := challenged && isSecChallengeURL(location)

		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
		if len(body) > 0 && !solvable {
			// 可解的验证跳转是正常流程的一环，别在用户可见的时间线里刷 WARN 302。
			snippet := string(body)
			if len(snippet) > 200 {
				snippet = snippet[:200] + "..."
			}
			applog.Warn("[Douban] HTTP %d response snippet: %s", resp.StatusCode, snippet)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			applog.Info("[Douban] Redirect detected: %d -> %s", resp.StatusCode, location)
		}
		if solvable {
			return "", location, &doubanFetchError{
				URL:        urlStr,
				StatusCode: resp.StatusCode,
				Location:   location,
				AntiCrawl:  true,
			}
		}
		if challenged {
			noteAntiCrawl()
		}
		return "", "", &doubanFetchError{
			URL:        urlStr,
			StatusCode: resp.StatusCode,
			Location:   location,
			AntiCrawl:  challenged,
		}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		applog.Error("[Douban] Failed to read response body: %v", err)
		return "", "", fmt.Errorf("failed to read body: %w", err)
	}

	page := string(body)

	if len(page) < 100 {
		applog.Warn("[Douban] Suspiciously short response (len=%d): %s", len(page), page)
	}

	if checkAntiCrawl(page) {
		applog.Warn("[Douban] Anti-crawl triggered, response length: %d", len(page))
		noteAntiCrawl()
		return "", "", &doubanFetchError{URL: urlStr, StatusCode: resp.StatusCode, AntiCrawl: true}
	}

	return page, "", nil
}

// stripSeasonInfo 移除关键词中的季数/部数信息，提高豆瓣搜索命中率。
// 例如 "权力的游戏 第八季" → "权力的游戏"
func stripSeasonInfo(keyword string) string {
	s := seasonStripRegex.ReplaceAllString(keyword, "")
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "　", "")
	return s
}

// doubanMatchThreshold 是「敢把这个 subject_id 挂到这部片上」的最低分，刻度见
// scoreCandidate：片名对不上直接零分（门槛，不是加分项），折叠后完全同名 100、
// 摘掉季号后片名一致 90、包含关系 50，年份 +30、导演每人 +20、演员最多 +15，
// 季号一致 +25，季号冲突则短路成 seasonConflictScore。
//
// 定在 70 的用意：只认「片名基本对上」或「片名沾边 + 至少一项硬元数据佐证」。
// 光靠包含关系（50）不足以区分「爱情」和「爱情公寓」，宁可这部片没有豆瓣信息，
// 也不能挂一个别人的条目——挂错的 id 会连带把评分、海报、热榜和兄弟记录继承
// 一起带偏，而且再也没有第二条路径会发现它错了。
const doubanMatchThreshold = 70

// uniqueFallbackSubjectID 在整页只指向一个豆瓣 subject 时返回它。
// 同一部片在页面上会被标题和海报各链接一次，所以按去重后的个数判断。
func uniqueFallbackSubjectID(html string) (string, bool) {
	var only string
	seen := make(map[string]bool, 2)
	for _, m := range subjectIDRegex.FindAllStringSubmatch(html, -1) {
		id := m[1]
		if seen[id] {
			continue
		}
		seen[id] = true
		only = id
		if len(seen) > 1 {
			return "", false
		}
	}
	return only, only != ""
}

func SearchSubjectID(keyword string, meta SearchMeta) (string, error) {
	// 冷却是按行记的：写和读都只认 global_id，不再靠 vod_name 猜行。
	if db.IsDoubanSearchOnCooldown(meta.GlobalID) {
		applog.Debug("[Douban] Skipping search for '%s' (id=%d in cooldown period)", keyword, meta.GlobalID)
		return "", fmt.Errorf("search cooldown active for '%s'", keyword)
	}

	// 多层搜索策略：先用原始关键词，失败后尝试去掉季数信息
	keywords := []string{keyword}
	stripped := stripSeasonInfo(keyword)
	if stripped != "" && stripped != keyword {
		keywords = append(keywords, stripped)
	}

	var lastErr error
	hadUsableSearchResponse := false
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
			if isRemoteAntiCrawl(err) {
				applog.Warn("[Douban] '%s' returned an anti-crawl page; keeping the record retryable", kw)
			}
			lastErr = err
			continue
		}
		if payload, ok := decodeSearchPage(html); ok && payload.ErrorInfo != "" && len(payload.Items) == 0 {
			// 豆瓣把限流写在 __DATA__ 的 error_info 里，配一个 200 和结构完整的正常页。
			// 这既不是验证页（checkAntiCrawl 匹配不到 \u 转义后的中文），也不是「这部片
			// 查无此条」：当成后者的话，每个关键词都要白挨一次「搜索无结果」冷却，
			// 而真正的解法只是全体停下来等。限流是 IP 级信号，够格触发全局静默。
			applog.Warn("[Douban] 搜索页回限流提示 %q，进入反爬静默: %s", payload.ErrorInfo, kw)
			noteAntiCrawl()
			lastErr = fmt.Errorf("douban search throttled for %s: %s", kw, payload.ErrorInfo)
			break // 换关键词再打一次只会把静默期推得更高
		}
		hadUsableSearchResponse = true

		if checkAntiCrawl(html) {
			applog.Warn("[Douban] Anti-crawl triggered for '%s' (HTML len=%d)", kw, len(html))
			lastErr = fmt.Errorf("anti-crawl detected")
			continue
		}

		// 解析所有候选结果
		candidates := parseSearchCandidates(html)

		if len(candidates) == 0 {
			// 兜底：旧版正则只能确认「整页只指向一个 subject」时才敢用。
			// 多个 ID 时取第一个等于抽签，页面顺序一变就挂到另一部片上。
			if id, ok := uniqueFallbackSubjectID(html); ok {
				applog.Info("[Douban] New parser found 0 candidates, page points at exactly one subject %s for '%s'", id, kw)
				db.ClearDoubanSearchFailure(meta.GlobalID)
				return id, nil
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

			if score >= doubanMatchThreshold {
				db.ClearDoubanSearchFailure(meta.GlobalID)
				return match.SubjectID, nil
			}

			// 分数不够就真的不要了。过去这里「低置信度也接受」，等于阈值只是
			// 日志里的装饰：错挂的 subject_id 一旦落库，评分、海报、热榜和
			// 兄弟继承都会把它复制到更多行上。
			applog.Warn("[Douban] Rejected match for '%s': score=%d < %d (best title=%q)",
				keyword, score, doubanMatchThreshold, match.Title)
			lastErr = fmt.Errorf("best match for %s scored %d, below threshold %d", keyword, score, doubanMatchThreshold)
			continue
		}

		lastErr = fmt.Errorf("no subject ID found for keyword: %s", kw)
	}

	// 只有确实拿到正常搜索页、但没有找到候选时才累计“搜索无结果”。
	// 网络错误、重定向和验证页不能触发 24 小时冷却，否则临时故障会被放大。
	if hadUsableSearchResponse {
		_ = db.MarkDoubanSearchFailure(meta.GlobalID)
	} else {
		applog.Warn("[Douban] Search failed before a usable result page; skip cooldown for '%s'", keyword)
	}
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

// subjectAbstract 对应 /j/subject_abstract 的 JSON 结构，只声明会用到的字段。
type subjectAbstract struct {
	R       int `json:"r"`
	Subject struct {
		ID            string   `json:"id"`
		Title         string   `json:"title"`
		Rate          string   `json:"rate"`
		Directors     []string `json:"directors"`
		Actors        []string `json:"actors"`
		Types         []string `json:"types"`
		Region        string   `json:"region"`
		Duration      string   `json:"duration"`
		EpisodesCount string   `json:"episodes_count"`
		ReleaseYear   string   `json:"release_year"`
	} `json:"subject"`
}

// parseSubjectAbstract 把 JSON 兜底数据填进 DoubanInfo。
// 评价人数、编剧、语言、又名、IMDb、海报在这个接口里没有，留空即可：
// db.UpsertDoubanInfo 对所有空字段都是“保留数据库里的旧值”，不会把已有数据冲掉。
func parseSubjectAbstract(subjectID, body string) (*DoubanInfo, error) {
	var payload subjectAbstract
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &payload); err != nil {
		return nil, fmt.Errorf("decode subject_abstract for %s: %w", subjectID, err)
	}
	s := payload.Subject
	if s.Rate == "" && len(s.Directors) == 0 && len(s.Actors) == 0 {
		return nil, fmt.Errorf("subject_abstract for %s has no usable fields", subjectID)
	}
	return &DoubanInfo{
		SubjectID:    subjectID,
		Title:        strings.TrimSpace(s.Title),
		Rating:       strings.TrimSpace(s.Rate),
		Director:     strings.Join(s.Directors, " / "),
		Actor:        strings.Join(s.Actors, " / "),
		Genre:        strings.Join(s.Types, "/"),
		Country:      strings.TrimSpace(s.Region),
		ReleaseDate:  strings.TrimSpace(s.ReleaseYear),
		EpisodeCount: strings.TrimSpace(s.EpisodesCount),
		Duration:     strings.TrimSpace(s.Duration),
	}, nil
}

// fetchDetailByAbstract 在详情页被反爬拦截时改走 JSON 接口。
// ignoreBlock 是必须的：详情页那次 302 已经给自己记了一轮静默期，
// 若连兜底请求都被本地闸门挡住，这条路径永远不会执行。
func fetchDetailByAbstract(subjectID string) (*DoubanInfo, error) {
	body, err := fetchDouban(fmt.Sprintf(subjectAbstractURL, subjectID), true)
	if err != nil {
		return nil, err
	}
	info, err := parseSubjectAbstract(subjectID, body)
	if err != nil {
		return nil, err
	}
	applog.Info("[Douban] 详情页被拦截，已用 subject_abstract 兜底: %s Rating='%s' Director='%s' Actor='%s' Genre='%s'",
		subjectID, info.Rating, truncate(info.Director, 30), truncate(info.Actor, 30), info.Genre)
	return info, nil
}

func ParseDetail(subjectID string) (*DoubanInfo, error) {
	applog.Info("[Douban] Parsing detail for subject ID: %s", subjectID)

	if !detailProbeAllowed() {
		applog.Info("[Douban] 详情页仍在拦截期内（每 %s 才试探一次网页），本次直接用 subject_abstract: %s", detailProbeInterval, subjectID)
		return fetchDetailByAbstract(subjectID)
	}

	urlStr := fmt.Sprintf(detailURL, subjectID)

	html, err := fetchHTML(urlStr)
	if err != nil {
		// /subject/<id>/ 网页层面无条件被 302 到 sec.douban.com 的 JS 验证页，
		// 不带登录 Cookie 就永远拿不到。这里降级到同站 JSON 接口，而不是让整次
		// 更新失败——评分/导演/演员/类型/集数这些主要字段都还能拿到。
		if isRemoteAntiCrawl(err) {
			markDetailChallenged()
		}
		applog.Info("[Douban] Detail page unavailable for %s (%v), falling back to subject_abstract", subjectID, err)
		info, fallbackErr := fetchDetailByAbstract(subjectID)
		if fallbackErr != nil {
			applog.Warn("[Douban] Detail fetch failed for %s: %v (fallback: %v)", subjectID, err, fallbackErr)
			return nil, err
		}
		return info, nil
	}
	clearDetailChallenged()

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
	if info.Title == "" {
		if matches := ogTitleRegex.FindStringSubmatch(html); len(matches) >= 2 {
			info.Title = cleanTitle(matches[1])
		}
	}

	// 兜底解析：如果以上正则都没匹配到关键字段，尝试从 <div id="info"> 中逐行解析
	parseInfoDivFallback(html, info)
	if info.Title == "" && info.Rating == "" && info.Votes == "" && info.PosterURL == "" {
		return nil, fmt.Errorf("detail page returned no recognized fields for subject %s", subjectID)
	}

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
	plPattern := regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>([^<]+)</span>`)
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

// FetchDoubanInfo 先搜 subject_id 再抓详情页。
// 这里不再重复查冷却：SearchSubjectID 已经按 global_id 查过本行了，第二次查
// 只能按关键词查，等于给同一条记录套上两套不同的冷却口径。
func FetchDoubanInfo(keyword string, meta SearchMeta) (*DoubanInfo, error) {
	applog.Info("[Douban] Fetching complete info for keyword: %s", keyword)

	subjectID, err := SearchSubjectID(keyword, meta)
	if err != nil {
		applog.Error("[Douban] FetchDoubanInfo failed at search step for '%s': %v", keyword, err)
		return nil, err
	}

	return ParseDetail(subjectID)
}
