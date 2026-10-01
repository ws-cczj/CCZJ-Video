package douban

import (
	"fmt"
	"testing"
	"time"
)

// 诊断页把这三块缓存的读数直接摆给用户看，所以「什么算命中、什么算跳过」必须由用例钉住，
// 而不是靠读代码推断。全部走抓取替身位，不发真实豆瓣请求。

func resetChartCounters(t *testing.T) {
	t.Helper()
	chartCounters = swrCounters{}
	t.Cleanup(func() { chartCounters = swrCounters{} })
}

func resetCommentCounters(t *testing.T) {
	t.Helper()
	commentsCounters = swrCounters{}
	t.Cleanup(func() { commentsCounters = swrCounters{} })
}

func TestChartCountersClassifyFreshStaleAndMiss(t *testing.T) {
	isolateChartCache(t)
	resetChartCounters(t)
	clearRateClock()
	fetchChartPage = func(string) (string, error) { return chartFixtureHTML("999999", "新榜单"), nil }

	// 新鲜：一次网络都不发。
	setChartCache([]DoubanChartItem{chartItem("111111", "新鲜榜单")}, time.Now())
	if _, err := FetchDoubanChart(); err != nil {
		t.Fatal(err)
	}
	if got := ChartCacheStats(); got.Hits != 1 || got.Entries != 1 || got.FetchOK != 0 {
		t.Fatalf("新鲜读应只记命中、不发抓取: %+v", got)
	}

	// 过期但仍能展示：记 stale_hits，刷新在后台补。
	setChartCache([]DoubanChartItem{chartItem("111111", "过期榜单")}, time.Now().Add(-2*time.Hour))
	if _, err := FetchDoubanChart(); err != nil {
		t.Fatal(err)
	}
	if got := ChartCacheStats(); got.StaleHits != 1 || got.Hits != 1 || got.Misses != 0 {
		t.Fatalf("过期读应记成过期命中且不算未命中: %+v", got)
	}
	// 等后台那趟落地再往下走，否则它和下面的未命中读会抢同一把抓取锁。
	if got := waitForChartStat(t, func(s SWR) bool { return s.FetchOK == 1 }); got.FetchOK != 1 {
		t.Fatalf("后台刷新成功要记 fetch_ok: %+v", got)
	}

	// 内存清空（单测环境也没有落库快照）才是真未命中：调用方必须等到实抓结果。
	setChartCache([]DoubanChartItem{}, time.Time{})
	if _, err := FetchDoubanChart(); err != nil {
		t.Fatal(err)
	}
	if got := ChartCacheStats(); got.Misses != 1 || got.FetchOK != 2 {
		t.Fatalf("空缓存读应记成未命中并再抓一趟: %+v", got)
	}
}

func TestChartCountersRecordSkipAndFailure(t *testing.T) {
	isolateChartCache(t)
	resetChartCounters(t)
	clearRateClock()

	setChartCache([]DoubanChartItem{chartItem("111111", "过期榜单")}, time.Now().Add(-2*time.Hour))
	var calls int
	fetchChartPage = func(string) (string, error) {
		calls++
		return chartFixtureHTML("222222", "新榜单"), nil
	}

	// 后台刷新抢不到锁 → skipped，一次都不抓。
	chartFetchMu.Lock()
	if _, err := refreshChart(false); err != nil {
		t.Fatal(err)
	}
	chartFetchMu.Unlock()
	if got := ChartCacheStats(); got.Skipped != 1 || calls != 0 {
		t.Fatalf("抢不到锁要记 skipped 且不发请求: %+v calls=%d", got, calls)
	}

	// 抓成功 → fetchOK。
	if _, err := refreshChart(false); err != nil {
		t.Fatal(err)
	}
	if got := ChartCacheStats(); got.FetchOK != 1 || got.FetchFail != 0 {
		t.Fatalf("抓取成功要记 fetch_ok: %+v", got)
	}

	// 抓失败 → fetchFail；随后的退避期内不再抓，记成 skipped。
	fetchChartPage = func(string) (string, error) { return "", fmt.Errorf("源站不通") }
	setChartCache([]DoubanChartItem{chartItem("222222", "过期榜单")}, time.Now().Add(-2*time.Hour))
	if _, err := refreshChart(false); err != nil {
		t.Fatal(err)
	}
	if got := ChartCacheStats(); got.FetchFail != 1 {
		t.Fatalf("抓取失败要记 fetch_fail: %+v", got)
	}
	before := calls
	if _, err := refreshChart(false); err != nil {
		t.Fatal(err)
	}
	if got := ChartCacheStats(); got.Skipped < 2 || calls != before {
		t.Fatalf("退避期内的一轮要记 skipped 且不撞豆瓣: %+v calls=%d", got, calls)
	}
}

