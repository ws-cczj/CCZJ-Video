package db

import (
	"cczjVideo/app/model"
	"testing"
	"time"
)

const (
	recycleSource  = "recycle_test"
	recycleOther   = "recycle_other"
	recycleName    = "Recycle bin title"
	recycleNameTwo = "Recycle bin other title"
)

func seedRecycleRow(t *testing.T, sourceKey, vodID, name string) {
	t.Helper()
	if err := UpsertCatalogItems(sourceKey, []*model.Video{{
		VodId: model.FlexibleString(vodID), TypeId: "recycle", TypeName: "Recycle test type", VodName: name,
	}}); err != nil {
		t.Fatalf("seed %s:%s: %v", sourceKey, vodID, err)
	}
}

// visibleFavorites 返回收藏列表里属于某个源的条目，走真实查询以覆盖投影过滤。
func visibleFavorites(t *testing.T, sourceKey string) int {
	t.Helper()
	rows, err := GetFavorites(1, 500)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, row := range rows {
		if row.SourceKey == sourceKey {
			n++
		}
	}
	return n
}

func countRows(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := DB().Get(&n, query, args...); err != nil {
		t.Fatal(err)
	}
	return n
}

func clearRecycleFixtures(t *testing.T) {
	t.Helper()
	for _, key := range []string{recycleSource, recycleOther} {
		if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, key); err != nil {
			t.Fatal(err)
		}
		if _, err := DB().Exec(`DELETE FROM favorites WHERE source_key=?`, key); err != nil {
			t.Fatal(err)
		}
	}
}

func TestRecycleBinRestoreKeepsFavorites(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	clearRecycleFixtures(t)
	defer clearRecycleFixtures(t)

	seedRecycleRow(t, recycleSource, "gone-1", recycleName)
	seedRecycleRow(t, recycleSource, "keep-1", recycleName)
	seedRecycleRow(t, recycleOther, "gone-2", recycleNameTwo)

	globalID, err := GetOrCreateGlobalID(recycleName, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := AddFavoriteByIdentity(globalID, recycleSource, "gone-1"); err != nil {
		t.Fatal(err)
	}
	if got := visibleFavorites(t, recycleSource); got != 1 {
		t.Fatalf("favorites before delete = %d; want 1", got)
	}

	if err := DeleteCatalogVideo(recycleSource, "gone-1"); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCatalogVideo(recycleOther, "gone-2"); err != nil {
		t.Fatal(err)
	}

	// 删除后收藏必须跟着藏起来（这是"删除不丢收藏"的前提）。
	if got := visibleFavorites(t, recycleSource); got != 0 {
		t.Fatalf("favorites after delete = %d; want hidden", got)
	}

	items, total, err := ListRecycleBin("", 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if total < 2 || len(items) < 2 {
		t.Fatalf("cross-source recycle bin = %d/%d; want at least 2", len(items), total)
	}
	var first *RecycleItem
	for i := range items {
		if items[i].SourceKey == recycleSource && items[i].VodId == "gone-1" {
			first = &items[i]
		}
	}
	if first == nil {
		t.Fatalf("recycle bin missing %s:gone-1: %+v", recycleSource, items)
	}
	if first.VodName != recycleName || first.GlobalId <= 0 {
		t.Fatalf("recycle entry lacks identity: %+v", first)
	}
	// deleted_at 必须自带 UTC 标记：updated_at 是 CURRENT_TIMESTAMP 写的，
	// 少了 Z 前端会按本地时间解析，删除时间整整差出一个时区。
	deletedAt, err := time.Parse(time.RFC3339, first.DeletedAt)
	if err != nil {
		t.Fatalf("deleted_at = %q; want RFC3339 UTC text: %v", first.DeletedAt, err)
	}
	if drift := time.Since(deletedAt); drift < -time.Minute || drift > time.Hour {
		t.Fatalf("deleted_at = %q; want a recent UTC timestamp, drift=%v", first.DeletedAt, drift)
	}

	// 按源过滤只返回该源的条目。
	own, ownTotal, err := ListRecycleBin(recycleSource, 1, 50)
	if err != nil {
		t.Fatal(err)
	}
	if ownTotal != 1 || own[0].VodId != "gone-1" {
		t.Fatalf("source-filtered recycle bin = %d %+v; want only gone-1", ownTotal, own)
	}

	// 还在库里的条目不可能被这个入口删掉。
	if _, err := PurgeCatalogVideo(recycleSource, "keep-1"); err == nil {
		t.Fatal("PurgeCatalogVideo accepted an active row")
	}
	if err := RestoreCatalogVideo(recycleSource, "keep-1"); err == nil {
		t.Fatal("RestoreCatalogVideo accepted an active row")
	}

	if err := RestoreCatalogVideo(recycleSource, "gone-1"); err != nil {
		t.Fatal(err)
	}
	if got := lifecycleStateOf(t, recycleSource, "gone-1"); got != "active" {
		t.Fatalf("after restore lifecycle_state = %q; want active", got)
	}
	if got := visibleFavorites(t, recycleSource); got != 1 {
		t.Fatalf("favorites after restore = %d; want the favorite visible again", got)
	}
	if _, total, err := ListRecycleBin(recycleSource, 1, 50); err != nil || total != 0 {
		t.Fatalf("restored entry still listed in the recycle bin (total=%d err=%v)", total, err)
	}

	// 彻底删除只碰回收站里的行，删完连行本身都不存在。
	if _, err := PurgeCatalogVideo(recycleOther, "gone-2"); err != nil {
		t.Fatal(err)
	}
	if rows := countRows(t, `SELECT COUNT(1) FROM source_videos WHERE source_key=?`, recycleOther); rows != 0 {
		t.Fatalf("purged source still has %d rows; want 0", rows)
	}

	// 清空：活着的条目一条都不能少。
	seedRecycleRow(t, recycleSource, "gone-3", recycleName)
	if err := DeleteCatalogVideo(recycleSource, "gone-3"); err != nil {
		t.Fatal(err)
	}
	deleted, err := ClearRecycleBin(recycleSource)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Fatalf("ClearRecycleBin deleted %d; want 1", deleted)
	}
	if active := countRows(t, `SELECT COUNT(1) FROM source_videos WHERE source_key=? AND lifecycle_state='active'`, recycleSource); active != 2 {
		t.Fatalf("active rows after clear = %d; want keep-1 + gone-1 untouched", active)
	}
	keys, err := RecycleSourceKeys("")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if key == recycleSource {
			t.Fatalf("source %q still reported in recycle keys %v", key, keys)
		}
	}
}
