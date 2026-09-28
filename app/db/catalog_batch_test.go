package db

import (
	"errors"
	"fmt"
	"testing"

	"cczjVideo/app/model"
)

// 一轮采集有几百页。以前每页都重新读一遍 global_video 并重建身份索引，
// 库越大越慢；批次把这份载入收敛成一次，代价是失败页必须作废整批内存身份。

const batchSourceA = "batch_src_a"
const batchSourceB = "batch_src_b"
const batchTypeName = "Batch reuse type"

func cleanupBatchFixture(t *testing.T) {
	t.Helper()
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key IN (?, ?)`, batchSourceA, batchSourceB); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM global_video WHERE name_norm IN (?, ?, ?)`,
		normalizeTitle("批次复用甲"), normalizeTitle("批次复用乙"), normalizeTitle("批次复用丙")); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM global_types WHERE type_name = ?`, batchTypeName); err != nil {
		t.Fatal(err)
	}
}

func batchVideo(vodID, title string) *model.Video {
	return &model.Video{
		VodId:    model.FlexibleString(vodID),
		TypeId:   model.FlexibleString("batch"),
		TypeName: batchTypeName,
		VodName:  title,
		VodYear:  "2024",
		VodTime:  "2026-01-01 00:00:00",
	}
}

func TestCatalogBatchReusesIdentitiesAcrossPages(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	cleanupBatchFixture(t)
	defer cleanupBatchFixture(t)

	batch := NewCatalogBatch()
	if err := UpsertCatalogItemsBatch(batch, batchSourceA, []*model.Video{batchVideo("b-1", "批次复用甲"), batchVideo("b-2", "批次复用乙")}); err != nil {
		t.Fatal(err)
	}
	first := batch.ident
	if first == nil {
		t.Fatal("批次没有载入身份索引")
	}
	loadedAfterPage1 := len(first.videoIndex.candidates)

	// 第二页换个源、带一部上一页已经建过身份的片：复用批次时 global_video 不该多行。
	if err := UpsertCatalogItemsBatch(batch, batchSourceB, []*model.Video{batchVideo("b-3", "批次复用甲"), batchVideo("b-4", "批次复用丙")}); err != nil {
		t.Fatal(err)
	}
	if batch.ident != first {
		t.Fatal("第二页重建了解析器：每页全表载入又回来了")
	}
	if len(first.videoIndex.candidates) != loadedAfterPage1+1 {
		t.Fatalf("索引里候选从 %d 变成 %d，只应新增一部新片", loadedAfterPage1, len(first.videoIndex.candidates))
	}

	var identities int
	if err := DB().Get(&identities, `SELECT COUNT(*) FROM global_video WHERE name_norm IN (?, ?)`,
		normalizeTitle("批次复用甲"), normalizeTitle("批次复用丙")); err != nil {
		t.Fatal(err)
	}
	if identities != 2 {
		t.Fatalf("global_video 有 %d 行，甲+丙 各一行才对", identities)
	}

	rows, err := DB().Query(`SELECT source_vod_id, global_id FROM source_videos WHERE source_key IN (?, ?) ORDER BY source_vod_id`, batchSourceA, batchSourceB)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	byVodID := map[string]int64{}
	for rows.Next() {
		var vodID string
		var gid int64
		if err := rows.Scan(&vodID, &gid); err != nil {
			t.Fatal(err)
		}
		byVodID[vodID] = gid
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// b-1（源 A）和 b-3（源 B）是同一部片，必须挂在同一条身份上。
	if byVodID["b-1"] == 0 || byVodID["b-1"] != byVodID["b-3"] {
		t.Fatalf("两个源没有挂在同一部片上: %+v", byVodID)
	}
	if byVodID["b-2"] == 0 || byVodID["b-4"] == 0 || byVodID["b-2"] == byVodID["b-1"] {
		t.Fatalf("不同片名不该并成一条身份: %+v", byVodID)
	}
}

// 失败页的事务会回滚掉它新建的身份行，批次内存里不能再留着那些 id。
func TestCatalogBatchDropsIdentityStateAfterFailedPage(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	batch := NewCatalogBatch()
	first, err := batch.begin()
	if err != nil {
		t.Fatal(err)
	}
	batch.end(fmt.Errorf("page failed: %w", errors.New("boom")))
	if batch.ident != nil {
		t.Fatal("失败之后批次仍留着旧索引")
	}
	second, err := batch.begin()
	if err != nil {
		t.Fatal(err)
	}
	if second == first {
		t.Fatal("失败之后没有重新载入身份")
	}
	batch.end(nil)
	third, err := batch.begin()
	if err != nil {
		t.Fatal(err)
	}
	if third != second {
		t.Fatal("成功页之间也重建了解析器")
	}
}

// 没有批次的调用方保持原样：一次写入自建自用，不留常驻内存。
func TestUpsertWithoutBatchBuildsThrowawayResolver(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	cleanupBatchFixture(t)
	defer cleanupBatchFixture(t)

	if err := UpsertCatalogItems(batchSourceA, []*model.Video{batchVideo("b-9", "批次复用乙")}); err != nil {
		t.Fatal(err)
	}
	var got int
	if err := DB().Get(&got, `SELECT COUNT(*) FROM source_videos WHERE source_key=? AND source_vod_id=?`, batchSourceA, "b-9"); err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Fatalf("无批次写入没有落库: %d 行", got)
	}
}
