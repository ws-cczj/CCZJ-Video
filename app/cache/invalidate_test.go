package cache

import (
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// TestMain 抢在任何 applog 调用之前把日志单例和库都绑到临时目录：
// applog.Default() 会在单例为空时用 %APPDATA% 的生产目录建出来，测试一触发
// 失效日志就会灌进用户真实日志；db.InitDB 受 sync.Once 保护，所以整二进只此一次。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-cache-test-")
	if err != nil {
		panic(err)
	}
	if err := applog.Init(filepath.Join(dir, "applog")); err != nil {
		panic(err)
	}
	if err := db.InitDB(filepath.Join(dir, "db")); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// events 是失效事件的记录器。emit 在调用方 goroutine 里同步执行，锁只为满足 -race
// 对共享切片的要求，不代表这里真有并发。
type events struct {
	mu    sync.Mutex
	items []InvalidateEventPayload
}

func (e *events) record(name string, data any) {
	payload, ok := data.(InvalidateEventPayload)
	if !ok {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.items = append(e.items, payload)
}

func (e *events) all() []InvalidateEventPayload {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]InvalidateEventPayload, len(e.items))
	copy(out, e.items)
	return out
}

func (e *events) reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.items = nil
}

// capturePublisher 把失效事件接到测试里，结束后恢复原状——包级单例一旦被某个测试
// 永久改掉，后面的测试就会看见一份不属于自己的事件流。
func capturePublisher(t *testing.T) *events {
	t.Helper()
	rec := &events{}
	previous := publisher
	SetEventPublisher(rec.record)
	t.Cleanup(func() { SetEventPublisher(previous) })
	return rec
}

// sourceKeysOf 把事件里的源按字典序收拢，便于和「该失效哪几个源」的期望直接比。
func sourceKeysOf(items []InvalidateEventPayload) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, item.SourceKey)
	}
	sort.Strings(out)
	return out
}

func seedCatalog(t *testing.T, sourceKey, vodID, name string) int64 {
	t.Helper()
	if err := db.UpsertCatalogItems(sourceKey, []*model.Video{{
		VodId: model.FlexibleString(vodID), TypeId: "cache_test", TypeName: "Cache test type", VodName: name,
	}}); err != nil {
		t.Fatalf("seed %s:%s: %v", sourceKey, vodID, err)
	}
	item, err := db.GetCatalogItem(sourceKey, vodID)
	if err != nil {
		t.Fatalf("read seeded %s:%s: %v", sourceKey, vodID, err)
	}
	return item.GlobalID
}

func cleanSources(t *testing.T, keys ...string) {
	t.Helper()
	for _, key := range keys {
		if _, err := db.DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, key); err != nil {
			t.Fatal(err)
		}
	}
}

// 失效事件的作用域必须和调用方意图一一对应：单条只带 vod_id，整源不带列表。
// 空键是调用方还没解析出身份时的常见状态，不能把它广播成「清掉某个源」。
func TestInvalidateScopesAndGuardClauses(t *testing.T) {
	rec := capturePublisher(t)

	InvalidateVideo("cache_scope", "vod-1", 7, "unit")
	InvalidateSource("cache_scope", "recollect")

	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("events = %d, want 2: %+v", len(got), got)
	}
	if got[0].Scope != ScopeVideo || got[0].SourceKey != "cache_scope" || got[0].Reason != "unit" {
		t.Errorf("video event = %+v", got[0])
	}
	if len(got[0].VodIDs) != 1 || got[0].VodIDs[0] != "vod-1" {
		t.Errorf("video event vod_ids = %v, want [vod-1]", got[0].VodIDs)
	}
	if got[1].Scope != ScopeSource || got[1].SourceKey != "cache_scope" || got[1].Reason != "recollect" {
		t.Errorf("source event = %+v", got[1])
	}
	if len(got[1].VodIDs) != 0 {
		t.Errorf("source event vod_ids = %v, want empty", got[1].VodIDs)
	}

	InvalidateVideo("", "vod-1", 7, "unit")
	InvalidateVideo("cache_scope", "", 7, "unit")
	InvalidateSource("", "unit")
	if still := rec.all(); len(still) != 2 {
		t.Fatalf("空键也发了事件：%+v", still[2:])
	}
}

// 没接上事件通道（单测、启动早期）时只能清 Go 侧，不能 panic。
func TestEmitWithoutPublisher(t *testing.T) {
	previous := publisher
	SetEventPublisher(nil)
	t.Cleanup(func() { SetEventPublisher(previous) })
	InvalidateVideo("cache_nopub", "vod-1", 1, "unit")
	InvalidateSource("cache_nopub", "unit")
}

