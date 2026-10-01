package db

import (
	"path/filepath"
	"strconv"
	"testing"

	"github.com/jmoiron/sqlx"
)

// 数据修正迁移（v7 地区、v8 标题归一化重算、v9 同豆瓣条目合并、v10 自动停用记号）
// 共用这套夹具：库建好后故意停在 v6，模拟一个从旧版升上来的用户库。
func newStagedMigrationDB(t *testing.T, stopAt int) {
	t.Helper()
	dir := t.TempDir()
	database, err := sqlx.Connect("sqlite", filepath.Join(dir, "cczj_video.db"))
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	prevInstance, prevDir := instance, dataDir
	t.Cleanup(func() {
		instance, dataDir = prevInstance, prevDir
		_ = database.Close()
	})
	instance, dataDir = database, dir
	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	if _, err := database.Exec("PRAGMA user_version = " + strconv.Itoa(stopAt)); err != nil {
		t.Fatalf("set schema version: %v", err)
	}
}

func execSeed(t *testing.T, query string, args ...any) {
	t.Helper()
	if _, err := DB().Exec(query, args...); err != nil {
		t.Fatalf("seed %q: %v", query, err)
	}
}

func oneString(t *testing.T, query string, args ...any) string {
	t.Helper()
	var value string
	if err := DB().Get(&value, query, args...); err != nil {
		t.Fatalf("read %q: %v", query, err)
	}
	return value
}

// 迁移改的是用户已有数据，动手之前必须先落一份备份；备份失败的代价是整库无兜底。
func TestRunMigrationsBacksUpBeforeTouchingData(t *testing.T) {
	newStagedMigrationDB(t, 6)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, area)
		VALUES (9001, '备份探针', 1, '备份探针', '中国大陆')`)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM global_video WHERE area='大陆'`); n != 1 {
		t.Fatalf("迁移没落地，area 仍为 %q", oneString(t, `SELECT area FROM global_video WHERE id=9001`))
	}
	backups, err := filepath.Glob(filepath.Join(dataDir, "schema-backups", "cczj_video_pre_migration_*.db"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("迁移前没有留下备份: files=%v err=%v", backups, err)
	}
}

func TestAreaMigrationNormalizesStoredSpellings(t *testing.T) {
	newStagedMigrationDB(t, 6)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, area)
		VALUES (9101, '地区探针', 1, '地区探针', '中国大陆,中国香港')`)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, area)
		VALUES (9102, '地区探针乙', 1, '地区探针乙', '内地')`)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, area)
		VALUES (9103, '地区探针丙', 1, '地区探针丙', '韩国')`)
	execSeed(t, `INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, vod_area)
		VALUES ('area_probe', 'v1', 9102, '地区探针乙', '美国网络')`)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	if got := oneString(t, `SELECT area FROM global_video WHERE id=9101`); got != "大陆,香港" {
		t.Fatalf("多值地区 = %q, want 大陆,香港", got)
	}
	if got := oneString(t, `SELECT area FROM global_video WHERE id=9102`); got != "大陆" {
		t.Fatalf("内地 应归到大陆，实际 %q", got)
	}
	if got := oneString(t, `SELECT area FROM global_video WHERE id=9103`); got != "韩国" {
		t.Fatalf("认不出的写法必须原样保留，实际 %q", got)
	}
	if got := oneString(t, `SELECT vod_area FROM source_videos WHERE source_key='area_probe'`); got != "美国" {
		t.Fatalf("目录侧 vod_area = %q, want 美国", got)
	}
}

// 画质尾巴曾经是独立身份：v8 重算 name_norm 之后两条撞成同一个 (name_norm, type_id)，
// 必须并成一条，且收藏与目录引用跟着走。
func TestRetitleMigrationMergesQualityTailIdentity(t *testing.T) {
	newStagedMigrationDB(t, 6)
	// 旧归一化不认识画质尾巴，所以两条 name_norm 不同，唯一索引当时也拦不住。
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year)
		VALUES (9201, '雷神4：爱与雷霆_1080P_', 1, '雷神4:爱与雷霆_1080p_', '2022')`)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id, douban_score)
		VALUES (9202, '雷神4：爱与雷霆', 1, '雷神4:爱与雷霆', '2022', '34477861', '6.6')`)
	execSeed(t, `INSERT INTO favorites (global_id, source_key, vod_id) VALUES (9201, 'tail_src', 'vod-1')`)
	execSeed(t, `INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name)
		VALUES ('tail_src', 'vod-1', 9201, '雷神4：爱与雷霆_1080P_')`)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	if n := countRows(t, `SELECT COUNT(*) FROM global_video WHERE type_id=1 AND name_norm='雷神4:爱与雷霆'`); n != 1 {
		t.Fatalf("画质尾巴身份未合并，剩 %d 条", n)
	}
	// 存活行是带豆瓣成果的那条（pickMergeSurvivor 优先 douban_id）。
	if id := oneString(t, `SELECT id FROM global_video WHERE name_norm='雷神4:爱与雷霆'`); id != "9202" {
		t.Fatalf("存活行 = %s, want 9202（挂着豆瓣 ID 的那条）", id)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM favorites WHERE global_id=9202`); n != 1 {
		t.Fatalf("收藏没有跟着改指，命中 %d 条", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM source_videos WHERE global_id=9202`); n != 1 {
		t.Fatalf("目录没有跟着改指，命中 %d 条", n)
	}
	// v8 为了合并删掉过唯一索引，迁移收尾必须把它建回来。
	if n := countRows(t,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_gv_name_type_norm'`); n != 1 {
		t.Fatal("唯一索引 idx_gv_name_type_norm 没有重建")
	}
}

