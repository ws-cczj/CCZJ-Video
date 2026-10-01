package detail

import (
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// 过期条目的正确行为是「立刻给旧的」，所以这些用例都拿一个明显很慢的 fetchFn 来钉住：
// 只要读被网络拖住，耗时断言就会红，而不是靠运气。
const swrFetchDelay = 80 * time.Millisecond

// genService 造一个每次取数换一代的服务，世代号写在 VodScore 里，读出来就知道拿到的是哪一次。
func genService(ttl time.Duration, failFrom int) (*Service, *atomic.Int32, chan int) {
	s := New(ttl)
	s.retryDelay = func(int) time.Duration { return 0 }
	var calls atomic.Int32
	done := make(chan int, 16)
	s.fetchFn = func(c *db.CatalogItem) (*Result, error) {
		n := int(calls.Add(1))
		time.Sleep(swrFetchDelay)
		if failFrom > 0 && n >= failFrom {
			done <- n
			return nil, &collect.HTTPError{StatusCode: 404}
		}
		done <- n
		return &Result{
			Video:   &model.Video{GlobalId: c.GlobalID, VodName: c.VodName, VodScore: model.FlexibleString(strconv.Itoa(n))},
			Catalog: c,
		}, nil
	}
	return s, &calls, done
}

func score(r *Result) string {
	if r == nil || r.Video == nil {
		return "<nil>"
	}
	return string(r.Video.VodScore)
}

// waitForScore 反复读直到拿到期望世代：后台那一趟是先回信号再落缓存的，所以要等一小会儿。
func waitForScore(t *testing.T, s *Service, catalog *db.CatalogItem, want string) *Result {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		r, err := s.getCatalog(catalog)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if score(r) == want {
			return r
		}
		if time.Now().After(deadline) {
			t.Fatalf("后台刷新没有落地，当前世代 %s，期望 %s", score(r), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestStaleDetailServesOldThenRevalidatesInBackground(t *testing.T) {
	s, calls, done := genService(20*time.Millisecond, 0)
	catalog := &db.CatalogItem{SourceKey: "swr", GlobalID: 11, SourceVodID: "v", VodName: "title"}

	// 冷启动没有旧数据可给，只能等这一次实抓。
	first, err := s.getCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if score(first) != "1" {
		t.Fatalf("首次应拿到第 1 代，实际 %s", score(first))
	}
	<-done

	time.Sleep(30 * time.Millisecond) // 越过 20ms 的软 TTL

	start := time.Now()
	stale, err := s.getCatalog(catalog)
	if err != nil {
		t.Fatalf("过期不该把错误抛给调用方: %v", err)
	}
	if elapsed := time.Since(start); elapsed >= swrFetchDelay {
		t.Fatalf("过期条目仍在等网络，耗时 %s", elapsed)
	}
	if score(stale) != "1" {
		t.Fatalf("过期时应先返回旧数据，实际 %s", score(stale))
	}

	if n := <-done; n != 2 {
		t.Fatalf("后台没有补第二轮，取数次数 %d", n)
	}
	waitForScore(t, s, catalog, "2")
	if got := int(calls.Load()); got != 2 {
		t.Fatalf("取数次数=%d, want 2", got)
	}
}

func TestStaleDetailRefreshFailureKeepsServingOldData(t *testing.T) {
	// 第 2 次取数开始失败：模拟源站在后台刷新时挂了。
	s, calls, done := genService(20*time.Millisecond, 2)
	catalog := &db.CatalogItem{SourceKey: "swr", GlobalID: 12, SourceVodID: "v", VodName: "title"}

	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	<-done
	time.Sleep(30 * time.Millisecond)

	stale, err := s.getCatalog(catalog)
	if err != nil {
		t.Fatalf("后台刷新失败不该让调用方尝到错误: %v", err)
	}
	if score(stale) != "1" {
		t.Fatalf("失败时应继续给旧数据，实际 %s", score(stale))
	}
	if n := <-done; n != 2 {
		t.Fatalf("期望第二轮失败，取数次数 %d", n)
	}

	// 失败后旧条目被续了一个窗口：紧接着的读仍然立刻命中，不会每次都去撞坏源。
	start := time.Now()
	again, err := s.getCatalog(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if score(again) != "1" || time.Since(start) >= swrFetchDelay {
		t.Fatalf("失败后仍该立刻给出旧数据（世代 %s，耗时 %s）", score(again), time.Since(start))
	}
	if got := int(calls.Load()); got != 2 {
		t.Fatalf("失败后不该反复重开取数，次数=%d", got)
	}
}

func TestForceRefreshStillWaitsForNewData(t *testing.T) {
	s, _, done := genService(time.Minute, 0)
	catalog := &db.CatalogItem{SourceKey: "swr", GlobalID: 13, SourceVodID: "v", VodName: "title"}

	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	<-done

	// 缓存还很新鲜，但 refresh=true 是用户主动点的「重新解析」，必须真的等到底。
	start := time.Now()
	fresh, err := s.getCatalog(catalog, true)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed < swrFetchDelay {
		t.Fatalf("强制刷新直接返回了旧数据，耗时 %s", elapsed)
	}
	if score(fresh) != "2" {
		t.Fatalf("强制刷新应拿到第 2 代，实际 %s", score(fresh))
	}
}

func TestFreshnessSettingDrivesCacheTTL(t *testing.T) {
	if got := FreshnessTTL("fast"); got != 30*time.Second {
		t.Fatalf("fast=%s", got)
	}
	if got := FreshnessTTL("saver"); got != 10*time.Minute {
		t.Fatalf("saver=%s", got)
	}
	// 空值和没见过的值都归标准档：设置项以后加档位时，旧库里的陌生字面量不该让缓存失能。
	for _, raw := range []string{"", "  ", "standard", "nonsense"} {
		if got := FreshnessTTL(raw); got != 90*time.Second {
			t.Fatalf("FreshnessTTL(%q)=%s, want 90s", raw, got)
		}
	}

	// 读的是同一套 db.GetSetting：库里没这个键时退回构造值，所以 New(ttl) 仍然测得准。
	if err := db.InitDB(testDBDir); err != nil {
		t.Fatal(err)
	}
	// 收尾必须还原这一项：ttlNow 每次落缓存都读库，留着 "saver" 会把同包其它用例的短 TTL
	// 变成十分钟，过期分支再也测不到。
	prev, _ := db.GetSetting(SettingFreshness)
	t.Cleanup(func() { _ = db.SetSetting(SettingFreshness, prev) })
	if err := db.SetSetting(SettingFreshness, ""); err != nil {
		t.Fatal(err)
	}
	s := New(90 * time.Second)
	if got := s.ttlNow(); got != 90*time.Second {
		t.Fatalf("未设置时应退回构造值，实际 %s", got)
	}
	if err := db.SetSetting(SettingFreshness, "saver"); err != nil {
		t.Fatal(err)
	}
	if got := s.ttlNow(); got != 10*time.Minute {
		t.Fatalf("saver 档应立刻生效（免重启），实际 %s", got)
	}
}

// standardFreshness 把新鲜度设回未配置，让 New(ttl) 的短 TTL 真的生效：
// ttlNow 每次落缓存都读库，别的用例留在 "saver" 档会把 40 毫秒变成十分钟。
func standardFreshness(t *testing.T) {
	t.Helper()
	if err := db.InitDB(testDBDir); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(SettingFreshness, ""); err != nil {
		t.Fatal(err)
	}
}

// 诊断页把这几个数直接标成「命中率」展示，所以每一次读归进哪一格要由用例钉住：
// 界面等没等、有没有真发请求，都必须和读数对上。
func TestStatsClassifyFreshStaleAndForcedReads(t *testing.T) {
	standardFreshness(t)
	s, _, done := genService(40*time.Millisecond, 0)
	catalog := &db.CatalogItem{SourceKey: "stats", GlobalID: 21, SourceVodID: "v", VodName: "title"}

	// 冷启动：没人等过这一次，记 miss + fetch_ok。
	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	<-done
	if got := s.Stats(); got.Misses != 1 || got.FetchOK != 1 || got.Entries != 1 || got.Bytes <= 0 {
		t.Fatalf("首次读应记成未命中并落一条缓存: %+v", got)
	}

	// 新鲜命中：不发网络。
	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	if got := s.Stats(); got.Hits != 1 || got.FetchOK != 1 {
		t.Fatalf("新鲜读应只记命中: %+v", got)
	}

	// 过期命中：界面拿到旧数据没等，刷新在后台补一趟。
	time.Sleep(60 * time.Millisecond)
	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	if got := s.Stats(); got.StaleHits != 1 || got.Misses != 1 {
		t.Fatalf("过期读应记 stale_hits 且不算未命中: %+v", got)
	}
	if got := waitForStats(t, s, func(g Stats) bool { return g.FetchOK == 2 }); got.FetchOK != 2 {
		t.Fatalf("后台刷新成功要记第二个 fetch_ok: %+v", got)
	}

	// 强制刷新是用户主动等的，归 miss 而不是命中。
	if _, err := s.getCatalog(catalog, true); err != nil {
		t.Fatal(err)
	}
	if got := s.Stats(); got.Misses != 2 || got.Hits != 1 {
		t.Fatalf("强制刷新应记成未命中: %+v", got)
	}
}

func TestStatsCountFailedBackgroundRefresh(t *testing.T) {
	standardFreshness(t)
	// 第 2 次取数起失败：第 1 次落缓存，过期后那次后台刷新必然失败。
	s, _, done := genService(40*time.Millisecond, 2)
	catalog := &db.CatalogItem{SourceKey: "stats", GlobalID: 22, SourceVodID: "v", VodName: "title"}
	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	<-done

	time.Sleep(60 * time.Millisecond)
	if _, err := s.getCatalog(catalog); err != nil {
		t.Fatal(err)
	}
	got := waitForStats(t, s, func(g Stats) bool { return g.FetchFail == 1 })
	if got.FetchFail != 1 || got.StaleHits != 1 {
		t.Fatalf("后台刷新失败应只记 fetch_fail，旧数据照旧命中: %+v", got)
	}
}

func waitForStats(t *testing.T, s *Service, ok func(Stats) bool) Stats {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		got := s.Stats()
		if ok(got) {
			return got
		}
		if time.Now().After(deadline) {
			return got
		}
		time.Sleep(5 * time.Millisecond)
	}
}
