package douban

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/db"
)

// 真机验证 C1/C2：向豆瓣发一次真实搜索，确认「解析 → 折叠 → 季号参与打分 → 阈值」
// 这条链在实时页面上成立。默认跳过，只有 CCZJ_DOUBAN_LIVE=1 才发请求，
// 每个用例固定一个关键词，避免把这里变成刷豆瓣的入口。

func TestLiveSearchDistinguishesSeasons(t *testing.T) {
	if os.Getenv("CCZJ_DOUBAN_LIVE") == "" {
		t.Skip("set CCZJ_DOUBAN_LIVE=1 to run against douban")
	}
	const keyword = "是，大臣 第二季"
	// 搜索页用去掉季号的关键词：SearchSubjectID 的第一次尝试带季号时豆瓣经常
	// 什么都不回，第二次才是 stripSeasonInfo 后的这张页，季号区分正是在这里决胜。
	pageKeyword := stripSeasonInfo(keyword)
	if pageKeyword == "" {
		t.Fatalf("stripSeasonInfo(%q) 把关键词剥空了", keyword)
	}
	// 冷却写读会碰 global_video，绝不能落在用户的库上。句柄要到进程退出才放，
	// 所以用手工临时目录而不是 t.TempDir()，否则清理时删不掉。
	dir, err := os.MkdirTemp("", "cczj-douban-live-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := db.InitDB(dir); err != nil {
		t.Fatal(err)
	}

	params := url.Values{}
	params.Set("search_text", pageKeyword)
	params.Set("cat", "1002")
	html, err := fetchHTML(searchURL + "?" + params.Encode())
	if err != nil {
		t.Fatalf("抓取搜索页失败: %v", err)
	}
	if checkAntiCrawl(html) {
		t.Skip("豆瓣返回验证页，改期重试")
	}
	// 限流提示藏在 __DATA__ 的 error_info 里，页面本身看着完全正常。不先分出来的话，
	// 下面「0 条候选」的报错会把「我们被限流」误报成「豆瓣又改版了」。
	if payload, ok := decodeSearchPage(html); ok && payload.ErrorInfo != "" {
		t.Skipf("豆瓣回话 %q，改期重试", payload.ErrorInfo)
	}
	candidates := parseSearchCandidates(html)
	if len(candidates) == 0 {
		kept := filepath.Join(os.TempDir(), "cczj-douban-live-search.html")
		_ = os.WriteFile(kept, []byte(html), 0o644)
		t.Fatalf("实时页面解析出 0 条候选（页面结构可能又变了），HTML 长度 %d，已存 %s", len(html), kept)
	}
	meta := SearchMeta{VodName: keyword}
	for _, c := range candidates {
		base, season, part := splitSeasonTag(normalizeTitle(c.Title))
		t.Logf("候选 id=%s title=%q year=%d -> base=%q season=%d part=%q score=%d",
			c.SubjectID, c.Title, c.Year, base, season, part, scoreCandidate(&c, meta))
	}

	best, score := bestMatch(candidates, meta)
	if best == nil {
		t.Fatal("没有选出候选")
	}
	if _, season, _ := splitSeasonTag(normalizeTitle(best.Title)); season != 2 {
		t.Fatalf("选中的是 %q (score=%d)，第二季没被认出来", best.Title, score)
	}
	if score < doubanMatchThreshold {
		t.Fatalf("选对了片却只有 %d 分，低于阈值 %d：这条会被拒掉", score, doubanMatchThreshold)
	}
}
