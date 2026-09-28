package douban

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

// TestMain 必须抢在任何 applog 调用之前把单例绑到临时目录。
// applog.Default() 会在单例为空时用 %APPDATA% 的生产目录把它建出来，
// 本包的测试一旦触发豆瓣日志，就会写进用户真实的日志文件里。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-douban-test-")
	if err != nil {
		panic(err)
	}
	if err := applog.Init(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// resetBlock 保存/还原豆瓣的两个全局时钟（熔断静默期 + 限速基准），避免用例之间互相影响。
func resetBlock(t *testing.T) {
	t.Helper()
	blockMu.Lock()
	until, strikes := blockUntil, blockStrikes
	blockMu.Unlock()
	rateMu.Lock()
	rate := lastRequestTime
	rateMu.Unlock()
	t.Cleanup(func() {
		blockMu.Lock()
		blockUntil, blockStrikes = until, strikes
		blockMu.Unlock()
		rateMu.Lock()
		lastRequestTime = rate
		rateMu.Unlock()
		batchMode.Store(false)
	})
}

// clearRateClock 把限速基准清空，用例从「闸门空闲」这个起点开始。
func clearRateClock() {
	rateMu.Lock()
	lastRequestTime = time.Time{}
	rateMu.Unlock()
}

func TestAntiCrawlBreakerEscalatesAndClears(t *testing.T) {
	resetBlock(t)
	blockMu.Lock()
	blockUntil, blockStrikes = time.Time{}, 0
	blockMu.Unlock()

	if left := remainingBlock(); left != 0 {
		t.Fatalf("未触发反爬时 remainingBlock = %v，期望 0", left)
	}

	// 静默时长按 5/15/60 分钟递增，第四次仍停在最后一档。
	for i, want := range []time.Duration{5 * time.Minute, 15 * time.Minute, time.Hour, time.Hour} {
		noteAntiCrawl()
		left := remainingBlock()
		if left <= 0 || left > want {
			t.Fatalf("第 %d 次命中的静默期 = %v，期望落在 (0, %v]", i+1, left, want)
		}
		if gap := want - left; gap > 5*time.Second {
			t.Fatalf("第 %d 次命中的静默期 = %v，偏离期望档位 %v", i+1, left, want)
		}
	}

	noteDoubanSuccess()
	if left := remainingBlock(); left != 0 {
		t.Fatalf("正常响应后 remainingBlock = %v，期望 0", left)
	}
}

func TestExpiredBlockIsClearedOnRead(t *testing.T) {
	resetBlock(t)
	blockMu.Lock()
	blockUntil = time.Now().Add(-time.Second)
	blockStrikes = 2
	blockMu.Unlock()

	if left := remainingBlock(); left != 0 {
		t.Fatalf("过期静默期 remainingBlock = %v，期望 0", left)
	}
	blockMu.Lock()
	strikes := blockStrikes
	blockMu.Unlock()
	if strikes != 0 {
		t.Fatalf("过期静默期未清零命中数，got %d，期望 0", strikes)
	}
}

// 限速单一闸门：搜索/详情、评论、热榜共用 reserveDoubanSlot 这一个基准，
// 所以「任何两次豆瓣请求之间至少隔一个 minGap」只能在这里钉住。
// 只断言落档，不断言具体抖动值——随机值不可复现，区间才是约定。

func TestReserveDoubanSlotKeepsInteractiveGap(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	batchMode.Store(false)

	first := reserveDoubanSlot()
	if wait := time.Until(first); wait > time.Second {
		t.Fatalf("闸门空闲时首次预约需要等 %v，期望立即放行", wait)
	}
	second := reserveDoubanSlot()
	if gap := second.Sub(first); gap < interactiveMinRequestInterval || gap >= interactiveMaxRequestInterval {
		t.Fatalf("连续两次预约间隔 %v，落在交互档 [%v, %v) 之外",
			gap, interactiveMinRequestInterval, interactiveMaxRequestInterval)
	}
}

func TestReserveDoubanSlotWidensGapInBatchMode(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	batchMode.Store(true)

	first := reserveDoubanSlot()
	second := reserveDoubanSlot()
	// 批量补全是无人值守任务，间隔必须显著宽于交互档；调小等于放宽对豆瓣的礼貌。
	if gap := second.Sub(first); gap < minRequestInterval || gap >= maxRequestInterval {
		t.Fatalf("批量模式间隔 %v，落在批量档 [%v, %v) 之外",
			gap, minRequestInterval, maxRequestInterval)
	}
}

func TestAwaitDoubanSlotRunsImmediatelyWhenGateIdle(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	batchMode.Store(false)

	started := time.Now()
	wait, ok := awaitDoubanSlot("搜索/详情", uiWaitBudget)
	if !ok {
		t.Fatalf("闸门空闲且预算为正，awaitDoubanSlot 不应放弃（wait=%v）", wait)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("闸门空闲时耗时 %v，不应排队", elapsed)
	}
}

func TestAwaitDoubanSlotGivesUpInsteadOfBypassingGate(t *testing.T) {
	resetBlock(t)
	// 模拟批量补全刚出发：闸门已把下一个槽订在 60 秒之后。交互请求此刻来，
	// 无论怎么排都超出 45 秒预算，正确行为是放弃而不是插队。
	rateMu.Lock()
	lastRequestTime = time.Now().Add(minRequestInterval)
	rateMu.Unlock()

	started := time.Now()
	wait, ok := awaitDoubanSlot("评论", uiWaitBudget)
	if ok {
		t.Fatalf("批量节奏下的交互请求应当放弃，却返回可以（wait=%v）", wait)
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("放弃时耗时 %v，应当快速失败而不是睡满", elapsed)
	}
	// 放弃也要把槽占掉：下一次预约必须排在更晚的位置，退让才真正让限速生效。
	rateMu.Lock()
	booked := lastRequestTime
	rateMu.Unlock()
	if left := time.Until(booked); left < minRequestInterval {
		t.Fatalf("放弃后闸门只留到 %v，期望至少还占着批量间隔 %v", left.Round(time.Second), minRequestInterval)
	}
}

func TestFetchHTMLFailsFastWhileBlocked(t *testing.T) {
	resetBlock(t)
	noteAntiCrawl()

	started := time.Now()
	html, err := fetchHTML("https://movie.douban.com/subject/10590706/")
	if err == nil {
		t.Fatal("静默期内 fetchHTML 应当失败，却拿到了响应")
	}
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("静默期内 fetchHTML 耗时 %v，应当快速失败而不是排队等待", elapsed)
	}
	if html != "" {
		t.Fatalf("静默期内不应返回内容，got %d 字节", len(html))
	}
	var fetchErr *doubanFetchError
	if !errors.As(err, &fetchErr) || !fetchErr.AntiCrawl {
		t.Fatalf("错误类型 = %v，期望按反爬错误处理，以便记录保持可重试", err)
	}
	// AntiCrawl 只用来标记「这不是查无此条」；Local 才区分「我们没问过豆瓣」。
	// 少了 Local，静默期里的一次放弃会被当成豆瓣封禁，把详情页降档 30 分钟。
	if !fetchErr.Local {
		t.Error("本地闸门拦下的请求没被标记成 Local，会被误读成豆瓣真的回了反爬页")
	}
	if isRemoteAntiCrawl(err) {
		t.Error("本地闸门拦下的请求被判成了远端封禁")
	}
}

