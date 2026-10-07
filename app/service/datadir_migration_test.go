package service

import (
	"os"
	"path/filepath"
	"testing"

	"cczjVideo/app/applog"
)

// TestMain 抢在任何 applog 调用之前把单例绑到临时目录。
// applog.Default() 会在单例为空时用 %APPDATA% 的生产目录把它建出来，而这里的
// 「新旧两处都有库」分支就是要写日志的，不绑就会灌进用户真实的日志文件。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-service-test-")
	if err != nil {
		panic(err)
	}
	if err := applog.Init(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

func writeDB(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func readDB(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return ""
	}
	return string(data)
}

// 结论决定界面接下来做什么：migrated 会把「旧数据找回」记账成已处理，blocked 才留下
// 提示。所以这些用例既查文件也查返回值——只看文件会把「什么都没做」和「做了但失败」
// 当成同一件事。
func checkMigration(t *testing.T, name string, got, want legacyMigration) {
	t.Helper()
	if got != want {
		t.Fatalf("%s 的结论 = %d, 期望 %d", name, got, want)
	}
}

// 2.0.x 及更早的库在 exe 旁边，2.1.0 起改读用户配置目录。原地热替换 exe 之后新目录是空的，
// 用户看到的就是「更新完数据没了」——这一条把旧库搬过来，并给出可复制成功的结论。
func TestMigrateLegacyDatabaseCopiesIntoEmptyNewDir(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "legacy")
	newDir := filepath.Join(root, "config")
	// 生产路径上 ServiceStartup 先 MkdirAll(dataDir) 再迁移，这里跟上同一个前提。
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDB(t, oldDir, "cczj_video.db", "old-database")
	writeDB(t, oldDir, "cczj_video.db-wal", "old-wal")

	checkMigration(t, "新目录为空时复制旧库", migrateLegacyDatabase(oldDir, newDir), legacyMigrated)

	if got := readDB(t, newDir, "cczj_video.db"); got != "old-database" {
		t.Fatalf("新目录的库 = %q, 期望 %q", got, "old-database")
	}
	// WAL 必须一起搬：只复制 .db 会把尚未 checkpoint 的已提交事务留在旧目录。
	if got := readDB(t, newDir, "cczj_video.db-wal"); got != "old-wal" {
		t.Fatalf("新目录的 WAL = %q, 期望 %q", got, "old-wal")
	}
	// 只复制不删除，旧目录留着当免费备份。
	if got := readDB(t, oldDir, "cczj_video.db"); got != "old-database" {
		t.Fatalf("旧目录被改动了: %q", got)
	}
}

// 两处都有库时猜哪边是"真的"都会覆盖掉用户的一半数据，所以这里必须停在 blocked，
// 那份旧库改由「旧数据找回」提示交给用户自己点。
func TestMigrateLegacyDatabaseNeverOverwritesExistingDatabase(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "legacy")
	newDir := filepath.Join(root, "config")
	writeDB(t, oldDir, "cczj_video.db", "old-database")
	writeDB(t, newDir, "cczj_video.db", "live-database")

	checkMigration(t, "新旧两处都有库", migrateLegacyDatabase(oldDir, newDir), legacyBlocked)

	if got := readDB(t, newDir, "cczj_video.db"); got != "live-database" {
		t.Fatalf("新目录被覆盖了: %q", got)
	}
	if got := readDB(t, oldDir, "cczj_video.db"); got != "old-database" {
		t.Fatalf("旧目录被改动了: %q", got)
	}
}

// getDataDir 在拿不到用户配置目录时会退回 exe 旁边的 data，这时两边是同一个目录，
// 复制等于把自己读一遍再写回自己。
func TestMigrateLegacyDatabaseSkipsSameDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "data")
	writeDB(t, dir, "cczj_video.db", "database")

	checkMigration(t, "旧新其实是同一个目录",
		migrateLegacyDatabase(dir, filepath.Join(dir, "..", "data")), legacyNone)

	if got := readDB(t, dir, "cczj_video.db"); got != "database" {
		t.Fatalf("同一个目录被改动了: %q", got)
	}
}

// 全新安装：旧落点根本没有库，不该在空目录里凭空造出一个数据库文件。
func TestMigrateLegacyDatabaseSkipsWhenNoLegacyDatabase(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "legacy")
	newDir := filepath.Join(root, "config")
	if err := os.MkdirAll(newDir, 0755); err != nil {
		t.Fatal(err)
	}

	checkMigration(t, "旧落点没有库", migrateLegacyDatabase(oldDir, newDir), legacyNone)

	if got := readDB(t, newDir, "cczj_video.db"); got != "" {
		t.Fatalf("没有旧库却写出了文件: %q", got)
	}
}

// 复制失败同样是 blocked。这条结论让界面继续提示用户去找回旧数据，
// 而不是记成「已经搬好了」——记错等于把那份旧库永久藏起来。
func TestMigrateLegacyDatabaseReportsBlockedWhenCopyFails(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "legacy")
	writeDB(t, oldDir, "cczj_video.db", "old-database")
	// 新落点是个普通文件：复制必定失败，而它下面的 .db 也 Stat 不到，
	// 于是走到复制分支而不是「两处都有库」分支。
	notADir := filepath.Join(root, "config")
	if err := os.WriteFile(notADir, []byte("not a directory"), 0644); err != nil {
		t.Fatal(err)
	}

	checkMigration(t, "复制失败", migrateLegacyDatabase(oldDir, notADir), legacyBlocked)

	if got := readDB(t, oldDir, "cczj_video.db"); got != "old-database" {
		t.Fatalf("旧目录被改动了: %q", got)
	}
}
