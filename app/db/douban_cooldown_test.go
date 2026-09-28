package db

import (
	"testing"
	"time"

	"github.com/jmoiron/sqlx"
)

// useSQLiteSingleton 把包级单例指到本次测试的临时库，返回的还原函数交给 t.Cleanup。
func useSQLiteSingleton(t *testing.T, database *sqlx.DB, dir string) func() {
	t.Helper()
	prevInstance, prevDir := instance, dataDir
	instance, dataDir = database, dir
	return func() { instance, dataDir = prevInstance, prevDir }
}

func seedGlobalVideoRows(t *testing.T, database *sqlx.DB, count int) []int {
	t.Helper()
	ids := make([]int, 0, count)
	for i := 0; i < count; i++ {
		res, err := database.Exec(`INSERT INTO global_video (vod_name) VALUES ('同一部片')`)
		if err != nil {
			t.Fatalf("seed row: %v", err)
		}
		id, err := res.LastInsertId()
		if err != nil {
			t.Fatalf("seed row id: %v", err)
		}
		ids = append(ids, int(id))
	}
	return ids
}

// 冷却必须写成哪一行、读就从哪一行。旧实现写按智能匹配后的 id、读按 vod_name 任取
// 一行、清空按 vod_name 全量扇出：同名两行里的一行进冷却，另一行的计数会被一起抹掉，
// 24 小时冷却形同虚设。
//
// 这里刻意用同名且 name_norm 为空的行——uq_global_identity 只约束 name_norm<>” 的行，
// 所以这正是采集库里真实存在的形态。
func TestDoubanSearchFailureCooldownIsRowScoped(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	t.Cleanup(useSQLiteSingleton(t, database, dir))
	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}

	ids := seedGlobalVideoRows(t, database, 2)

	for i := 0; i < searchFailureCooldownThreshold; i++ {
		if err := MarkDoubanSearchFailure(ids[0]); err != nil {
			t.Fatalf("mark failure %d: %v", i+1, err)
		}
	}
	if !IsDoubanSearchOnCooldown(ids[0]) {
		t.Fatalf("global_id=%d 累计到阈值仍未进冷却", ids[0])
	}
	if IsDoubanSearchOnCooldown(ids[1]) {
		t.Fatalf("global_id=%d 被同名邻居一起拖进了冷却", ids[1])
	}

	// 未到阈值时不得把已有的冷却抹成 NULL：那样这条本来该搁置一天的记录
	// 会因为一次无关紧要的失败重新排进队列。
	future := time.Now().Add(2 * time.Hour).Format(doubanCooldownFormat)
	if _, err := database.Exec(`UPDATE global_video SET douban_search_failures = 1, douban_cooldown_until = ? WHERE id = ?`, future, ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := MarkDoubanSearchFailure(ids[1]); err != nil {
		t.Fatal(err)
	}
	if !IsDoubanSearchOnCooldown(ids[1]) {
		t.Fatal("未达阈值的失败抹掉了已有的冷却")
	}
	// 反过来也要成立：冷却过期后必须重新放行，否则一次查无此片就永久否决。
	if _, err := database.Exec(`UPDATE global_video SET douban_cooldown_until = ? WHERE id = ?`,
		time.Now().Add(-time.Hour).Format(doubanCooldownFormat), ids[1]); err != nil {
		t.Fatal(err)
	}
	if IsDoubanSearchOnCooldown(ids[1]) {
		t.Fatal("冷却已经过期，却仍把这行挡在队列外")
	}

	if err := ClearDoubanSearchFailure(ids[0]); err != nil {
		t.Fatal(err)
	}
	if IsDoubanSearchOnCooldown(ids[0]) {
		t.Fatal("清除后仍在冷却")
	}
	var otherFailures int
	if err := database.Get(&otherFailures, `SELECT douban_search_failures FROM global_video WHERE id = ?`, ids[1]); err != nil {
		t.Fatal(err)
	}
	if otherFailures != 2 {
		t.Fatalf("邻居行的失败计数被扇出改动：%d，期望保持 2", otherFailures)
	}

	// 手动搜索没有行身份，GlobalID 为 0：它不该被任何一条记录的冷却挡住。
	if IsDoubanSearchOnCooldown(0) {
		t.Fatal("GlobalID=0 的手动搜索被冷却挡住")
	}
	if err := MarkDoubanSearchFailure(0); err != nil {
		t.Fatalf("GlobalID=0 不该写库: %v", err)
	}
}