// 本地闸门和真被封必须分开处置：前者只是「现在不该问」，等就行；后者才需要把详情页
// 路径降档 30 分钟。混为一谈会自锁——队列越闲、越没人真的问过豆瓣，详情页却一直被降档。
func TestIsRemoteAntiCrawlDistinguishesLocalGateFromRealBlock(t *testing.T) {
	if isRemoteAntiCrawl(blockedError("https://movie.douban.com/subject/1292052/", "反爬熔断静默中")) {
		t.Error("本地闸门拦下的请求被判成了远端封禁")
	}
	remote := &doubanFetchError{AntiCrawl: true, StatusCode: 403}
	if !isRemoteAntiCrawl(remote) {
		t.Error("豆瓣真的回了反爬页却没有触发详情页降档")
	}
	if !isRemoteAntiCrawl(fmt.Errorf("解析详情失败: %w", remote)) {
		t.Error("错误被包装后分类丢了")
	}
	if isRemoteAntiCrawl(errors.New("context deadline exceeded")) {
		t.Error("普通传输错误不该按反爬处理")
	}
	if isRemoteAntiCrawl(&doubanFetchError{StatusCode: 404}) {
		t.Error("普通的 404 不该按反爬处理")
	}
}

func TestChartItemsFromGlobalRowsPreservesFallbackMetadata(t *testing.T) {
	items := chartItemsFromGlobalRows([]db.GlobalVideoRow{{
		DoubanId:    "12345",
		VodName:     "示例电影",
		Pic:         "https://example.com/poster.jpg",
		DoubanScore: "8.2",
		DoubanVotes: "1000",
		Year:        "2026",
		Area:        "中国大陆",
		ReleaseDate: "2026-01-01",
	}})
	if len(items) != 1 {
		t.Fatalf("chartItemsFromGlobalRows() returned %d items, want 1", len(items))
	}
	item := items[0]
	if item.SubjectID != "12345" || item.Title != "示例电影" || item.PosterURL == "" {
		t.Fatalf("fallback item identity = %#v", item)
	}
	if item.Rating != "8.2" || item.Votes != "1000" || item.Info != "2026-01-01 / 中国大陆" {
		t.Fatalf("fallback item metadata = %#v", item)
	}
}

