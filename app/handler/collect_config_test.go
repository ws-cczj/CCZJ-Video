package handler

import (
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
)

// putSetting 直接写原始配置值：这一层要验的正是「库里存的字符串怎么被读成配置」，
// 用 SetScheduleConfig 写回去就等于自己验自己。
func putSetting(t *testing.T, key, value string) {
	t.Helper()
	if err := db.SetSetting(key, value); err != nil {
		t.Fatalf("seed setting %s: %v", key, err)
	}
	t.Cleanup(func() {
		if _, err := db.DB().Exec(`DELETE FROM settings WHERE key=?`, key); err != nil {
			t.Fatal(err)
		}
	})
}

// 调度配置是设置页唯一的数据源：库里没东西、值坏了、字段缺了，都必须落回可用默认，
// 否则一次手改 settings 就能让后台采集永远排不出班。
func TestGetScheduleConfigFallsBackToDefaults(t *testing.T) {
	for _, raw := range []string{"", "{", `{"background_interval_seconds":0}`, `null`} {
		putSetting(t, scheduleConfigKey, raw)
		got := GetScheduleConfig()
		if got.BackgroundIntervalSeconds != 60 || got.SourceGapSeconds != 10 || got.PageGapSeconds != 30 {
			t.Errorf("raw=%q -> %+v, want 60/10/30 默认", raw, got)
		}
		if got.EnableBackground || got.EnableStartupCatchup || got.EnableInitialFullCollect {
			t.Errorf("raw=%q 不该把开关读成 true: %+v", raw, got)
		}
	}
	if got := defaultScheduleConfig(); got.BackgroundIntervalSeconds != 60 || got.PageGapSeconds != 30 {
		t.Fatalf("defaultScheduleConfig = %+v", got)
	}
}

// 旧版配置只有 minutes 字段。转换后仍要过最小 30 秒的闸门：只写 1 分钟的老设置
// 读出来是 60 秒，而不是 1 秒。
func TestGetScheduleConfigConvertsLegacyMinutes(t *testing.T) {
	putSetting(t, scheduleConfigKey, `{"background_interval_minutes":5,"source_gap_seconds":1,"page_gap_seconds":0}`)
	got := GetScheduleConfig()
	if got.BackgroundIntervalSeconds != 300 {
		t.Errorf("BackgroundIntervalSeconds = %d, want 5 分钟换算成 300", got.BackgroundIntervalSeconds)
	}
	// 间隔只有「<=0 回默认」这一条规则：填了正数就原样读回，下限是设置页自己夹的。
	if got.SourceGapSeconds != 1 || got.PageGapSeconds != 30 {
		t.Errorf("SourceGapSeconds=%d PageGapSeconds=%d, want 1 原样读回 / 0 回落到 30", got.SourceGapSeconds, got.PageGapSeconds)
	}

	putSetting(t, scheduleConfigKey, `{"background_interval_seconds":5}`)
	if got := GetScheduleConfig(); got.BackgroundIntervalSeconds != 30 {
		t.Errorf("BackgroundIntervalSeconds = %d, want 最小值夹到 30", got.BackgroundIntervalSeconds)
	}
}

