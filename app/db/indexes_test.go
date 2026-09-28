package db

import (
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

// 索引不是"建了就有用"。这两条旧索引都是建在那里但 EXPLAIN 显示查询根本没走它，
// 其中 douban_id 那条还是局部索引，等值查询证明不了占位符非空，于是每次按豆瓣 ID
// 查都扫全表。这个测试把每条热查询的执行计划钉住：哪天计划里重新出现扫表或
// 临时排序，就是索引被改动或者查询被改写，必须当场发现。
func TestIndexesCoverHotQueries(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() {
		instance, dataDir = prevInstance, prevDir
	})

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	seedIndexFixture(t, database)
	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	visibility := catalogTypeVisibilityClause("source_videos")
	// 健康度样本表是迁移 v6 建的，所以灌数据必须排在 runMigrations 之后。
	seedSourceHealthFixture(t, database)
	cases := []struct {
		name  string
		query string
		args  []any
		want  []string
		deny  []string
	}{
		{
			name: "目录翻页",
			query: `SELECT id FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND ` +
				visibility + ` ORDER BY vod_time DESC, id DESC LIMIT ?`,
			args: []any{"src-a", 20},
			want: []string{"idx_sv_key_lifecycle_time"},
			deny: []string{"TEMP B-TREE", "SCAN source_videos"},
		},
		{
			name: "目录标题搜索",
			query: `SELECT id FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND ` + visibility +
				` AND vod_name LIKE ? ORDER BY vod_time DESC, id DESC LIMIT ?`,
			args: []any{"src-a", "%48%", 20},
			want: []string{"idx_sv_key_lifecycle_time"},
			deny: []string{"TEMP B-TREE"},
		},
		{
			name:  "翻页总数",
			query: `SELECT COUNT(*) FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND ` + visibility,
			args:  []any{"src-a"},
			want:  []string{"idx_sv_key_lifecycle_time"},
		},
		{
			name: "源内按全局身份取最近一条",
			query: `SELECT id FROM source_videos WHERE source_key=? AND global_id=? AND lifecycle_state='active' AND ` +
				visibility + ` ORDER BY updated_at DESC LIMIT 1`,
			args: []any{"src-a", 5},
			want: []string{"idx_sv_key_global_updated"},
			deny: []string{"TEMP B-TREE"},
		},
		{
			name:  "跨源同片列表",
			query: `SELECT source_key FROM source_videos WHERE global_id=? AND lifecycle_state='active' AND ` + visibility + ` ORDER BY source_key`,
			args:  []any{5},
			want:  []string{"idx_sv_global_key"},
			deny:  []string{"SCAN source_videos"},
		},
		{
			name:  "年代筛选项",
			query: `SELECT DISTINCT vod_year FROM source_videos WHERE source_key=? AND ` + visibility + ` AND vod_year!='' ORDER BY vod_year DESC`,
			args:  []any{"src-a"},
			want:  []string{"idx_sv_key_year"},
		},
		{
			name:  "地区筛选项",
			query: `SELECT DISTINCT vod_area FROM source_videos WHERE source_key=? AND ` + visibility + ` AND vod_area!='' ORDER BY vod_area`,
			args:  []any{"src-a"},
			want:  []string{"idx_sv_key_area"},
		},
		{
			name: "最近观看",
			query: `SELECT h.global_id FROM watch_history h JOIN global_video g ON h.global_id = g.id
				LEFT JOIN source_videos sv ON sv.source_key = h.source_key AND sv.source_vod_id = h.vod_id
				WHERE sv.id IS NULL OR ` + catalogProjectionFilter("sv") + ` ORDER BY h.updated_at DESC LIMIT ?`,
			args: []any{20},
			want: []string{"idx_wh_updated", "sqlite_autoindex_source_videos_1"},
			deny: []string{"TEMP B-TREE"},
		},
		{
			name: "收藏列表",
			query: `SELECT f.id FROM favorites f JOIN global_video g ON f.global_id = g.id
				LEFT JOIN source_videos sv ON sv.source_key = f.source_key AND sv.source_vod_id = f.vod_id
				WHERE sv.id IS NULL OR ` + catalogProjectionFilter("sv") + ` ORDER BY f.created_at DESC LIMIT ? OFFSET ?`,
			args: []any{20, 0},
			want: []string{"idx_fav_created"},
			deny: []string{"TEMP B-TREE"},
		},
		{
			name:  "按源清历史",
			query: `DELETE FROM watch_history WHERE source_key = ? AND vod_id = ?`,
			args:  []any{"src-a", "vod-1"},
			want:  []string{"idx_wh_source_vod"},
			deny:  []string{"SCAN watch_history"},
		},
		{
			name:  "按标题查全局条目",
			query: `SELECT id FROM global_video WHERE vod_name = ? LIMIT 1`,
			args:  []any{"影片 100"},
			want:  []string{"idx_gv_vod_name"},
			deny:  []string{"SCAN global_video"},
		},
		{
			name:  "按豆瓣 ID 查",
			query: `SELECT id FROM global_video WHERE douban_id = ? LIMIT 1`,
			args:  []any{"d-50"},
			want:  []string{"idx_gv_douban_id"},
			deny:  []string{"SCAN global_video"},
		},
		{
			name:  "首页最近入库（豆瓣字段保底）",
			query: `SELECT id FROM global_video WHERE pic != '' ORDER BY updated_at DESC LIMIT ?`,
			args:  []any{10},
			want:  []string{"idx_gv_updated"},
			deny:  []string{"TEMP B-TREE"},
		},
		{
			name: "豆瓣缺 subject_id 队列（按尝试时间轮转）",
			query: `SELECT gv.id FROM global_video gv
				LEFT JOIN global_types gt ON gv.type_id = gt.id
				WHERE (gv.douban_id = '' OR gv.douban_id IS NULL)
				AND (gv.douban_cooldown_until IS NULL OR gv.douban_cooldown_until < ?)
				ORDER BY gv.douban_last_attempt_at ASC, gv.id ASC LIMIT ?`,
			args: []any{"2026-01-01 00:00:00", 5},
			want: []string{"idx_gv_douban_id"},
		},
		{
			name: "跨源合并列表（一张卡片一个身份）",
			query: `SELECT sv.id FROM source_videos sv WHERE sv.lifecycle_state='active' AND ` + catalogTypeVisibilityClause("sv") +
				` AND sv.global_id>0 AND NOT EXISTS (SELECT 1 FROM source_videos s4 WHERE s4.global_id=sv.global_id AND s4.lifecycle_state='active' AND ` +
				catalogTypeVisibilityClause("s4") + ` AND s4.global_id>0
				AND (s4.vod_time > sv.vod_time OR (s4.vod_time = sv.vod_time AND s4.id > sv.id)))
				ORDER BY sv.vod_time DESC, sv.id DESC LIMIT ?`,
			args: []any{20},
			deny: []string{"USE TEMP B-TREE"},
		},
		{
			name:  "跨源合并总数",
			query: `SELECT COUNT(DISTINCT sv.global_id) FROM source_videos sv WHERE sv.lifecycle_state='active' AND ` + catalogTypeVisibilityClause("sv") + ` AND sv.global_id>0`,
			deny:  []string{"SCAN source_videos"},
		},
		{
			name:  "健康度窗口聚合（某源某类最近 N 条）",
			query: `SELECT COUNT(*), COALESCE(SUM(ok),0) FROM (SELECT ok, latency_ms, ts_unix FROM source_health WHERE source_key = ? AND kind = ? ORDER BY ts_unix DESC, id DESC LIMIT ?)`,
			args:  []any{"src-1", "collect", 20},
			want:  []string{"idx_sh_key_time"},
			deny:  []string{"SCAN source_health", "USE TEMP B-TREE"},
		},
		{
			name:  "连败计数（最后一次成功之后有多少失败）",
			query: `SELECT COUNT(*) FROM source_health WHERE source_key = ? AND kind = ? AND ok = 0 AND ts_unix >= ?`,
			args:  []any{"src-1", "patrol", 1700000000},
			want:  []string{"idx_sh_key_time"},
			deny:  []string{"SCAN source_health"},
		},
	}

	for _, c := range cases {
		plan := explainPlan(t, database, c.query, c.args...)
		for _, w := range c.want {
			if !strings.Contains(plan, w) {
				t.Errorf("%s 没有用到 %s，计划:\n%s", c.name, w, plan)
			}
		}
		for _, d := range c.deny {
			if strings.Contains(plan, d) {
				t.Errorf("%s 计划里出现了 %s:\n%s", c.name, d, plan)
			}
		}
	}
}

