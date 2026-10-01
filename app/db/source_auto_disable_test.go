package db

import (
	"cczjVideo/app/model"
	"testing"
	"time"
)

func newSourceAutoDisableFixture(t *testing.T, key string, enabled int) {
	t.Helper()
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if _, err := database.Exec(`DELETE FROM sources WHERE source_key=?`, key); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO sources (source_key, name, api_url, enabled) VALUES (?, ?, ?, ?)`,
		key, key, "https://example.com/api.php/provide/vod/", enabled); err != nil {
		t.Fatal(err)
	}
}

func sourceRow(t *testing.T, key string) model.Source {
	t.Helper()
	var row model.Source
	if err := instance.Get(&row, `SELECT * FROM sources WHERE source_key=?`, key); err != nil {
		t.Fatalf("读回 %s: %v", key, err)
	}
	return row
}

// 自动停用要能被冷却恢复认领，所以停用时刻必须落库；对已经停用的源重复调用不能报成功，
// 否则每轮巡检都会对着同一个死源重复播报。
func TestAutoDisableSourceStampsTimeAndReportsTransition(t *testing.T) {
	const key = "auto_disable_probe"
	newSourceAutoDisableFixture(t, key, 1)

	at := time.Date(2026, 10, 1, 9, 30, 0, 0, time.UTC)
	changed, err := AutoDisableSource(key, at)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("首次停用应报告状态发生了变化")
	}
	row := sourceRow(t, key)
	if row.Enabled != 0 {
		t.Fatalf("enabled = %d, want 0", row.Enabled)
	}
	if row.AutoDisabledAt != at.Unix() {
		t.Fatalf("auto_disabled_at = %d, want %d", row.AutoDisabledAt, at.Unix())
	}

	// 用户早已手动关掉时不再改动，也不再播报。
	changed, err = AutoDisableSource(key, at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Fatal("重复停用不该报告变化")
	}
	if got := sourceRow(t, key).AutoDisabledAt; got != at.Unix() {
		t.Fatalf("重复停用把时间戳刷新的 %d，应保持 %d", got, at.Unix())
	}
}

// 冷却恢复只认自动停用：用户明确关掉的源谁也不能替他打开。
func TestRecoverAutoDisabledSourcesOnlyTouchesAutoDisabled(t *testing.T) {
	const autoKey, manualKey = "recover_auto", "recover_manual"
	newSourceAutoDisableFixture(t, autoKey, 1)
	if _, err := instance.Exec(`INSERT INTO sources (source_key, name, api_url, enabled)
		VALUES (?, ?, ?, 0)`, manualKey, manualKey, "https://example.com/api.php/provide/vod/"); err != nil {
		t.Fatal(err)
	}

	// 手动停用：enabled=0 且没有时间戳。
	if got := sourceRow(t, manualKey).AutoDisabledAt; got != 0 {
		t.Fatalf("手动停用的源不该有时间戳，实际 %d", got)
	}

	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	if _, err := AutoDisableSource(autoKey, at); err != nil {
		t.Fatal(err)
	}
	if err := RecordSourceHealth(autoKey, SourceHealthKindCollect, false, 10, 0, "第 1 页取页失败", at); err != nil {
		t.Fatal(err)
	}

	// 冷却未到期：一个都不该恢复。
	keys, err := RecoverAutoDisabledSources(at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 0 {
		t.Fatalf("未到期却恢复了 %v", keys)
	}

	// 到期：自动停用的回来了，手动停用的原样不动，失败样本一起作废。
	keys, err = RecoverAutoDisabledSources(at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || keys[0] != autoKey {
		t.Fatalf("恢复 = %v, want [%s]", keys, autoKey)
	}
	row := sourceRow(t, autoKey)
	if row.Enabled != 1 || row.AutoDisabledAt != 0 {
		t.Fatalf("恢复后 enabled=%d auto_disabled_at=%d，应为 1 与 0", row.Enabled, row.AutoDisabledAt)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM source_health WHERE source_key=?`, autoKey); n != 0 {
		t.Fatalf("恢复后仍留着 %d 条失败样本，下一次失败会立刻把它再停用", n)
	}
	if got := sourceRow(t, manualKey); got.Enabled != 0 {
		t.Fatal("用户手动关掉的源被自动恢复了")
	}
}

// 手动保存一次源就等于重新宣告它可用，自动停用的记号必须一起清掉。
func TestUpdateSourceClearsAutoDisableStamp(t *testing.T) {
	const key = "manual_save_clears"
	newSourceAutoDisableFixture(t, key, 1)
	if _, err := AutoDisableSource(key, time.Now()); err != nil {
		t.Fatal(err)
	}

	row := sourceRow(t, key)
	row.Enabled = 1
	row.Name = "改过名"
	if err := UpdateSource(&row); err != nil {
		t.Fatal(err)
	}
	after := sourceRow(t, key)
	if after.AutoDisabledAt != 0 {
		t.Fatalf("手动保存后时间戳仍是 %d", after.AutoDisabledAt)
	}
	if after.Enabled != 1 || after.Name != "改过名" {
		t.Fatalf("手动保存把开关或名字写坏了：enabled=%d name=%q", after.Enabled, after.Name)
	}
}
