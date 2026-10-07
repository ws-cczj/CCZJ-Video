package db

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

// 顺序迁移取代过去"generation 不符就归档删库"的做法：用户本地库原地演进，
// 升级不再清空收藏、历史和已采集的视频。
//
// schema 版本存在 SQLite 自带的 PRAGMA user_version 里。每条迁移在自己的事务
// 里执行并在同一事务落章，因此失败会整体回滚、下次启动重放同一条，不会留下
// 半迁移的库继续跑。
type migration struct {
	version int
	note    string
	apply   func(tx *sqlx.Tx) error
}

// migrations 必须按 version 升序手工排列（见 TestMigrationsAreOrdered）：
// 迁移一旦发布就不要再改动它的语句，需要新的修正时往后追加一条。
var migrations = []migration{
	{version: 1, note: "采集游标表：per-source 增量水位线", apply: migrateCollectCursor},
	{version: 2, note: "global_video 归一化列 name_norm：按 Go 侧唯一实现回填", apply: migrateGlobalVideoNameNorm},
	{version: 3, note: "合并归一化后重复的 global_video，并改指收藏/历史/目录", apply: migrateDedupeGlobalVideo},
	{version: 4, note: "作废旧索引，交给 createIndexes 按 EXPLAIN 结果重建", apply: migrateRebuildIndexes},
	{version: 5, note: "global_video 补 douban_last_attempt_at：豆瓣队列改为轮转", apply: migrateDoubanAttemptClock},
	{version: 6, note: "源健康度样本表：采集与巡检结果从此有历史", apply: migrateSourceHealth},
	{version: 7, note: "地区写法归一：中国大陆/内地→大陆、中国香港→香港、美国网络→美国", apply: migrateNormalizeAreaValues},
	{version: 8, note: "标题归一化去掉画质尾巴，重算 name_norm 并合并因此撞车的身份", apply: migrateRetitleGlobalVideo},
	{version: 9, note: "同一个豆瓣条目被拆成两条身份时按 douban_id 合并（跨类型）", apply: migrateMergeSameDoubanIdentity},
	{version: 10, note: "sources 补 auto_disabled_at：区分自动停用与手动关闭，到期后允许重试", apply: migrateSourceAutoDisable},
}

// migrateDoubanAttemptClock 给豆瓣补全队列加一个「最近一次尝试」时钟。
//
// 两条队列原先按 id / updated_at 取前 N 条：只要队头那几条持续失败（片名在豆瓣
// 根本查不到），每一轮选中的都是同一批行，后面的记录永远排不进来。空跑还什么都不
// 写，于是这个队头会一直卡住。有了逐行的尝试时间，队列才能按「谁等得最久谁先来」轮转。
//
// 顺手作废给它建过的轮转索引：EXPLAIN 显示两条队列都不选 (douban_last_attempt_at, id)
// ——筛选那半边一个走 NOT EXISTS 子查询、一个走 MULTI-INDEX OR——临时排序照旧，
// 白留一次写入。v5 尚未发布，但跑过 dev 的库可能已经建过它。
func migrateDoubanAttemptClock(tx *sqlx.Tx) error {
	exists, err := tableExists(tx, "global_video")
	if err != nil || !exists {
		return err
	}
	if _, err := tx.Exec("DROP INDEX IF EXISTS " + quoteIdentifier("idx_gv_douban_attempt")); err != nil {
		return fmt.Errorf("drop obsolete douban rotation index: %w", err)
	}
	return addColumnIfMissing(tx, "global_video", doubanAttemptColumn, "DATETIME DEFAULT NULL")
}

// migrateRebuildIndexes 只删索引，随后的 createIndexes 负责重建。两条都有实测依据：
// idx_fav_global 与 favorites 的 UNIQUE(global_id, source_key, vod_id) 自动索引左前缀
// 完全重复，白付一次写入；idx_gv_douban_id 建成了局部索引 (WHERE douban_id != ”)，
// 而按豆瓣 ID 查是 `douban_id = ?`，SQLite 证明不了占位符非空，于是全程扫表。
func migrateRebuildIndexes(tx *sqlx.Tx) error {
	for _, name := range []string{"idx_fav_global", "idx_gv_douban_id"} {
		if _, err := tx.Exec("DROP INDEX IF EXISTS " + quoteIdentifier(name)); err != nil {
			return fmt.Errorf("drop index %s: %w", name, err)
		}
	}
	return nil
}

// schemaVersion 读取当前库已应用的迁移版本。
func schemaVersion(database *sqlx.DB) (int, error) {
	var version int
	if err := database.Get(&version, "PRAGMA user_version"); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}

// SchemaVersion 暴露给诊断页：真机上看到它等于最新迁移号，才能确认升级
// 走的是迁移而不是过去的删库重建。
func SchemaVersion() (int, error) {
	return schemaVersion(instance)
}

// LatestSchemaVersion 是 migrations 列表里的最高版本。
func LatestSchemaVersion() int {
	latest := 0
	for _, m := range migrations {
		if m.version > latest {
			latest = m.version
		}
	}
	return latest
}

// schemaAheadOfBuild 记下「库结构比这个构建能认的最高版本还新」这件事。走到这一步只有
// 两条路：拿旧 exe 开新库，或者「退回上一版」把老程序换回原位——两条落下的是同一个状态。
var schemaAheadOfBuild struct {
	dbVersion    int
	buildVersion int
}