func TestSetScheduleConfigRoundTrip(t *testing.T) {
	want := CollectScheduleConfig{
		EnableBackground:          true,
		BackgroundIntervalSeconds: 120,
		EnableStartupCatchup:      true,
		SourceGapSeconds:          8,
		PageGapSeconds:            45,
	}
	t.Cleanup(func() {
		if _, err := db.DB().Exec(`DELETE FROM settings WHERE key=?`, scheduleConfigKey); err != nil {
			t.Fatal(err)
		}
	})
	if err := SetScheduleConfig(want); err != nil {
		t.Fatal(err)
	}
	got := GetScheduleConfig()
	if got.EnableBackground != want.EnableBackground || got.BackgroundIntervalSeconds != want.BackgroundIntervalSeconds ||
		got.EnableStartupCatchup != want.EnableStartupCatchup || got.SourceGapSeconds != want.SourceGapSeconds ||
		got.PageGapSeconds != want.PageGapSeconds {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
	var stored map[string]any
	raw, err := db.GetSetting(scheduleConfigKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		t.Fatalf("stored value is not json: %q", raw)
	}
	if _, ok := stored["enable_background"]; !ok {
		t.Errorf("stored json = %q, want 前端约定的字段名", raw)
	}
}

// 退出时间与上次采集时间是补采窗口的两个端点：库里读不出整数（空、脏、被手工改过）
// 必须回 0 表示「没有记录」，而不是把垃圾值传进窗口计算。
func TestLastExitAndLastRunUnixRoundTrip(t *testing.T) {
	keys := []string{scheduleLastExitKey, scheduleLastRunKey}
	t.Cleanup(func() {
		for _, key := range keys {
			if _, err := db.DB().Exec(`DELETE FROM settings WHERE key=?`, key); err != nil {
				t.Fatal(err)
			}
		}
	})
	for _, key := range keys {
		if _, err := db.DB().Exec(`DELETE FROM settings WHERE key=?`, key); err != nil {
			t.Fatal(err)
		}
	}
	if got := GetLastExitUnix(); got != 0 {
		t.Errorf("GetLastExitUnix without record = %d, want 0", got)
	}
	if got := GetLastRunUnix(); got != 0 {
		t.Errorf("GetLastRunUnix without record = %d, want 0", got)
	}

	before := time.Now().Unix()
	TouchLastExit()
	TouchLastRun()
	after := time.Now().Unix()

	for _, got := range []int64{GetLastExitUnix(), GetLastRunUnix()} {
		if got < before || got > after {
			t.Errorf("recorded unix = %d, want 落在 [%d, %d]", got, before, after)
		}
	}

	putSetting(t, scheduleLastExitKey, "not-a-number")
	if got := GetLastExitUnix(); got != 0 {
		t.Errorf("脏值读成 %d, want 0", got)
	}
	putSetting(t, scheduleLastRunKey, strconv.FormatInt(1234567890, 10))
	if got := GetLastRunUnix(); got != 1234567890 {
		t.Errorf("GetLastRunUnix = %d, want 原样读回", got)
	}
}

// collect:done 的载荷键是前端 stores/collect.ts 直接取的字段名，改一个就等于事件失效。
func TestCollectDonePayloadKeysAreTheEventContract(t *testing.T) {
	outcome := RunOutcome{
		Log:           "boom",
		Saved:         12,
		FetchFailures: []int{3, 7},
		SaveFailures:  []int{4},
		EmptyPages:    []int{9},
		ErrorKind:     collect.ErrorKind(nil),
		ElapsedMs:     1500,
		Stopped:       true,
	}
	got := CollectDonePayload("src_a", "incremental", outcome)
	for _, key := range []string{"source_key", "mode", "error", "error_kind", "saved", "fetch_failures", "save_failures", "empty_pages", "elapsed_ms", "stopped"} {
		if _, ok := got[key]; !ok {
			t.Errorf("payload 缺键 %q: %+v", key, got)
		}
	}
	if got["source_key"] != "src_a" || got["mode"] != "incremental" || got["error"] != "boom" {
		t.Errorf("payload = %+v", got)
	}
	if got["stopped"] != true || got["saved"] != 12 || got["elapsed_ms"] != int64(1500) {
		t.Errorf("payload = %+v", got)
	}
	if failures, ok := got["fetch_failures"].([]int); !ok || len(failures) != 2 {
		t.Errorf("fetch_failures = %#v", got["fetch_failures"])
	}
}

// 未跑过的源也要回一个空状态而不是 nil：前端一进采集页就轮询状态，nil 会让整页报错。
func TestGetCollectStatusForUnknownSource(t *testing.T) {
	status := GetCollectStatus("never_ran_source")
	if status == nil {
		t.Fatalf("status is nil")
	}
	if status.SourceKey != "never_ran_source" || status.Running || status.Paused {
		t.Fatalf("status = %+v", status)
	}
	// 返回的是快照拷贝：调用方改它不能污染引擎里的真状态。
	status.Current = 999
	if again := GetCollectStatus("never_ran_source"); again.Current == 999 {
		t.Errorf("GetCollectStatus 回了内部指针")
	}
}

// 暂停/恢复/停止对「根本没有这个源」和「有 entry 但没绑引擎」都必须回 false：
// 设置页据此显示按钮是否生效，回 true 等于骗用户以为按下去了。
func TestPauseResumeStopWithoutEngine(t *testing.T) {
	if PauseCollect("no_such_source") || ResumeCollect("no_such_source") || StopCollect("no_such_source") {
		t.Fatalf("未知源的操作回了 true")
	}

	const sourceKey = "handler_entry_no_engine"
	entry := GetOrCreateEngine(sourceKey)
	t.Cleanup(func() {
		engineMapMu.Lock()
		delete(engineMap, sourceKey)
		engineMapMu.Unlock()
	})
	if PauseCollect(sourceKey) || ResumeCollect(sourceKey) || StopCollect(sourceKey) {
		t.Errorf("空闲 entry 上的操作回了 true")
	}
	if entry.IsRunning() {
		t.Errorf("新建 entry 不该是 running")
	}

	// 进度与页名的写入走的是同一把锁，快照必须读得到。
	entry.UpdateProgress(3, 20)
	entry.UpdatePageNames(2, []string{"甲", "乙"})
	snapshot := GetCollectStatus(sourceKey)
	if snapshot.Current != 3 || snapshot.Total != 20 || snapshot.Page != 2 || len(snapshot.Names) != 2 {
		t.Errorf("snapshot = %+v", snapshot)
	}
}

// 回收站入口只做参数校验和分页归一，真正的生命周期在 db/cache 里；
// 这里钉住的是「界面上能看见什么」：空列表回 []、缺参数回 Validation 码。
func TestRecycleHandlersValidateAndNormalize(t *testing.T) {
	if err := (RecycleReq{}).validate(); err == nil || apperror.CodeOf(err) != apperror.Validation {
		t.Errorf("空参数 validate = %v, want Validation", err)
	}
	if err := (RecycleReq{SourceKey: "s"}).validate(); err == nil {
		t.Errorf("缺 vod_id 竟然通过校验")
	}
	if err := (RecycleReq{SourceKey: "s", VodId: "v"}).validate(); err != nil {
		t.Errorf("完整参数被拒: %v", err)
	}

	if err := RestoreVideo(RecycleReq{VodId: "v"}); apperror.CodeOf(err) != apperror.Validation {
		t.Errorf("RestoreVideo 缺参 = %v, want Validation", err)
	}
	if _, err := PurgeVideo(RecycleReq{SourceKey: "s"}); apperror.CodeOf(err) != apperror.Validation {
		t.Errorf("PurgeVideo 缺参 = %v, want Validation", err)
	}

	// 一条都不存在的源：items 必须是空数组而不是 null，前端 v-for 直接吃到 null 会炸。
	resp, err := ListRecycleBin(RecycleListReq{SourceKey: "recycle_handler_unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Items == nil {
		t.Errorf("items is nil, want 空切片")
	}
	if resp.Page != 1 || resp.PageSize != 50 {
		t.Errorf("分页归一 = page %d size %d, want 1/50", resp.Page, resp.PageSize)
	}
	if resp.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Total)
	}

	deleted, err := ClearRecycleBin(RecycleListReq{SourceKey: "recycle_handler_unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Affected != 0 {
		t.Errorf("affected = %d, want 0", deleted.Affected)
	}
}