// 索引定义本身也要钉住：局部索引在等值查询上用不上，重复索引白付写入。
// 迁移 v4 负责把老的这两条从已发布的库里删掉。
func TestIndexDefinitionsAreNeitherPartialNorDuplicate(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	var count int
	if err := database.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_fav_global'`); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Error("idx_fav_global 仍在：favorites 的 UNIQUE(global_id,...) 自动索引已经覆盖按 global_id 查")
	}
	var sql string
	if err := database.Get(&sql, `SELECT sql FROM sqlite_master WHERE type='index' AND name='idx_gv_douban_id'`); err != nil {
		t.Fatalf("idx_gv_douban_id missing: %v", err)
	}
	if strings.Contains(strings.ToUpper(sql), "WHERE") {
		t.Errorf("idx_gv_douban_id 又是局部索引，等值查询用不上: %s", sql)
	}

	// 有些索引是「建过、被 EXPLAIN 证伪、又删掉」的，钉住它们不再回来：
	// 豆瓣队列的轮转时钟每轮都在变，而两条队列的筛选都不选这条索引。
	for _, name := range []string{"idx_gv_douban_attempt"} {
		var n int
		if err := database.Get(&n, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name=?`, name); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("%s 又回来了：EXPLAIN 显示豆瓣队列并不选它，只是多付一次写入", name)
		}
	}
}

func explainPlan(t *testing.T, database *sqlx.DB, query string, args ...any) string {
	t.Helper()
	rows, err := database.Query("EXPLAIN QUERY PLAN "+query, args...)
	if err != nil {
		t.Fatalf("explain %q: %v", query, err)
	}
	defer rows.Close()
	var builder strings.Builder
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("scan plan row: %v", err)
		}
		builder.WriteString(detail)
		builder.WriteString("\n")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	return builder.String()
}

// 健康度样本每源每类都只有几百条，但库里有几十个源，所以整体行数足以让
// 优化器在"没有可用索引"时选择全表扫。
func seedSourceHealthFixture(t *testing.T, database *sqlx.DB) {
	t.Helper()
	const rows = 16000
	if _, err := database.Exec(`
		INSERT INTO source_health (source_key, kind, ok, latency_ms, saved, err, ts_unix)
		SELECT 'src-' || (i % 40), CASE WHEN i % 2 = 0 THEN 'collect' ELSE 'patrol' END,
		       CASE WHEN i % 4 = 0 THEN 0 ELSE 1 END, i, i,
		       CASE WHEN i % 4 = 0 THEN 'boom' ELSE '' END, 1700000000 + i
		FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < ?) SELECT i FROM n)`, rows); err != nil {
		t.Fatalf("seed source_health: %v", err)
	}
}

// 计划是按行数选代价的，几十行的库什么都敢扫。灌入足够多的行，
// 让断言检验的是真实规模下的选择。
func seedIndexFixture(t *testing.T, database *sqlx.DB) {
	t.Helper()
	const rows = 600
	if _, err := database.Exec(`INSERT INTO global_types (type_name, collect_enabled, sort) VALUES ('国产',1,1)`); err != nil {
		t.Fatalf("seed global type: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO global_video (vod_name, name_norm, type_id, year, douban_id, douban_score, updated_at)
		SELECT '影片 '||i, '影片 '||i, 1, printf('%04d', 1970 + (i % 56)),
		       CASE WHEN i % 3 = 0 THEN '' ELSE 'd-'||i END,
		       CASE WHEN i % 4 = 0 THEN '' ELSE '8.1' END,
		       printf('2020-%02d-%02d 10:00:00', 1 + (i % 12), 1 + (i % 28))
		FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < ?) SELECT i FROM n)`, rows); err != nil {
		t.Fatalf("seed global_video: %v", err)
	}
	for _, sourceKey := range []string{"src-a", "src-b"} {
		if _, err := database.Exec(`
			INSERT INTO source_videos (source_key, source_vod_id, global_id, source_type_id, global_type_id, type_name, vod_name, vod_year, vod_area, vod_time, lifecycle_state, updated_at)
			SELECT ?, 'vod-'||i, 1 + (i % ?), '1', 1, '国产', '影片 '||i, printf('%04d', 1970 + (i % 56)),
			       CASE i % 5 WHEN 0 THEN '大陆' WHEN 1 THEN '美国' WHEN 2 THEN '日本' WHEN 3 THEN '韩国' ELSE '其他' END,
			       printf('2026-%02d-%02d %02d:00:00', 1 + (i % 12), 1 + (i % 28), i % 24),
			       CASE WHEN i % 37 = 0 THEN 'deleted' ELSE 'active' END,
			       printf('2026-%02d-%02d 08:00:00', 1 + (i % 12), 1 + (i % 28))
			FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < ?) SELECT i FROM n)`,
			sourceKey, rows, rows); err != nil {
			t.Fatalf("seed source_videos: %v", err)
		}
	}
	if _, err := database.Exec(`
		INSERT INTO watch_history (global_id, source_key, vod_id, ep_num, position, updated_at)
		SELECT 1 + (i % ?), 'src-a', 'vod-'||i, 1 + (i % 12), 12.5,
		       printf('2026-%02d-%02d %02d:00:00', 1 + (i % 12), 1 + (i % 28), i % 24)
		FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < ?) SELECT i FROM n)`, rows, rows); err != nil {
		t.Fatalf("seed watch_history: %v", err)
	}
	if _, err := database.Exec(`
		INSERT INTO favorites (global_id, source_key, vod_id, created_at)
		SELECT 1 + (i % ?), 'src-a', 'vod-'||i,
		       printf('2026-%02d-%02d %02d:00:00', 1 + (i % 12), 1 + (i % 28), i % 24)
		FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < ?) SELECT i FROM n)`, rows, rows); err != nil {
		t.Fatalf("seed favorites: %v", err)
	}
}
