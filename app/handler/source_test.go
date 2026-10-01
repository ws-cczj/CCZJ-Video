package handler

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"strings"
	"testing"
)

func TestAddSourceRejectsUnsafeSourceKeyBeforeDatabaseAccess(t *testing.T) {
	err := AddSource(&model.Source{
		SourceKey: "unsafe-key",
		ApiUrl:    "https://example.com/api.php/provide/vod/",
	})
	if err == nil {
		t.Fatal("expected unsafe source key to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid source_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeriveKeyProducesSafeKey(t *testing.T) {
	key := deriveKey("https://api.example-video.com/api.php/provide/vod/")
	if err := model.ValidateSourceKey(key); err != nil {
		t.Fatalf("derived key %q is invalid: %v", key, err)
	}
}

// db.UpdateSource 是无条件整列覆盖，所以「只改源名」的入参一旦不带三个 JSON 配置列，
// 定时采集计划、高级配置、扩展包适配策略就会被静默抹掉——前端本来就拿不到前两个。
// 这条测试钉住 handler 层的补值：空着传就等于「不动这一列」，显式传值才覆盖。
func TestUpdateSourceKeepsConfigColumnsWhenPayloadOmitsThem(t *testing.T) {
	const key = "cfg_keep"
	t.Cleanup(func() { _ = db.DeleteSource(key) })

	seed := &model.Source{
		SourceKey:      key,
		Name:           "原名",
		ApiUrl:         "https://example.com/api.php/provide/vod/",
		Enabled:        1,
		AdvConfigRaw:   `{"mode":"full"}`,
		ScheduleCfgRaw: `{"enabled":true}`,
		StrategyConfig: `{"version":2,"strategy":"declarative"}`,
	}
	if err := db.AddSource(seed); err != nil {
		t.Fatalf("种数据: %v", err)
	}

	// 前端「编辑源」实际会传的字段：只有名称/地址/开关/上限。
	if err := UpdateSource(&model.Source{
		SourceKey: key,
		Name:      "改名",
		ApiUrl:    seed.ApiUrl,
		Enabled:   1,
	}); err != nil {
		t.Fatalf("UpdateSource: %v", err)
	}

	got, err := db.GetSourceByKey(key)
	if err != nil {
		t.Fatalf("读回: %v", err)
	}
	if got.Name != "改名" {
		t.Fatalf("源名没改成功，实际 %q", got.Name)
	}
	for _, item := range []struct {
		field, label, want string
	}{
		{got.AdvConfigRaw, "adv_config", seed.AdvConfigRaw},
		{got.ScheduleCfgRaw, "schedule_config", seed.ScheduleCfgRaw},
		{got.StrategyConfig, "strategy_config", seed.StrategyConfig},
	} {
		if item.field != item.want {
			t.Fatalf("%s 被清空了：读到 %q，想要 %q", item.label, item.field, item.want)
		}
	}

	// 反过来也要成立：显式传值时按新值写，否则换适配策略这条路走不通。
	if err := UpdateSource(&model.Source{
		SourceKey:      key,
		Name:           "改名",
		ApiUrl:         seed.ApiUrl,
		Enabled:        1,
		StrategyConfig: `{"version":2,"strategy":"standard_cms"}`,
	}); err != nil {
		t.Fatalf("显式改策略: %v", err)
	}
	got, err = db.GetSourceByKey(key)
	if err != nil {
		t.Fatalf("再读回: %v", err)
	}
	if got.StrategyConfig != `{"version":2,"strategy":"standard_cms"}` {
		t.Fatalf("显式传的 strategy_config 应覆盖原值，实际 %q", got.StrategyConfig)
	}
	if got.AdvConfigRaw != seed.AdvConfigRaw {
		t.Fatalf("改策略不该动 adv_config，实际 %q", got.AdvConfigRaw)
	}
}

func seedGuardSource(t *testing.T, key string) {
	t.Helper()
	t.Cleanup(func() { _ = db.DeleteSource(key) })
	if err := db.AddSource(&model.Source{
		SourceKey: key,
		Name:      "守卫探针",
		ApiUrl:    "https://example.com/api.php/provide/vod/",
		Enabled:   1,
	}); err != nil {
		t.Fatalf("种采集源: %v", err)
	}
}

func guardSource(t *testing.T, key string) *model.Source {
	t.Helper()
	source, err := db.GetSourceByKey(key)
	if err != nil {
		t.Fatalf("读回采集源: %v", err)
	}
	return source
}

// 连败到阈值就把源停用：观测面（健康度样本）此前只是看得见，调度照样去戳一个死接口。
func TestRecordSourceHealthSampleDisablesAfterStreak(t *testing.T) {
	const key = "guard_streak"
	seedGuardSource(t, key)

	for i := 0; i < SourceDeadStreak-1; i++ {
		RecordSourceHealthSample(key, db.SourceHealthKindCollect, false, 10, 0, "第 1 页取页失败")
		if guardSource(t, key).Enabled != 1 {
			t.Fatalf("连败 %d 次就停用了，阈值应为 %d", i+1, SourceDeadStreak)
		}
	}
	RecordSourceHealthSample(key, db.SourceHealthKindCollect, false, 10, 0, "第 1 页取页失败")

	source := guardSource(t, key)
	if source.Enabled != 0 {
		t.Fatal("连败到达阈值后没有停用")
	}
	if source.AutoDisabledAt == 0 {
		t.Fatal("自动停用没有记下时刻，冷却到期后无从恢复")
	}

	// 再失败一次不该把时间戳刷成"刚刚才停用"，也不该重复播报。
	RecordSourceHealthSample(key, db.SourceHealthKindCollect, false, 10, 0, "第 1 页取页失败")
	if got := guardSource(t, key); got.AutoDisabledAt != source.AutoDisabledAt {
		t.Fatalf("重复停用把时刻刷成 %d，原本是 %d", got.AutoDisabledAt, source.AutoDisabledAt)
	}
}

// 一次成功就抹掉之前的抖动：连败数只算最后一次成功之后的失败。
func TestRecordSourceHealthSampleSuccessClearsStreak(t *testing.T) {
	const key = "guard_shaky"
	seedGuardSource(t, key)

	for i := 0; i < SourceDeadStreak-1; i++ {
		RecordSourceHealthSample(key, db.SourceHealthKindPatrol, false, 10, 0, "探测超时")
	}
	RecordSourceHealthSample(key, db.SourceHealthKindPatrol, true, 900, 0, "")
	for i := 0; i < SourceDeadStreak-1; i++ {
		RecordSourceHealthSample(key, db.SourceHealthKindPatrol, false, 10, 0, "探测超时")
	}
	if guardSource(t, key).Enabled != 1 {
		t.Fatal("中间成功过一次的抖动不该被算成失效")
	}

	// 巡检与采集各有各的连败口径，一边红不该把另一边的样本也算进来。
	RecordSourceHealthSample(key, db.SourceHealthKindCollect, false, 10, 0, "取页失败")
	if guardSource(t, key).Enabled != 1 {
		t.Fatal("采集刚失败一次，不该借用巡检的连败数停用")
	}
}

// 用户在采集源页重新打开后，旧样本必须一起作废，否则他看到的是"我明明开了，它自己又关了"。
func TestManualReopenInvalidatesStaleFailures(t *testing.T) {
	const key = "guard_reopen"
	seedGuardSource(t, key)

	for i := 0; i < SourceDeadStreak; i++ {
		RecordSourceHealthSample(key, db.SourceHealthKindCollect, false, 10, 0, "第 1 页取页失败")
	}
	if guardSource(t, key).Enabled != 0 {
		t.Fatal("前置条件：源应已被自动停用")
	}

	source := guardSource(t, key)
	source.Enabled = 1
	if err := UpdateSource(source); err != nil {
		t.Fatalf("手动重新启用: %v", err)
	}
	if health, err := db.GetSourceHealth(key, db.SourceHealthKindCollect, 0); err != nil {
		t.Fatalf("读健康度: %v", err)
	} else if health.FailStreak != 0 {
		t.Fatalf("重新启用后连败数仍是 %d", health.FailStreak)
	}

	// 重新启用后一次失败不该立刻再停用，得重新攒够一整轮。
	RecordSourceHealthSample(key, db.SourceHealthKindCollect, false, 10, 0, "第 1 页取页失败")
	after := guardSource(t, key)
	if after.Enabled != 1 {
		t.Fatal("手动重开后被旧样本立刻再次停用")
	}
	if after.AutoDisabledAt != 0 {
		t.Fatalf("手动重开却留着自动停用时刻 %d，冷却恢复会绕过用户意图", after.AutoDisabledAt)
	}
}