func TestCommentCountersClassifyReads(t *testing.T) {
	isolateComments(t)
	resetCommentCounters(t)

	// 未过期 → hits，且不发请求。
	seedComments("2400010_1_new_score", commentPage("新评论"), time.Hour)
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		t.Error("未过期不该发请求")
		return nil, fmt.Errorf("不该被调用")
	}
	if _, err := FetchComments("2400010", 1, "new_score"); err != nil {
		t.Fatal(err)
	}
	if got := CommentCacheStats(); got.Hits != 1 || got.Entries != 1 {
		t.Fatalf("新鲜读应记成命中: %+v", got)
	}

	// 过期但仍能看 → stale_hits，后台抓成功后 fetch_ok。
	seedComments("2400011_1_new_score", commentPage("旧评论"), 25*time.Hour)
	done := make(chan struct{}, 1)
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		resp := commentPage("刷新后的评论")
		done <- struct{}{}
		return resp, nil
	}
	if _, err := FetchComments("2400011", 1, "new_score"); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := CommentCacheStats(); got.StaleHits != 1 || got.Misses != 0 {
		t.Fatalf("过期读应记成过期命中: %+v", got)
	}
	if got := waitForCommentStat(t, func(s SWR) bool { return s.FetchOK == 1 }); got.FetchOK != 1 {
		t.Fatalf("后台刷新成功要记 fetch_ok: %+v", got)
	}

	// 超过上限 → misses，实抓失败要把错误交给调用方并记 fetch_fail。
	seedComments("2400012_1_new_score", commentPage("太旧的评论"), commentStaleCeiling+time.Hour)
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		return nil, fmt.Errorf("源站不通")
	}
	if _, err := FetchComments("2400012", 1, "new_score"); err == nil {
		t.Fatal("实抓失败应把错误交给调用方")
	}
	if got := CommentCacheStats(); got.Misses != 1 || got.FetchFail != 1 {
		t.Fatalf("过上限后应记 miss 与 fetch_fail: %+v", got)
	}
}

func TestCommentCountersRecordBackgroundSkip(t *testing.T) {
	isolateComments(t)
	resetCommentCounters(t)

	seedComments("2400013_1_new_score", commentPage("旧评论"), 25*time.Hour)
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		return nil, fmt.Errorf("源站不通")
	}
	// 第一次读安排后台刷新，刷新失败后进入退避；后续读仍算过期命中，但只多算 skipped。
	for i := 0; i < 3; i++ {
		if _, err := FetchComments("2400013", 1, "new_score"); err != nil {
			t.Fatalf("有旧评论时不该报错: %v", err)
		}
		time.Sleep(30 * time.Millisecond)
	}
	got := CommentCacheStats()
	if got.StaleHits != 3 || got.Misses != 0 {
		t.Fatalf("三次过期读应记三次 stale_hits: %+v", got)
	}
	if got.FetchFail != 1 || got.Skipped != 2 {
		t.Fatalf("一趟失败换一段退避，另两轮应记 skipped: %+v", got)
	}
}

func waitForChartStat(t *testing.T, ok func(SWR) bool) SWR {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := ChartCacheStats()
		if ok(got) {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func waitForCommentStat(t *testing.T, ok func(SWR) bool) SWR {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := CommentCacheStats()
		if ok(got) {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}