// v9 按豆瓣条目 ID 合并，但只认同名同年：抓错挂和翻拍片都得留在人工合并队列里。
func TestSameDoubanIdentityMigrationMergesSameTitleAcrossTypes(t *testing.T) {
	newStagedMigrationDB(t, 6)
	// 同名同年、同一个豆瓣条目，只是源站类型一边报剧集一边报喜剧 —— v8 合不掉它们。
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id)
		VALUES (9301, '老友记', 3, '老友记', '1994', '2579072')`)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id)
		VALUES (9302, '老友记', 5, '老友记', '1994', '2579072')`)
	// 同名同年但豆瓣 ID 不同 —— 不归 v9 管。
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id)
		VALUES (9311, '独立探针', 3, '独立探针', '2001', '1111111')`)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id)
		VALUES (9312, '独立探针', 5, '独立探针', '2001', '2222222')`)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	if n := countRows(t, `SELECT COUNT(*) FROM global_video WHERE name_norm='老友记'`); n != 1 {
		t.Fatalf("同豆瓣条目跨类型未合并，剩 %d 条", n)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM global_video WHERE name_norm='独立探针'`); n != 2 {
		t.Fatalf("豆瓣 ID 不同的两条不该被合并，剩 %d 条", n)
	}
}

// 年份不一致说明这是两部片（原作与翻拍），豆瓣 ID 相同也不能并。
func TestSameDoubanIdentityMigrationKeepsDifferentYears(t *testing.T) {
	newStagedMigrationDB(t, 6)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id)
		VALUES (9401, '异形', 1, '异形', '1979', '1447786')`)
	execSeed(t, `INSERT INTO global_video (id, vod_name, type_id, name_norm, year, douban_id)
		VALUES (9402, '异形', 2, '异形', '1986', '1447786')`)

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	if n := countRows(t, `SELECT COUNT(*) FROM global_video WHERE name_norm='异形'`); n != 2 {
		t.Fatalf("年份不同的两条被错并成 %d 条", n)
	}
}

// v10 只是补列，老库升上来得带上默认值，否则整表读回会缺字段。
func TestAutoDisableColumnMigrationAddsDefault(t *testing.T) {
	newStagedMigrationDB(t, 6)
	execSeed(t, `INSERT INTO sources (source_key, name, api_url, enabled)
		VALUES ('col_probe', '补列探针', 'https://example.com/api.php/provide/vod/', 1)`)
	// 建表已经带了新列，这里按老库的样子剥掉，才轮得到 v10 去补。
	if _, err := DB().Exec(`ALTER TABLE sources DROP COLUMN auto_disabled_at`); err != nil {
		t.Fatalf("模拟老库（去掉新列）: %v", err)
	}

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}
	sources, err := ReadSources(DB())
	if err != nil {
		t.Fatalf("读回带新列的源: %v", err)
	}
	found := false
	for _, s := range sources {
		if s.SourceKey != "col_probe" {
			continue
		}
		found = true
		if s.AutoDisabledAt != 0 {
			t.Fatalf("新列默认值 = %d, want 0", s.AutoDisabledAt)
		}
	}
	if !found {
		t.Fatal("补列之后读不回这个源，说明整表 SELECT 被新列打断了")
	}
}
