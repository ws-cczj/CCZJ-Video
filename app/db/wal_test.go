package db

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jmoiron/sqlx"
)

// WAL 的账要在退出时结清，否则代价是看不见的：写入一直堆在 -wal 里，主库看着没变大，
// -wal 却只增不减；进程退出时没人归并，下次启动还得先重放一遍才读到最新数据。
// 这里钉两件事——每条连接都带上这组 pragma（写在 DSN 里才对池子里所有连接生效），
// 以及 TRUNCATE 归并真能把 -wal 收回到 0 字节。
func TestWALPragmasApplyAndCheckpointTruncates(t *testing.T) {
	dir := t.TempDir()
	database, err := sqlx.Connect("sqlite", sqliteDSN(filepath.Join(dir, "cczj_video.db")))
	if err != nil {
		t.Fatalf("按生产 DSN 连接: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })

	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	t.Cleanup(func() { instance, dataDir = prevInstance, prevDir })

	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	type pragma struct {
		name string
		want any
	}
	for _, p := range []pragma{
		{"journal_mode", "wal"},
		{"journal_size_limit", walTruncateBytes},
		{"wal_autocheckpoint", walAutoCheckpointPages},
	} {
		var got any
		// journal_mode 返回文本，其余返回整数，所以按 any 比。
		if err := database.QueryRow("PRAGMA " + p.name).Scan(&got); err != nil {
			t.Fatalf("读 PRAGMA %s: %v", p.name, err)
		}
		switch want := p.want.(type) {
		case string:
			if asText, _ := got.(string); asText != want {
				t.Errorf("PRAGMA %s = %v，想要 %q", p.name, got, want)
			}
		case int:
			// modernc 把整型 pragma 报成 int64。
			if asNum, _ := got.(int64); int(asNum) != want {
				t.Errorf("PRAGMA %s = %v，想要 %d", p.name, got, want)
			}
		}
	}

	// 灌一批够不着自动归并阈值（wal_autocheckpoint 页）的写入，让它们留在 -wal 里。
	if _, err := database.Exec(`
		INSERT INTO source_videos (source_key, source_vod_id, global_id, type_name, vod_name, lifecycle_state, updated_at)
		SELECT 'src-wal', 'vod-'||i, i, '国产', '影片 '||i, 'active', '2026-01-01 00:00:00'
		FROM (WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i < 400) SELECT i FROM n)`); err != nil {
		t.Fatalf("写入测试数据: %v", err)
	}
	wal := filepath.Join(dir, "cczj_video.db-wal")
	before := walSize(t, wal)
	if before == 0 {
		t.Fatal("写入后 -wal 是空的，后面的截断断言没有意义")
	}

	var busy, logPages, moved int
	if err := walCheckpoint(&busy, &logPages, &moved); err != nil {
		t.Fatalf("TRUNCATE 归并: %v", err)
	}
	if busy != 0 {
		t.Fatalf("归并报告被占用（-wal %d 页、已归并 %d 页），退出时收不了尾", logPages, moved)
	}
	if after := walSize(t, wal); after != 0 {
		t.Errorf("归并后 -wal 仍有 %d 字节，TRUNCATE 没起作用", after)
	}

	// 归并之后数据必须还在主库里读得到，否则「截断」就是丢数据。
	var count int
	if err := database.Get(&count, `SELECT COUNT(*) FROM source_videos WHERE source_key='src-wal'`); err != nil {
		t.Fatal(err)
	}
	if count != 400 {
		t.Errorf("归并后读到 %d 行，想要 400 行", count)
	}
}

func walSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatalf("读 -wal 状态: %v", err)
	}
	return info.Size()
}
