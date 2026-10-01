package douban

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"
)

// 这一组用例钉的是「缓存过期了先摆旧的、新的在后台悄悄换」这层语义，
// 全部走 fetchChartPage / fetchCommentsPage 替身位，一条豆瓣请求都不发。

// —— 测试夹具：结构够 parseDoubanChart 认出来就行，不求逐字还原豆瓣页面。 ————

func chartFixtureHTML(subjectID, title string) string {
	return fmt.Sprintf(`<table><tr class="item">
<td class="cover"><a href="https://movie.douban.com/subject/%s/"><img src="https://img.doubanio.com/p.jpg"></a></td>
<td class="info"><span class="pl2"><a href="https://movie.douban.com/subject/%s/">%s</a></span>
<p>2024-01-01 / 中国大陆 / 剧情</p>
<span class="rating_nums">8.8</span> <span class="votes">(1234人评价)</span></td>
</tr></table>`, subjectID, subjectID, title)
}

func chartItem(id, title string) DoubanChartItem {
	return DoubanChartItem{SubjectID: id, Title: title, Rating: "8.8", Votes: "1234"}
}

func commentPage(marker string) *DoubanCommentsResp {
	return &DoubanCommentsResp{
		Comments: []DoubanComment{{ID: marker, Content: marker}},
		Total:    1,
	}
}