func TestParseSearchCandidatesToleratesSearchMarkupVariants(t *testing.T) {
	const fixture = `<html><body>
<div class='item-root result-item'>
  <div data-moreurl="/movie/subject_search?subject_id:\"35861087\"&is_tv=1">
    <a href="#" data-role="result" class="title-text highlighted">万界独尊 第一季 (2024)</a>
    <div class="abstract meta">中国大陆 / 动画 / 2024</div>
  </div>
</div>
</body></html>`

	candidates := parseSearchCandidates(fixture)
	if len(candidates) != 1 {
		t.Fatalf("parseSearchCandidates() returned %d candidates, want 1", len(candidates))
	}
	if candidates[0].SubjectID != "35861087" {
		t.Fatalf("SubjectID = %q, want %q", candidates[0].SubjectID, "35861087")
	}
	if candidates[0].Year != 2024 {
		t.Fatalf("Year = %d, want %d", candidates[0].Year, 2024)
	}
	if candidates[0].Title != "万界独尊 第一季" {
		t.Fatalf("Title = %q, want %q", candidates[0].Title, "万界独尊 第一季")
	}
}

func TestParseSearchCandidatesFallsBackToSubjectAnchors(t *testing.T) {
	const fixture = `<a class="title-text" href="https://movie.douban.com/subject/1295644/" title="这个杀手不太冷">这个杀手不太冷</a>`

	candidates := parseSearchCandidates(fixture)
	if len(candidates) != 1 {
		t.Fatalf("parseSearchCandidates() returned %d candidates, want 1", len(candidates))
	}
	if candidates[0].SubjectID != "1295644" || candidates[0].Title != "这个杀手不太冷" {
		t.Fatalf("candidate = %#v, want subject 1295644 and the expected title", candidates[0])
	}
}

func TestParseSearchCandidatesMatchesLiveDoubanMarkup(t *testing.T) {
	const fixture = `<div class="item-root">
  <a href="https://movie.douban.com/subject/35426411/" data-moreurl="onclick=&quot;moreurl(this,{from:'mv_subject_search',subject_id:'35426411',query:'%E4%B8%87%E7%95%8C%E7%8B%AC%E5%B0%8A',i:'1',is_tv:'1'})&quot;" class="cover-link"><img src="https://img3.doubanio.com/view/photo/s_ratio_poster/public/p2639185077.webp" alt="万界独尊 第一季‎ (2021)" class="cover"></a>
  <div class="detail"><div class="title"><a href="https://movie.douban.com/subject/35426411/" data-moreurl="onclick=&quot;moreurl(this,{from:'mv_subject_search',subject_id:'35426411',query:'%E4%B8%87%E7%95%8C%E7%8B%AC%E5%B0%8A',i:'1',is_tv:'1'})&quot;" class="title-text">万界独尊 第一季‎ (2021)</a><span class="label">[剧集]</span></div>
    <div class="meta abstract">中国大陆 / 动画 / 万界独尊 第1-50集 / Ten Thousand Worlds / 10分钟</div>
    <div class="meta abstract_2">叶老酒 / 酱紫 / 王大伟 / 柳知萧 / 陆敏悦 Minyue Lu</div>
  </div>
</div>`

	candidates := parseSearchCandidates(fixture)
	if len(candidates) != 1 {
		t.Fatalf("parseSearchCandidates() returned %d candidates, want 1", len(candidates))
	}
	candidate := candidates[0]
	if candidate.SubjectID != "35426411" || candidate.Title != "万界独尊 第一季" || candidate.Year != 2021 {
		t.Fatalf("candidate = %#v, want live subject 35426411, title, and year 2021", candidate)
	}
	if !candidate.IsSeries {
		t.Fatal("IsSeries = false, want true for the live [剧集] markup")
	}
}

func TestIsDoubanChallengeURL(t *testing.T) {
	for _, location := range []string{
		"https://sec.douban.com/c?r=https%3A%2F%2Fmovie.douban.com%2Fsubject%2F35426411%2F",
		"https://movie.douban.com/captcha/verify",
		"https://accounts.douban.com/passport/login",
	} {
		if !isDoubanChallengeURL(location) {
			t.Fatalf("isDoubanChallengeURL(%q) = false, want true", location)
		}
	}
	if isDoubanChallengeURL("https://movie.douban.com/subject/35426411/") {
		t.Fatal("isDoubanChallengeURL() marked a normal subject URL as a challenge")
	}
}

