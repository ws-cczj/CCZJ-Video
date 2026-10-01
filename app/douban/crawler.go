package douban

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/netstats"
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
		Transport: netstats.WrapTransport(netstats.CategoryDouban, &http.Transport{
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 2,
			IdleConnTimeout:     30 * time.Second,
		}),
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
		return "", "", apperror.Wrap(apperror.Unavailable, err, "failed to create request")
	}

	applyDoubanHeaders(req, requestReferer)

	resp, err := client.Do(req)
	if err != nil {
		applog.Error("[Douban] HTTP request failed: %v", err)
		return "", "", apperror.Wrap(apperror.Unavailable, err, "failed to fetch")
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
		return "", "", apperror.Wrap(apperror.Unavailable, err, "failed to read body")
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
		return "", apperror.Newf(apperror.Unavailable, "search cooldown active for '%s'", keyword)
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
			lastErr = apperror.Newf(apperror.NotFound, "no candidates parsed for keyword: %s", kw)
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
			lastErr = apperror.Newf(apperror.NotFound, "best match for %s scored %d, below threshold %d", keyword, score, doubanMatchThreshold)
			continue
		}

		lastErr = apperror.Newf(apperror.NotFound, "no subject ID found for keyword: %s", kw)
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
