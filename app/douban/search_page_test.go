package douban

import (
	"strings"
	"testing"
)

// 2026-09-27 从 movie.douban.com/subject_search 抓下来的真实页面结构（标题、ID、
// 摘要按原样保留，人名截短以免测试文件比被测逻辑还长）。这一份是离线兜底：
// live 用例只在 CCZJ_DOUBAN_LIVE=1 时跑，结构再变也要先在这里红一次。
const liveSearchPageFixture = `<html><body>
<div class="article-content"></div>
<script type="text/javascript">
    window.__DATA__ = {"count": 15, "error_info": "", "items": [
      {"abstract": "英国 / 喜剧 / 遵命，部长 / 部长大人(台) / 30分钟",
       "abstract_2": "彼得·惠特莫尔 / Sydney Lotterby / 保罗·爱丁顿 / 奈杰尔·霍桑",
       "cover_url": "https://img3.doubanio.com/view/photo/s_ratio_poster/public/p2187837239.webp",
       "id": 4937405,
       "labels": [{"color": "#00ad3f", "text": "剧集"}, {"color": "#00ad3f", "text": "可播放"}],
       "more_url": "onclick=\"moreurl(this,{from:'mv_subject_search',subject_id:'4937405',query:'%E6%98%AF%EF%BC%8C%E5%A4%A7%E8%87%A3',i:'0',is_tv:'1'})\"",
       "rating": {"count": 79757, "star_count": 5.0, "value": 9.8},
       "title": "是，大臣  第一季 Yes Minister Season 1\u200e (1980)",
       "tpl_name": "search_subject",
       "url": "https://movie.douban.com/subject/4937405/"},
      {"abstract": "英国 / 喜剧 / 遵命，部长 第三季 / 是，大臣 第三季 / 30分钟",
       "abstract_2": "西德尼·洛特比 / 彼得·惠特莫尔 / 保罗·爱丁顿 / 奈杰尔·霍桑",
       "id": 4933235,
       "labels": [{"color": "#00ad3f", "text": "剧集"}],
       "more_url": "onclick=\"moreurl(this,{from:'mv_subject_search',subject_id:'4933235',query:'%E6%98%AF%EF%BC%8C%E5%A4%A7%E8%87%A3',i:'1',is_tv:'1'})\"",
       "rating": {"count": 37737, "star_count": 5.0, "value": 9.8},
       "title": "是，大臣  第三季 Yes Minister Season 3\u200e (1982)",
       "tpl_name": "search_subject",
       "url": "https://movie.douban.com/subject/4933235/"},
      {"abstract": "英国 / 喜剧 / 遵命，部长 第二季 / 是，大臣 第二季 / 30分钟",
       "abstract_2": "Peter Whitmore / Sydney Lotterby / 保罗·爱丁顿 / 奈杰尔·霍桑",
       "id": 4933194,
       "labels": [{"color": "#00ad3f", "text": "剧集"}],
       "more_url": "onclick=\"moreurl(this,{from:'mv_subject_search',subject_id:'4933194',query:'%E6%98%AF%EF%BC%8C%E5%A4%A7%E8%87%A3',i:'2',is_tv:'1'})\"",
       "rating": {"count": 39040, "star_count": 5.0, "value": 9.8},
       "title": "是，大臣  第二季 Yes Minister Season 2\u200e (1981)",
       "tpl_name": "search_subject",
       "url": "https://movie.douban.com/subject/4933194/"},
      {"abstract": "英国 / 喜剧 / 是，大臣：党派之戏 / 60分钟",
       "abstract_2": "彼得·惠特莫尔 / 保罗·爱丁顿 / 奈杰尔·霍桑",
       "id": 26725031,
       "labels": [],
       "more_url": "onclick=\"moreurl(this,{from:'mv_subject_search',subject_id:'26725031',query:'%E6%98%AF%EF%BC%8C%E5%A4%A7%E8%87%A3',i:'3',is_tv:'0'})\"",
       "rating": {"count": 18049, "star_count": 5.0, "value": 9.8},
       "title": "是，大臣 1984圣诞特辑 Yes, Minister: Party Games\u200e (1984)",
       "tpl_name": "search_subject",
       "url": "https://movie.douban.com/subject/26725031/"},
      {"abstract": "英国 / 纪录片 / Comedy Connections-Season 6, Episode2 / 40分钟",
       "abstract_2": "Ellen-Raissa Jackson / Doon Mackichan",
       "id": 3541903,
       "labels": [],
       "more_url": "onclick=\"moreurl(this,{from:'mv_subject_search',subject_id:'3541903',query:'%E6%98%AF%EF%BC%8C%E5%A4%A7%E8%87%A3',i:'4',is_tv:'0'})\"",
       "rating": {"count": 498, "star_count": 4.5, "value": 9.4},
       "title": "喜剧联结-是大臣专题 Comedy Connections: Yes Minister\u200e (2008)",
       "tpl_name": "search_subject",
       "url": "https://movie.douban.com/subject/3541903/"}],
     "report": {"qtype": "194", "tags": "电影"}, "start": 0, "text": "是，大臣", "total": 5};
    window.__USER__ = { }
</script>
</body></html>`