// applySchemaAhead 在库比构建新时换成忽略未知列的那份连接，并记下两个版本号。
//
// 为什么非得换：库里多出来的那一列会让旧结构体的 SELECT * 当场报错（sqlx 默认严格映射），
// 界面看到的就是「加载源站列表失败」加一整页空数据——库好好的，看着却像数据没了。
// 2026-10-06 用官方 v2.1.0 开 v10 库实测过，见 docs/pending-decisions.md 第 10 条。
//
// 只认这一种情形，平时仍由严格映射兜住写错的 db tag。
func applySchemaAhead(database *sqlx.DB, current int) (*sqlx.DB, bool) {
	latest := LatestSchemaVersion()
	if current <= latest {
		return database, false
	}
	schemaAheadOfBuild.dbVersion = current
	schemaAheadOfBuild.buildVersion = latest
	return database.Unsafe(), true
}

// DataNewerThanBuild 报告这份库是不是被更新的版本写过的，以及两边的版本号。
func DataNewerThanBuild() (dbVersion int, buildVersion int, newer bool) {
	if schemaAheadOfBuild.dbVersion == 0 {
		return 0, LatestSchemaVersion(), false
	}
	return schemaAheadOfBuild.dbVersion, schemaAheadOfBuild.buildVersion, true
}

// migrateToLatest 补齐落后的版本，返回实际执行的迁移。
func migrateToLatest(database *sqlx.DB) ([]migration, error) {
	current, err := schemaVersion(database)
	if err != nil {
		return nil, err
	}
	applied := make([]migration, 0, len(migrations))
	for _, m := range migrations {
		if m.version <= current {
			continue
		}
		tx, err := database.Beginx()
		if err != nil {
			return applied, fmt.Errorf("begin migration %d: %w", m.version, err)
		}
		if err := m.apply(tx); err != nil {
			_ = tx.Rollback()
			return applied, fmt.Errorf("migration %d (%s) failed: %w", m.version, m.note, err)
		}
		if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.version)); err != nil {
			_ = tx.Rollback()
			return applied, fmt.Errorf("record schema version %d: %w", m.version, err)
		}
		if err := tx.Commit(); err != nil {
			_ = tx.Rollback()
			return applied, fmt.Errorf("commit migration %d: %w", m.version, err)
		}
		applied = append(applied, m)
		logInfo(fmt.Sprintf("已应用迁移 v%d: %s", m.version, m.note))
	}
	return applied, nil
}

// backupBeforeMigrations 在动 schema 之前复制一份当前库。备份失败不阻断启动
// 之外的迁移执行：没有备份就不改 schema，这是"不得造成用户数据丢失"的硬约束。
func backupBeforeMigrations(database *sqlx.DB, dir string, pending int) (string, error) {
	if pending == 0 {
		return "", nil
	}
	backupDir := filepath.Join(dir, "schema-backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("create schema backup dir: %w", err)
	}
	name := fmt.Sprintf("cczj_video_pre_migration_%s.db", time.Now().Format("20060102_150405"))
	destination := filepath.Join(backupDir, name)
	if err := vacuumInto(database, destination); err != nil {
		return "", fmt.Errorf("backup database before migration: %w", err)
	}
	pruneSchemaBackups(backupDir)
	return destination, nil
}

// keepSchemaBackups 是滚动保留的备份份数。更早的备份属于本次启动自动产生的
// 中间产物，删掉它们才不会把用户目录越堆越大。
const keepSchemaBackups = 5

func pruneSchemaBackups(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	backups := make([]os.DirEntry, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "cczj_video_pre_migration_") && strings.HasSuffix(e.Name(), ".db") {
			backups = append(backups, e)
		}
	}
	if len(backups) <= keepSchemaBackups {
		return
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i].Name() > backups[j].Name() })
	for _, e := range backups[keepSchemaBackups:] {
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			logWarn(fmt.Sprintf("清理旧的结构备份失败(%s): %v", e.Name(), err))
		}
	}
}

// addColumnIfMissing 给已存在的表补列。旧做法是靠删库让新列"出现"，现在按
// 声明补齐，缺什么补什么，历史行保留。
func addColumnIfMissing(tx *sqlx.Tx, table, column, definition string) error {
	exists, err := tableExists(tx, table)
	if err != nil || !exists {
		return err
	}
	rows, err := tx.Query("PRAGMA table_info(" + quoteIdentifier(table) + ")")
	if err != nil {
		return fmt.Errorf("inspect table %s: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    any
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return fmt.Errorf("read column of %s: %w", table, err)
		}
		if name == column {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read columns of %s: %w", table, err)
	}
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", quoteIdentifier(table), quoteIdentifier(column), definition)
	if _, err := tx.Exec(stmt); err != nil {
		return fmt.Errorf("add column %s.%s: %w", table, column, err)
	}
	logInfo(fmt.Sprintf("补列 %s.%s", table, column))
	return nil
}

func tableExists(tx *sqlx.Tx, table string) (bool, error) {
	var count int
	err := tx.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check table %s: %w", table, err)
	}
	return count > 0, nil
}

// quoteIdentifier 只用于代码里写死的表名/列名，不接受用户输入。
func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