// 删—恢复—彻底删除这条链上，每步只广播一次，且失败的那次必须安静：
// 前端收到事件就会丢缓存，为一个没改动任何数据的失败清空整页是净损失。
func TestCatalogVideoLifecycleEvents(t *testing.T) {
	const sourceKey = "cache_lifecycle"
	cleanSources(t, sourceKey)
	defer cleanSources(t, sourceKey)
	rec := capturePublisher(t)

	globalID := seedCatalog(t, sourceKey, "vod-1", "Lifecycle title")
	if globalID <= 0 {
		t.Fatalf("seeded row has no global_id")
	}
	rec.reset()

	if err := DeleteCatalogVideo(sourceKey, "vod-1", "user delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.GetCatalogItem(sourceKey, "vod-1"); err == nil {
		t.Fatalf("软删除后目录仍读得到")
	}
	item, err := db.GetRecycleItem(sourceKey, "vod-1")
	if err != nil {
		t.Fatal(err)
	}
	if item.GlobalId != globalID {
		t.Errorf("回收站里的 global_id = %d, want %d（删除前解析的那条）", item.GlobalId, globalID)
	}
	assertSingleVideoEvent(t, rec, sourceKey, "vod-1", "user delete")

	if err := RestoreCatalogVideo(sourceKey, "vod-1", "user restore"); err != nil {
		t.Fatal(err)
	}
	if item, err := db.GetCatalogItem(sourceKey, "vod-1"); err != nil || item.GlobalID != globalID {
		t.Fatalf("恢复后读不到原行：item=%+v err=%v", item, err)
	}
	assertSingleVideoEvent(t, rec, sourceKey, "vod-1", "user restore")

	// 行已经是 active，再恢复一次必须失败：这条分支覆盖的是 requireRecycleRow，
	// 也是「重复点恢复」在前端唯一可能的形态。
	rec.reset()
	if err := RestoreCatalogVideo(sourceKey, "vod-1", "duplicate"); err == nil {
		t.Fatalf("对未删除的行重复恢复竟然成功")
	}
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("失败的操作不该广播事件：%+v", got)
	}

	if err := DeleteCatalogVideo(sourceKey, "vod-1", "user delete"); err != nil {
		t.Fatal(err)
	}
	rec.reset()
	deleted, err := PurgeCatalogVideo(sourceKey, "vod-1", "user purge")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("purged = %d, want 1", deleted)
	}
	if _, err := db.GetRecycleItem(sourceKey, "vod-1"); err == nil {
		t.Fatalf("彻底删除后回收站仍读得到")
	}
	assertSingleVideoEvent(t, rec, sourceKey, "vod-1", "user purge")

	// 彻底删除一个不存在的条目：报错、且不发事件。
	rec.reset()
	if _, err := PurgeCatalogVideo(sourceKey, "vod-1", "again"); err == nil {
		t.Fatalf("重复彻底删除竟然成功")
	}
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("失败的操作不该广播事件：%+v", got)
	}
}

func assertSingleVideoEvent(t *testing.T, rec *events, sourceKey, vodID, reason string) {
	t.Helper()
	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("events = %+v, want 1", got)
	}
	if got[0].Scope != ScopeVideo || got[0].SourceKey != sourceKey || got[0].Reason != reason {
		t.Errorf("event = %+v, want video/%s/%s", got[0], sourceKey, reason)
	}
	if len(got[0].VodIDs) != 1 || got[0].VodIDs[0] != vodID {
		t.Errorf("event vod_ids = %v, want [%s]", got[0].VodIDs, vodID)
	}
	rec.reset()
}

// 清空回收站按受影响的源整体失效：逐条广播的话，几千条回收站条目会把前端淹掉。
// 没被碰过的源和空回收站都必须一个事件都不发。
func TestClearRecycleBinInvalidatesAffectedSourcesOnly(t *testing.T) {
	const (
		sourceA = "cache_clear_a"
		sourceB = "cache_clear_b"
		sourceC = "cache_clear_c"
	)
	cleanSources(t, sourceA, sourceB, sourceC)
	defer cleanSources(t, sourceA, sourceB, sourceC)
	rec := capturePublisher(t)

	seedCatalog(t, sourceA, "a-1", "Clear first")
	seedCatalog(t, sourceA, "a-2", "Clear second")
	seedCatalog(t, sourceB, "b-1", "Clear other source")
	seedCatalog(t, sourceC, "c-1", "Untouched")

	if err := DeleteCatalogVideo(sourceA, "a-1", "seed"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCatalogVideo(sourceB, "b-1", "seed"); err != nil {
		t.Fatal(err)
	}
	rec.reset()

	deleted, err := ClearRecycleBin("", "empty recycle bin")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 2 {
		t.Errorf("deleted = %d, want 2", deleted)
	}
	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("events = %+v, want 每个受影响源一条", got)
	}
	for _, item := range got {
		if item.Scope != ScopeSource || item.Reason != "empty recycle bin" || len(item.VodIDs) != 0 {
			t.Errorf("event = %+v, want source scope without vod ids", item)
		}
	}
	if keys := sourceKeysOf(got); len(keys) != 2 || keys[0] != sourceA || keys[1] != sourceB {
		t.Errorf("invalidated sources = %v, want [%s %s]", keys, sourceA, sourceB)
	}
	// 视频本体不受影响：清空回收站不能把还在库里的条目也判死。
	if _, err := db.GetCatalogItem(sourceA, "a-2"); err != nil {
		t.Errorf("源里未删除的条目读不到了：%v", err)
	}
	if _, err := db.GetCatalogItem(sourceC, "c-1"); err != nil {
		t.Errorf("无关源的条目读不到了：%v", err)
	}

	// 回收站已经空了：什么都不该发生，包括事件。
	rec.reset()
	deleted, err = ClearRecycleBin(sourceA, "second clear")
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0", deleted)
	}
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("空回收站不该广播事件：%+v", got)
	}
}