func candidateByScore(candidates []SearchCandidate, meta SearchMeta, subjectID string) (int, bool) {
	for i := range candidates {
		if candidates[i].SubjectID == subjectID {
			return scoreCandidate(&candidates[i], meta), true
		}
	}
	return 0, false
}

func TestParseSearchCandidatesUsesEmbeddedJSON(t *testing.T) {
	candidates := parseSearchCandidates(liveSearchPageFixture)
	if len(candidates) != 5 {
		t.Fatalf("解析出 %d 条候选，页面里有 5 条: %#v", len(candidates), candidates)
	}
	var season2 SearchCandidate
	for _, c := range candidates {
		if c.SubjectID == "4933194" {
			season2 = c
		}
	}
	if season2.SubjectID == "" {
		t.Fatal("没拿到第二季的候选")
	}
	if season2.Year != 1981 {
		t.Errorf("Year = %d，期望 1981", season2.Year)
	}
	if !season2.IsSeries {
		t.Error("IsSeries = false，labels 里有「剧集」")
	}
	if strings.ContainsRune(season2.Title, '\u200e') {
		t.Errorf("Title 还留着 U+200E: %q", season2.Title)
	}
	// 页面上是「大臣␣␣第二季」，cleanTitle 里的 stripHTMLTags 会把连续空白压成一个空格。
	if want := "是，大臣 第二季 Yes Minister Season 2"; season2.Title != want {
		t.Errorf("Title = %q，期望 %q", season2.Title, want)
	}
	if season2.Director == "" || season2.Actor == "" {
		t.Errorf("导演/演员没拆开: director=%q actor=%q", season2.Director, season2.Actor)
	}
}

// 这份 fixture 同时钉住 C1/C2 的两端：要哪一季就选哪一季，其余四条一分都过不了线。
func TestEmbeddedJSONSearchDistinguishesSeasonsAndRejectsTheRest(t *testing.T) {
	candidates := parseSearchCandidates(liveSearchPageFixture)
	meta := SearchMeta{VodName: "是，大臣 第二季"}

	best, score := bestMatch(candidates, meta)
	if best == nil {
		t.Fatal("没有选出候选")
	}
	if best.SubjectID != "4933194" {
		t.Fatalf("选中的是 %s (%q)，第二季没被认出来", best.SubjectID, best.Title)
	}
	if score < doubanMatchThreshold {
		t.Fatalf("选对了片却只有 %d 分，低于阈值 %d", score, doubanMatchThreshold)
	}

	for _, id := range []string{"4937405", "4933235", "26725031", "3541903"} {
		got, ok := candidateByScore(candidates, meta, id)
		if !ok {
			t.Fatalf("候选列表里没有 %s", id)
		}
		if got >= doubanMatchThreshold {
			t.Errorf("%s 拿了 %d 分，越过了阈值 %d", id, got, doubanMatchThreshold)
		}
	}
}

// window.__DATA__ 在但 JSON 读不动（响应被截断、结构又改）时必须退回 HTML 路径，
// 而不是把老页面也一起判成零候选。
func TestBrokenEmbeddedJSONFallsBackToHTMLParser(t *testing.T) {
	page := `<script>window.__DATA__ = {"items": [{"title": "万界独尊</script>` +
		`<div class="item-root"><a class="title-text" href="https://movie.douban.com/subject/35426411/">万界独尊 第一季 (2021)</a></div>`
	candidates := parseSearchCandidates(page)
	if len(candidates) != 1 {
		t.Fatalf("退回 HTML 路径后拿到 %d 条候选: %#v", len(candidates), candidates)
	}
	if candidates[0].SubjectID != "35426411" {
		t.Fatalf("SubjectID = %q", candidates[0].SubjectID)
	}
}