// 队列按「上次被豆瓣试过」轮转：反复失败的队头必须让位，否则一批补不全的记录会把
// 整轮配额吃光，后面的行永远排不到。
func TestDoubanQueueRotatesByLastAttempt(t *testing.T) {
	dir := t.TempDir()
	database := openFreshSQLite(t, dir)
	t.Cleanup(useSQLiteSingleton(t, database, dir))
	if err := createTables(); err != nil {
		t.Fatalf("createTables: %v", err)
	}
	ids := seedGlobalVideoRows(t, database, 3)

	first, err := GetDoubanInfoMissingSubjectID(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 2 || first[0].GlobalID != ids[0] || first[1].GlobalID != ids[1] {
		t.Fatalf("首轮队列 = %v，期望前两条", globalIDsOf(first))
	}
	for _, row := range first {
		if err := MarkDoubanAttempt(row.GlobalID); err != nil {
			t.Fatal(err)
		}
	}

	second, err := GetDoubanInfoMissingSubjectID(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second) == 0 || second[0].GlobalID != ids[2] {
		t.Fatalf("轮转后队首 = %v，期望从没被试过的 %d 排在最前", globalIDsOf(second), ids[2])
	}

	// 冷却是硬过滤：进了冷却的行连轮转的资格都没有，否则每轮都要为它白跑一次查询。
	if err := SetDoubanCooldown(ids[2]); err != nil {
		t.Fatal(err)
	}
	third, err := GetDoubanInfoMissingSubjectID(2)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range third {
		if row.GlobalID == ids[2] {
			t.Fatalf("冷却中的 %d 仍在队列里: %v", ids[2], globalIDsOf(third))
		}
	}

	// 打点只写轮转时钟，不许顺手动 updated_at：那一列是兄弟继承和列表排序的依据。
	var before, after string
	if err := database.Get(&before, `SELECT COALESCE(updated_at, '') FROM global_video WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := MarkDoubanAttempt(ids[0]); err != nil {
		t.Fatal(err)
	}
	if err := database.Get(&after, `SELECT COALESCE(updated_at, '') FROM global_video WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("MarkDoubanAttempt 顺带改了 updated_at: %q -> %q", before, after)
	}
}

func globalIDsOf(rows []*DoubanInfoRow) []int {
	ids := make([]int, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.GlobalID)
	}
	return ids
}

// 老库必须靠迁移拿到轮转时钟，而不是靠删库重建。
func TestMigrateDoubanAttemptClockAddsColumnInPlace(t *testing.T) {
	database := openFreshSQLite(t, t.TempDir())
	if _, err := database.Exec(`CREATE TABLE global_video (id INTEGER PRIMARY KEY, vod_name TEXT NOT NULL DEFAULT '')`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO global_video (vod_name) VALUES ('已有的一行')`); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		tx, err := database.Beginx()
		if err != nil {
			t.Fatal(err)
		}
		if err := migrateDoubanAttemptClock(tx); err != nil {
			t.Fatalf("attempt %d: %v", i, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}

	// 老行留 NULL 才对：SQLite 的 ASC 把 NULL 排在最前，它们才会优先补进队列。
	var unattempted int
	if err := database.Get(&unattempted, `SELECT COUNT(*) FROM global_video WHERE douban_last_attempt_at IS NULL`); err != nil {
		t.Fatal(err)
	}
	if unattempted != 1 {
		t.Fatalf("未打点的老行 = %d，期望 1", unattempted)
	}

	// 跑过 dev 的库里可能已经建过那条被 EXPLAIN 证伪的轮转索引，重放迁移要把它收掉。
	if _, err := database.Exec(`CREATE INDEX idx_gv_douban_attempt ON global_video (douban_last_attempt_at, id)`); err != nil {
		t.Fatal(err)
	}
	txIdx, err := database.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	if err := migrateDoubanAttemptClock(txIdx); err != nil {
		t.Fatalf("作废轮转索引: %v", err)
	}
	if err := txIdx.Commit(); err != nil {
		t.Fatal(err)
	}
	var stale int
	if err := database.Get(&stale, `SELECT COUNT(*) FROM sqlite_master WHERE type='index' AND name='idx_gv_douban_attempt'`); err != nil {
		t.Fatal(err)
	}
	if stale != 0 {
		t.Fatal("轮转索引没有被迁移作废")
	}

	// 表还不存在的首启动不该把迁移卡住。
	empty := openFreshSQLite(t, t.TempDir())
	tx, err := empty.Beginx()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := migrateDoubanAttemptClock(tx); err != nil {
		t.Fatalf("缺表时必须跳过: %v", err)
	}
}