// 身份合并既改写存活行、又把兄弟行的归属搬走，所以只能整源失效；而且必须在合并前
// 查清这些身份用过哪些源——被并掉的身份之后就查不回来了。
func TestMergeGlobalVideoIdentitiesInvalidatesSourcesOfBothIdentities(t *testing.T) {
	const (
		sourceA = "cache_merge_a"
		sourceB = "cache_merge_b"
	)
	cleanSources(t, sourceA, sourceB)
	defer cleanSources(t, sourceA, sourceB)
	if _, err := db.DB().Exec(`DELETE FROM global_video WHERE id IN (441, 442)`); err != nil {
		t.Fatal(err)
	}

	// 两条同名身份（归一化标题一致才允许并），各挂一个源。
	if _, err := db.DB().Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id, year) VALUES
			(441, '合并测试片', '合并测试片', 0, ''),
			(442, '合并测试片', '合并测试片！', 0, '');
		INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, lifecycle_state) VALUES
			(?, 'm-441', 441, '合并测试片', 'active'),
			(?, 'm-442', 442, '合并测试片', 'active');
	`, sourceA, sourceB); err != nil {
		t.Fatalf("seed identities: %v", err)
	}
	rec := capturePublisher(t)

	keep, merged, err := MergeGlobalVideoIdentities([]int64{441, 442}, "merge identities")
	if err != nil {
		t.Fatal(err)
	}
	if merged != 1 || keep != 441 && keep != 442 {
		t.Fatalf("keep=%d merged=%d", keep, merged)
	}
	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("events = %+v, want 两个源各一条", got)
	}
	if keys := sourceKeysOf(got); keys[0] != sourceA || keys[1] != sourceB {
		t.Errorf("invalidated sources = %v, want [%s %s]", keys, sourceA, sourceB)
	}
	for _, item := range got {
		if item.Scope != ScopeSource || item.Reason != "merge identities" {
			t.Errorf("event = %+v", item)
		}
	}

	// 不可合并的一组：DB 失败必须先于任何失效，前端不能因为一次没发生的合并丢缓存。
	rec.reset()
	if _, err := db.DB().Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id, year) VALUES
			(443, '另一部片', '另一部片', 0, '');
		INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, lifecycle_state) VALUES
			(?, 'm-443', 443, '另一部片', 'active');
	`, sourceA); err != nil {
		t.Fatal(err)
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{442, 443}, "bad group"); err == nil {
		t.Fatalf("非同名片竟然合并成功")
	}
	if got := rec.all(); len(got) != 0 {
		t.Fatalf("失败的合并不该广播事件：%+v", got)
	}
	if _, err := db.DB().Exec(`DELETE FROM global_video WHERE id=443`); err != nil {
		t.Fatal(err)
	}
	cleanSources(t, sourceA)
}

// 诊断面板按 key 取文案，不是按中文标题。这四块缓存的键是界面和本层的契约，
// 改名等于让设置页的缓存表格整列空白。
func TestStatsRowsUseStableKeys(t *testing.T) {
	rows := Stats()
	if len(rows) != 4 {
		t.Fatalf("rows = %d, want 4: %+v", len(rows), rows)
	}
	want := []string{"detail", "chart", "comments", "match"}
	for i, row := range rows {
		if row.Key != want[i] {
			t.Errorf("row %d key = %q, want %q", i, row.Key, want[i])
		}
	}
	// match 只是「查过一次就记住」的映射，没有新鲜度，所以它只有条目数。
	if rows[3].Hits != 0 || rows[3].Bytes != 0 {
		t.Errorf("match row = %+v, want counters untouched", rows[3])
	}
}