// waitCachedComment 等后台那一趟刷新真正落进缓存。替身位里的 done 只表示「数据取回了」，
// 写缓存发生在它返回之后，拿 done 直接断言会偶发读到旧的一页。
func waitCachedComment(t *testing.T, key, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		commentsCache.RLock()
		entry, has := commentsCache.entries[key]
		commentsCache.RUnlock()
		if has && len(entry.data.Comments) > 0 && entry.data.Comments[0].ID == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("后台刷新没落地：缓存 %s 仍不是 %q", key, want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// —— 状态隔离 ————

// isolateChartCache 保存并还原热榜的全部全局状态，用例之间不会互相污染。
func isolateChartCache(t *testing.T) {
	t.Helper()
	chartCacheMu.RLock()
	data, at, failure := chartCacheData, chartCacheTime, chartFailureTime
	chartCacheMu.RUnlock()
	oldFetch := fetchChartPage
	t.Cleanup(func() {
		chartCacheMu.Lock()
		chartCacheData, chartCacheTime, chartFailureTime = data, at, failure
		chartCacheMu.Unlock()
		fetchChartPage = oldFetch
	})
}

// isolateComments 同理，管住评论缓存和后台刷新的去重状态。
func isolateComments(t *testing.T) {
	t.Helper()
	commentsCache.RLock()
	entries := commentsCache.entries
	commentsCache.RUnlock()
	commentRefresh.Lock()
	running, blocked := commentRefresh.running, commentRefresh.blocked
	commentRefresh.Unlock()
	oldFetch := fetchCommentsPage
	t.Cleanup(func() {
		commentsCache.Lock()
		commentsCache.entries = entries
		commentsCache.Unlock()
		commentRefresh.Lock()
		commentRefresh.running, commentRefresh.blocked = running, blocked
		commentRefresh.Unlock()
		fetchCommentsPage = oldFetch
	})
}

// —— 热榜 ————

func TestStaleChartServesOldThenRevalidatesInBackground(t *testing.T) {
	isolateChartCache(t)
	clearRateClock()

	setChartCache([]DoubanChartItem{chartItem("111111", "旧榜单")}, time.Now().Add(-2*time.Hour))

	var calls atomic.Int32
	started := make(chan struct{}, 4)
	fetchChartPage = func(string) (string, error) {
		calls.Add(1)
		started <- struct{}{}
		time.Sleep(80 * time.Millisecond) // 慢成一次真实抓取，好让「没等」可测
		return chartFixtureHTML("222222", "新榜单"), nil
	}

	start := time.Now()
	items, err := FetchDoubanChart()
	if err != nil {
		t.Fatalf("有旧榜单时不该报错: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 80*time.Millisecond {
		t.Fatalf("过期热榜仍在等网络，耗时 %s", elapsed)
	}
	if len(items) != 1 || items[0].SubjectID != "111111" {
		t.Fatalf("应先返回旧榜单: %+v", items)
	}

	if _, ok := waitSignal(started, 2*time.Second); !ok {
		t.Fatal("后台没有发起刷新")
	}
	if got := waitForChartSubject(t, "222222"); got != "222222" {
		t.Fatalf("后台刷新没落地，拿到 %s", got)
	}
	if n := int(calls.Load()); n != 1 {
		t.Fatalf("取数次数=%d, want 1", n)
	}
}

func TestChartBackgroundRefreshSkippedWhenOneIsRunning(t *testing.T) {
	isolateChartCache(t)
	clearRateClock()
	setChartCache([]DoubanChartItem{chartItem("111111", "旧榜单")}, time.Now().Add(-2*time.Hour))

	var calls atomic.Int32
	fetchChartPage = func(string) (string, error) {
		calls.Add(1)
		return chartFixtureHTML("222222", "新榜单"), nil
	}

	// 已经有一趟在跑（占着 chartFetchMu）：后台刷新必须直接放弃，而不是排队再打一次。
	chartFetchMu.Lock()
	if _, err := refreshChart(false); err != nil {
		t.Fatalf("跳过的一轮不该报错: %v", err)
	}
	chartFetchMu.Unlock()

	if n := int(calls.Load()); n != 0 {
		t.Fatalf("抢不到锁还去抓取，次数=%d", n)
	}
}

func TestChartWithoutDataStillWaits(t *testing.T) {
	isolateChartCache(t)
	clearRateClock()
	setChartCache([]DoubanChartItem{}, time.Time{})

	fetchChartPage = func(string) (string, error) {
		time.Sleep(60 * time.Millisecond)
		return chartFixtureHTML("333333", "首次榜单"), nil
	}
	items, err := FetchDoubanChart()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].SubjectID != "333333" {
		t.Fatalf("手里没数据时应等到实抓结果: %+v", items)
	}
}

// —— 评论 ————

func TestStaleCommentsServeOldThenRevalidate(t *testing.T) {
	isolateComments(t)
	seedComments("2400001_1_new_score", commentPage("旧评论"), 25*time.Hour)

	var calls atomic.Int32
	done := make(chan struct{}, 4)
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		calls.Add(1)
		time.Sleep(60 * time.Millisecond)
		done <- struct{}{}
		return commentPage("新评论"), nil
	}

	start := time.Now()
	resp, err := FetchComments("2400001", 1, "new_score")
	if err != nil {
		t.Fatalf("有旧评论时不该报错: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= 60*time.Millisecond {
		t.Fatalf("过期评论仍在等网络，耗时 %s", elapsed)
	}
	if resp.Comments[0].ID != "旧评论" {
		t.Fatalf("应先返回旧评论: %+v", resp.Comments[0])
	}

	<-done
	waitCachedComment(t, "2400001_1_new_score", "新评论")
	again, err := FetchComments("2400001", 1, "new_score")
	if err != nil {
		t.Fatal(err)
	}
	if again.Comments[0].ID != "新评论" {
		t.Fatalf("后台刷新没落地: %+v", again.Comments[0])
	}
	if n := int(calls.Load()); n != 1 {
		t.Fatalf("取数次数=%d, want 1", n)
	}
}

func TestCommentRefreshFailureBacksOff(t *testing.T) {
	isolateComments(t)
	seedComments("2400002_1_new_score", commentPage("旧评论"), 25*time.Hour)

	var calls atomic.Int32
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		calls.Add(1)
		return nil, fmt.Errorf("源站不通")
	}

	for i := 0; i < 3; i++ {
		resp, err := FetchComments("2400002", 1, "new_score")
		if err != nil || resp.Comments[0].ID != "旧评论" {
			t.Fatalf("第 %d 次读应拿到旧评论: resp=%+v err=%v", i+1, resp, err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 一次失败换一段退避：三次读只该撞一趟网络，而不是每次打开都白打一遍豆瓣。
	if n := int(calls.Load()); n != 1 {
		t.Fatalf("失败后仍在反复刷新，次数=%d", n)
	}
}

func TestFreshCommentsNeverReachTheNetwork(t *testing.T) {
	isolateComments(t)
	seedComments("2400003_1_new_score", commentPage("新一点的旧评论"), time.Hour)

	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		t.Error("缓存未过期不该发请求")
		return nil, fmt.Errorf("不该被调用")
	}
	resp, err := FetchComments("2400003", 1, "new_score")
	if err != nil || resp.Comments[0].ID != "新一点的旧评论" {
		t.Fatalf("resp=%+v err=%v", resp, err)
	}
}

func TestCommentsBeyondCeilingWaitForNetwork(t *testing.T) {
	isolateComments(t)
	// 超过上限的旧数据不再展示：这时让调用方等一次实抓，比给他一页三年前的评论好。
	seedComments("2400004_1_new_score", commentPage("太旧的评论"), commentStaleCeiling+time.Hour)

	called := make(chan struct{}, 2)
	fetchCommentsPage = func(string, int, string) (*DoubanCommentsResp, error) {
		called <- struct{}{}
		return commentPage("刚抓的评论"), nil
	}
	resp, err := FetchComments("2400004", 1, "new_score")
	if err != nil {
		t.Fatal(err)
	}
	if resp.Comments[0].ID != "刚抓的评论" {
		t.Fatalf("过上限后应等到实抓结果: %+v", resp.Comments[0])
	}
	<-called
}

// —— 小工具 ————

func setChartCache(items []DoubanChartItem, at time.Time) {
	chartCacheMu.Lock()
	chartCacheData, chartCacheTime = items, at
	chartFailureTime = time.Time{}
	chartCacheMu.Unlock()
}

func waitForChartSubject(t *testing.T, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		chartCacheMu.RLock()
		var got string
		if len(chartCacheData) > 0 {
			got = chartCacheData[0].SubjectID
		}
		chartCacheMu.RUnlock()
		if got == want {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func seedComments(key string, resp *DoubanCommentsResp, age time.Duration) {
	commentsCache.Lock()
	commentsCache.entries = map[string]commentCacheEntry{}
	commentsCache.entries[key] = commentCacheEntry{data: resp, fetchedAt: time.Now().Add(-age)}
	commentsCache.Unlock()
	commentRefresh.Lock()
	commentRefresh.running = map[string]bool{}
	commentRefresh.blocked = map[string]time.Time{}
	commentRefresh.Unlock()
}

func waitSignal(ch chan struct{}, timeout time.Duration) (struct{}, bool) {
	select {
	case v := <-ch:
		return v, true
	case <-time.After(timeout):
		return struct{}{}, false
	}
}
