package db

import (
	"testing"

	"github.com/jmoiron/sqlx"
)

// useMigratedTestDB 建一个跑完整套建表与迁移的临时库，并把包级 instance/dataDir 指过去，
// 让直接调用 package 函数的测试走的就是这块库。
func useMigratedTestDB(t *testing.T) *sqlx.DB {
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
	return database
}

// 详情页的「换源看同一部」只认本地身份关联：软删除的源不能当备选，
// 顺序要稳定（按 source_key），没有身份时直接报错而不是返回空列表骗过前端。
func TestFindSourcesByGlobalIdSkipsDeletedRows(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id) VALUES (401, '同片', '同片', 0);
		INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, lifecycle_state) VALUES
			('src-b', 'b-401', 401, '同片', 'active'),
			('src-a', 'a-401', 401, '同片', 'active'),
			('src-c', 'c-401', 401, '同片', 'deleted'),
			('src-d', 'd-999', 999, '别片', 'active');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	refs, err := FindSourcesByGlobalId(401)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 2 || refs[0].SourceKey != "src-a" || refs[1].SourceKey != "src-b" {
		t.Fatalf("换源列表=%+v, want src-a,src-b（排除软删除并按源排序）", refs)
	}
	if refs[0].VodId != "a-401" {
		t.Errorf("vod_id=%q, want a-401", refs[0].VodId)
	}
	if _, err := FindSourcesByGlobalId(0); err == nil {
		t.Error("global_id<=0 应报错")
	}
}
