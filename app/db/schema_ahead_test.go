package db

import (
	"strconv"
	"testing"
)

// 那份「旧结构体」：只有它自己认识的列，正如 2.1.0 的 model.Source 没有 auto_disabled_at。
type oldProbe struct {
	ID      int `db:"id"`
	Enabled int `db:"enabled"`
}

func withRestoredState(t *testing.T) {
	t.Helper()
	saved := schemaAheadOfBuild
	t.Cleanup(func() { schemaAheadOfBuild = saved })
	schemaAheadOfBuild = struct {
		dbVersion    int
		buildVersion int
	}{}
}

func TestApplySchemaAheadLeavesNormalAndUpgradePathAlone(t *testing.T) {
	withRestoredState(t)
	database := openFreshSQLite(t, t.TempDir())

	lenient, ahead := applySchemaAhead(database, LatestSchemaVersion())
	if ahead || lenient != database {
		t.Fatalf("同版本被判成降级: ahead=%v 换了连接=%v", ahead, lenient != database)
	}
	if _, _, newer := DataNewerThanBuild(); newer {
		t.Fatal("同版本不该报「库比程序新」")
	}

	lenient, ahead = applySchemaAhead(database, LatestSchemaVersion()-1)
	if ahead || lenient != database {
		t.Fatal("落后于最新迁移该走升级迁移，不是降级判定")
	}
}

// 第 10 条的正面答案：库比程序新时，多出来的那一列不该把整条查询炸掉。
func TestApplySchemaAheadKeepsQueriesAliveOnUnknownColumns(t *testing.T) {
	withRestoredState(t)
	database := openFreshSQLite(t, t.TempDir())

	if _, err := database.Exec(`CREATE TABLE ahead_probe (id INTEGER PRIMARY KEY, enabled INTEGER, brand_new_column TEXT DEFAULT 'x')`); err != nil {
		t.Fatalf("建探针表: %v", err)
	}
	if _, err := database.Exec(`INSERT INTO ahead_probe (id, enabled) VALUES (1, 1)`); err != nil {
		t.Fatalf("塞探针行: %v", err)
	}

	future := LatestSchemaVersion() + 3
	if _, err := database.Exec("PRAGMA user_version = " + strconv.Itoa(future)); err != nil {
		t.Fatalf("打上更高的库版本: %v", err)
	}

	// 严格映射下这就是旧版界面上那条红提示：结果集比结构体多一列，sqlx 当场报错。
	var rows []oldProbe
	if err := database.Select(&rows, "SELECT * FROM ahead_probe"); err == nil {
		t.Fatal("严格映射本应因未知列报错，却成功了")
	}

	lenient, ahead := applySchemaAhead(database, future)
	if !ahead {
		t.Fatal("库比构建高 3 版却没判成降级")
	}
	if err := lenient.Select(&rows, "SELECT * FROM ahead_probe"); err != nil {
		t.Fatalf("降级判定后仍被未知列挡住: %v", err)
	}
	if len(rows) != 1 || rows[0].ID != 1 || rows[0].Enabled != 1 {
		t.Fatalf("查回来的行不对: %+v", rows)
	}

	dbVersion, buildVersion, newer := DataNewerThanBuild()
	if !newer || dbVersion != future || buildVersion != LatestSchemaVersion() {
		t.Fatalf("回报的版本号不对: newer=%v db=%d build=%d", newer, dbVersion, buildVersion)
	}

	// 换的必须只是映射策略：底层还是同一个连接池，池上限与已打开的库自然跟着走。
	if lenient.DB != database.DB {
		t.Fatal("Unsafe() 之后底层连接池不是原来那一份")
	}
}