// more_url 里只有一份 ID 时的取值路径；url 缺失也不该丢掉整条候选。
func TestSearchPageItemPrefersURLThenMoreURL(t *testing.T) {
	item := searchPageItem{
		URL:     "https://movie.douban.com/subject/1295644/",
		MoreURL: "moreurl(this,{subject_id:'99999999'})",
	}
	if got := item.subjectID(); got != "1295644" {
		t.Errorf("subjectID = %q，url 在时应优先于 more_url", got)
	}
	item.URL = ""
	if got := item.subjectID(); got != "99999999" {
		t.Errorf("subjectID = %q，url 缺失时应退回 more_url", got)
	}
	item.MoreURL = "subject_id:'not-a-number'"
	if got := item.subjectID(); got != "" {
		t.Errorf("subjectID = %q，取不到数字 ID 时应返回空", got)
	}
}

// 连打两次搜索之后拿到的限流页（2026-09-27 实抓）：HTTP 200、页面骨架正常、items 空，
// 唯一的信息在 error_info 里，而且是 \u 转义过的中文。
const liveThrottlePageFixture = `<html><body>
<script>window.__DATA__ = {"total": 0, "start": 0, "count": 15, "error_info": "\u641c\u7d22\u8bbf\u95ee\u592a\u9891\u7e41\u3002", "items": [], "text": "\u662f\uff0c\u5927\u81e3", "report": {"qtype": "194", "tags": "\u7535\u5f71"}};</script>
</body></html>`

func TestThrottledSearchPageStillReadsAsThrottled(t *testing.T) {
	if strings.Contains(liveThrottlePageFixture, "搜索访问太频繁") {
		t.Fatal("fixture 里已经出现明文中文了，这个用例的前提（明文字节不在页面里）不再成立")
	}
	// 这正是必须读 error_info 的理由：老防线只看字节流，转义后的提示它永远匹配不到。
	if checkAntiCrawl(liveThrottlePageFixture) {
		t.Fatal("checkAntiCrawl 现在能认出这页了，本用例的注释需要同步更新")
	}
	payload, ok := decodeSearchPage(liveThrottlePageFixture)
	if !ok {
		t.Fatal("内嵌数据没读出来")
	}
	if payload.ErrorInfo != "搜索访问太频繁。" {
		t.Errorf("ErrorInfo = %q，期望解码后的限流提示", payload.ErrorInfo)
	}
	if len(payload.Items) != 0 {
		t.Errorf("限流页 items 有 %d 条，应为空", len(payload.Items))
	}
	if got := parseSearchCandidates(liveThrottlePageFixture); len(got) != 0 {
		t.Errorf("限流页解析出 %d 条候选，应为空", len(got))
	}
}

// 内嵌 JSON 把斜杠转义成 \/、非 ASCII 转义成 \u，于是「页面里有结果链接」那四条判据
// 一条都匹配不到 \/subject\/——只剩候选本身能作证。豆瓣把限流提示印在页面上、同时又
// 给出可用候选是有的：手里已经有结果，就不该被一句提示整页作废。
func TestEscapedSearchPageWithCandidatesIsNotAntiCrawl(t *testing.T) {
	payload := `<script>window.__DATA__ = {"count": 1, "error_info": "", "items": [
	  {"abstract": "\u82f1\u56fd / \u5267\u60c5",
	   "id": 1292052,
	   "title": "\u7f57\u9a6c\u65e0\u6570 Roman Holiday\u200e (1953)",
	   "url": "https:\/\/movie.douban.com\/subject\/1292052\/"}]};</script>`
	shell := `<html><body><div class="global-tip">访问过于频繁，请稍后再试</div>` +
		`<div class="article-content"></div></body></html>`
	page := strings.Replace(shell, `</body>`, payload+`</body>`, 1)

	// 前提：同一份外壳（没有候选时）确实会被判成反爬——链接判据全空、明文关键词命中。
	// 这样断言的变化就只能归因于内嵌 JSON 那条例外的防线。
	if !checkAntiCrawl(shell) {
		t.Fatal("外壳页没被判成反爬，本用例的对照失效")
	}
	if checkAntiCrawl(page) {
		t.Error("有候选的页面被判成了反爬，整批搜索结果会被丢掉")
	}
	candidates := parseSearchCandidates(page)
	if len(candidates) != 1 || candidates[0].SubjectID != "1292052" {
		t.Fatalf("候选解析 = %#v，期望 1 条 1292052", candidates)
	}
}

// 反过来：没有候选的可疑页仍然要判成反爬，否则新防线会把真封一起放过。
func TestCaptchaPageWithoutCandidatesIsStillAntiCrawl(t *testing.T) {
	if !checkAntiCrawl(`<html><body><div class="article-content">请完成安全验证</div></body></html>`) {
		t.Error("验证页没被判成反爬")
	}
}