// 2026-09-22 从 /j/subject_abstract 实抓的响应。详情页 HTML 被 sec.douban.com
// 的 JS 验证挡住时，这份 JSON 是唯一可用的兜底数据源。
const liveSubjectAbstract = `{"r":0,"subject":{"episodes_count":"6","star":3.0,"blacklisted":"available","title":"权力的游戏 第八季 Game of Thrones Season 8\u200e (2019)","url":"https:\/\/movie.douban.com\/subject\/26584183\/","collection_status":"","rate":"6.0","short_comment":{"content":"...","author":"柏林苍穹下"},"is_tv":true,"subtype":"TV","directors":["米格尔·萨普什尼克","大卫·努特尔"],"actors":["艾米莉亚·克拉克","基特·哈灵顿"],"duration":"60分钟(E1-E2)","region":"美国","playable":true,"id":"26584183","types":["剧情","奇幻","冒险"],"release_year":"2019"}}`

func TestParseSubjectAbstractFillsAvailableFields(t *testing.T) {
	info, err := parseSubjectAbstract("26584183", liveSubjectAbstract)
	if err != nil {
		t.Fatalf("parseSubjectAbstract: %v", err)
	}
	checks := map[string]string{
		"Rating":       info.Rating,
		"Director":     info.Director,
		"Actor":        info.Actor,
		"Genre":        info.Genre,
		"Country":      info.Country,
		"ReleaseDate":  info.ReleaseDate,
		"EpisodeCount": info.EpisodeCount,
		"Duration":     info.Duration,
		"SubjectID":    info.SubjectID,
	}
	want := map[string]string{
		"Rating":       "6.0",
		"Director":     "米格尔·萨普什尼克 / 大卫·努特尔",
		"Actor":        "艾米莉亚·克拉克 / 基特·哈灵顿",
		"Genre":        "剧情/奇幻/冒险",
		"Country":      "美国",
		"ReleaseDate":  "2019",
		"EpisodeCount": "6",
		"Duration":     "60分钟(E1-E2)",
		"SubjectID":    "26584183",
	}
	for field, got := range checks {
		if got != want[field] {
			t.Errorf("%s = %q, want %q", field, got, want[field])
		}
	}
	// 这个接口没有的字段必须留空，db.UpsertDoubanInfo 才会保留库里的旧值。
	if info.Votes != "" || info.PosterURL != "" || info.Writer != "" || info.Hotness != "" {
		t.Errorf("unavailable fields must stay empty, got votes=%q poster=%q writer=%q hotness=%q",
			info.Votes, info.PosterURL, info.Writer, info.Hotness)
	}
}

func TestParseSubjectAbstractRejectsUnusablePayload(t *testing.T) {
	for _, body := range []string{`{"r":1}`, `not json`, ``} {
		if _, err := parseSubjectAbstract("1", body); err == nil {
			t.Errorf("parseSubjectAbstract(%q) = nil error, want failure", body)
		}
	}
}

// 详情页被拦后不该再逐条白打网页：拦截期内只试探一次，过期后恢复试探，
// 网页重新可用时要立刻解除拦截期。
func TestDetailChallengeCooldownSkipsDoomedHTMLFetch(t *testing.T) {
	t.Cleanup(clearDetailChallenged)

	clearDetailChallenged()
	if !detailProbeAllowed() {
		t.Fatal("detail probe must be allowed before any challenge is recorded")
	}

	markDetailChallenged()
	if detailProbeAllowed() {
		t.Fatal("detail probe must be suppressed during the cooldown")
	}

	detailChallengedUntil.Store(time.Now().Add(-time.Second).UnixNano())
	if !detailProbeAllowed() {
		t.Fatal("expired cooldown must allow a new probe")
	}

	markDetailChallenged()
	clearDetailChallenged()
	if !detailProbeAllowed() {
		t.Fatal("a successful HTML fetch must clear the cooldown")
	}
}

func TestSecFetchSite(t *testing.T) {
	cases := map[string]string{
		"https://movie.douban.com/|movie.douban.com":  "same-origin",
		"https://movie.douban.com/|search.douban.com": "same-site",
		"https://movie.douban.com/|www.google.com":    "cross-site",
		"|movie.douban.com":                           "none",
	}
	for tc, want := range cases {
		parts := strings.SplitN(tc, "|", 2)
		if got := secFetchSite(parts[0], parts[1]); got != want {
			t.Errorf("secFetchSite(%q, %q) = %q, want %q", parts[0], parts[1], got, want)
		}
	}
}
