package db

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

func openFreshSQLite(t *testing.T, dir string) *sqlx.DB {
	t.Helper()
	database, err := sqlx.Connect("sqlite", filepath.Join(dir, "cczj_video.db"))
	if err != nil {
		t.Fatalf("connect test database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	return database
}

// 迁移数组的顺序就是执行顺序，写反了会先落章高版本再跳过低版本，schema 永久缺一块。
func TestMigrationsAreOrdered(t *testing.T) {
	if len(migrations) == 0 {
		t.Fatal("migration list is empty")
	}
	for i, m := range migrations {
		if m.version < 1 {
			t.Fatalf("migration %d has version %d; must start at 1", i, m.version)
		}
		if m.apply == nil {
			t.Fatalf("migration %d has no apply func", m.version)
		}
		if i > 0 && m.version <= migrations[i-1].version {
			t.Fatalf("migration %d is not strictly after %d", m.version, migrations[i-1].version)
		}
	}
}

func TestMigrateToLatestAppliesOnceAndStaysIdempotent(t *testing.T) {
	database := openFreshSQLite(t, t.TempDir())

	applied, err := migrateToLatest(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(migrations) {
		t.Fatalf("applied %d migrations, want %d", len(applied), len(migrations))
	}
	version, err := schemaVersion(database)
	if err != nil {
		t.Fatal(err)
	}
	if version != migrations[len(migrations)-1].version {
		t.Fatalf("user_version = %d, want %d", version, migrations[len(migrations)-1].version)
	}
	if exists, err := tableExistsIn(database, "collect_cursor"); err != nil || !exists {
		t.Fatalf("collect_cursor exists=%v err=%v", exists, err)
	}

	// 第二次启动不得重放：重放会把滚动备份刷成一次性文件，也掩盖真实迁移失败。
	applied, err = migrateToLatest(database)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != 0 {
		t.Fatalf("second run applied %d migrations, want 0", len(applied))
	}
}

// 失败的迁移必须整体回滚，版本号不能前进，否则下次启动会跳过这条没做成的迁移。
func TestFailedMigrationRollsBackAndStallsVersion(t *testing.T) {
	defer func(orig []migration) { migrations = orig }(migrations)
	database := openFreshSQLite(t, t.TempDir())

	migrations = []migration{
		{version: 1, note: "ok", apply: func(tx *sqlx.Tx) error {
			_, err := tx.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY)`)
			return err
		}},
		{version: 2, note: "boom", apply: func(tx *sqlx.Tx) error {
			if _, err := tx.Exec(`INSERT INTO probe (id) VALUES (1)`); err != nil {
				return err
			}
			return errors.New("intended failure")
		}},
	}

	applied, err := migrateToLatest(database)
	if err == nil {
		t.Fatal("failing migration must surface an error")
	}
	if len(applied) != 1 {
		t.Fatalf("applied %d, want the one committed before the failure", len(applied))
	}
	version, _ := schemaVersion(database)
	if version != 1 {
		t.Fatalf("user_version = %d, want 1 (failed migration must not advance it)", version)
	}
	var rows int
	if err := database.Get(&rows, `SELECT COUNT(*) FROM probe`); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("failed migration left %d rows behind; transaction did not roll back", rows)
	}
}

// 核心约束：带着旧 database_reset_version 标记的库必须原地演进，
// 一条视频都不许丢，也不许再产生归档删库的痕迹。
func TestRunMigrationsKeepsExistingData(t *testing.T) {
	dir := t.TempDir()
	seed := openFreshSQLite(t, dir)
	if _, err := seed.Exec(`CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(`INSERT INTO settings (key, value) VALUES ('database_reset_version', '3')`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(`CREATE TABLE global_video (id INTEGER PRIMARY KEY, vod_name TEXT NOT NULL DEFAULT '', type_id INTEGER DEFAULT 0, douban_id TEXT DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := seed.Exec(`INSERT INTO global_video (vod_name, type_id) VALUES ('必须活下来', 1)`); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}

	database, err := sqlx.Connect("sqlite", filepath.Join(dir, "cczj_video.db"))
	if err != nil {
		t.Fatal(err)
	}
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	defer func() {
		instance, dataDir = prevInstance, prevDir
		_ = database.Close()
	}()

	if err := runMigrations(); err != nil {
		t.Fatalf("runMigrations: %v", err)
	}

	var name string
	if err := database.Get(&name, `SELECT vod_name FROM global_video LIMIT 1`); err != nil {
		t.Fatalf("collection data lost during upgrade: %v", err)
	}
	if name != "必须活下来" {
		t.Fatalf("vod_name = %q", name)
	}
	var marker string
	if err := database.Get(&marker, `SELECT value FROM settings WHERE key='database_reset_version'`); err != nil {
		t.Fatalf("legacy marker row lost: %v", err)
	}

	backups, err := filepath.Glob(filepath.Join(dir, "schema-backups", "*.db"))
	if err != nil || len(backups) == 0 {
		t.Fatalf("no pre-migration backup written: %v %v", backups, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reset-archives")); err == nil {
		t.Fatal("archive-and-delete upgrade path must be gone")
	}

	// 已迁移完毕的库再启动一次：不该又复制一份备份。
	before := len(backups)
	if err := runMigrations(); err != nil {
		t.Fatalf("second runMigrations: %v", err)
	}
	after, _ := filepath.Glob(filepath.Join(dir, "schema-backups", "*.db"))
	if len(after) != before {
		t.Fatalf("clean startup wrote another backup: %d -> %d", before, len(after))
	}
}

func TestBackupPrunesToRollingWindow(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	if _, err := database.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	backupDir := filepath.Join(dir, "schema-backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < keepSchemaBackups+3; i++ {
		name := filepath.Join(backupDir, "cczj_video_pre_migration_2026010"+string(rune('0'+i))+"_000000.db")
		if err := os.WriteFile(name, []byte("old"), 0644); err != nil {
			t.Fatal(err)
		}
	}

	destination, err := backupBeforeMigrations(database, dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); err != nil {
		t.Fatalf("backup not written: %v", err)
	}
	remaining, err := filepath.Glob(filepath.Join(backupDir, "*.db"))
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != keepSchemaBackups {
		t.Fatalf("backups = %d, want rolling %d", len(remaining), keepSchemaBackups)
	}

	// 没有待执行迁移时不备份，避免每次启动都复制一遍库。
	if destination, err := backupBeforeMigrations(database, dir, 0); err != nil || destination != "" {
		t.Fatalf("backupBeforeMigrations with nothing to do = %q, %v", destination, err)
	}
}

func TestAddColumnIfMissingBackfillsExistingRows(t *testing.T) {
	database := openFreshSQLite(t, t.TempDir())
	if _, err := database.Exec(`CREATE TABLE probe (id INTEGER PRIMARY KEY, name TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO probe (name) VALUES ('保留')`); err != nil {
		t.Fatal(err)
	}

	tx, err := database.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	for i := 0; i < 2; i++ {
		if err := addColumnIfMissing(tx, "probe", "extra_col", "TEXT NOT NULL DEFAULT 'x'"); err != nil {
			t.Fatalf("add column attempt %d: %v", i, err)
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	var value string
	if err := database.Get(&value, `SELECT extra_col FROM probe LIMIT 1`); err != nil {
		t.Fatal(err)
	}
	if value != "x" {
		t.Fatalf("extra_col = %q", value)
	}

	// 表不存在时不该报错：迁移可能面向尚未建立的表，跳过即可。
	skipTx, err := database.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer skipTx.Rollback()
	if err := addColumnIfMissing(skipTx, "absent_table", "col", "TEXT"); err != nil {
		t.Fatalf("absent table must be skipped, got %v", err)
	}
}

func tableExistsIn(database *sqlx.DB, table string) (bool, error) {
	var count int
	if err := database.Get(&count, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, table); err != nil {
		return false, err
	}
	return count > 0, nil
}
