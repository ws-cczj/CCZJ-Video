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

// 2.1.0 的库在 exe 旁边，2.2.0 起在用户配置目录。原地热替换之后新目录是空的，
// 用户看到的就是「更新完数据没了」——这一条把旧库搬过来。
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

	migrateLegacyDatabase(oldDir, newDir)

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

// 两处都有库时猜哪边是"真的"都会覆盖掉用户的一半数据，所以什么都不做。
func TestMigrateLegacyDatabaseNeverOverwritesExistingDatabase(t *testing.T) {
	root := t.TempDir()
	oldDir := filepath.Join(root, "legacy")
	newDir := filepath.Join(root, "config")
	writeDB(t, oldDir, "cczj_video.db", "old-database")
	writeDB(t, newDir, "cczj_video.db", "live-database")

	migrateLegacyDatabase(oldDir, newDir)

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

	migrateLegacyDatabase(dir, filepath.Join(dir, "..", "data"))

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

	migrateLegacyDatabase(oldDir, newDir)

	if got := readDB(t, newDir, "cczj_video.db"); got != "" {
		t.Fatalf("没有旧库却写出了文件: %q", got)
	}
}
