package db

import (
	"strings"
	"testing"
)

func TestMergeCandidatesGroupSplitDoubanRows(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id, year, douban_id, douban_score) VALUES
			(401, '长津湖', '长津湖', 0, '2021', 'd-1001', ''),
			(402, '长津湖 4K', '长津湖4k', 0, '2021', 'd-1001', '8.5'),
			(403, '别的片', '别的片', 0, '', '', '');
		INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, lifecycle_state) VALUES
			('src-a', 'a-401', 401, '长津湖', 'active'),
			('src-b', 'b-402', 402, '长津湖！', 'active');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	groups, err := ListIdentityMergeCandidates(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("候选组=%d, want 1：%+v", len(groups), groups)
	}
	group := groups[0]
	if group.Reason != mergeReasonDoubanID || group.Key != "d-1001" {
		t.Errorf("组=%s/%s, want douban_id/d-1001", group.Reason, group.Key)
	}
	if len(group.Rows) != 2 {
		t.Fatalf("组内 %d 条, want 2", len(group.Rows))
	}
	if group.Rows[0].CatalogRows != 1 {
		t.Errorf("目录引用数=%d, want 1", group.Rows[0].CatalogRows)
	}
}

// 同名但年份对不上的是两部片，绝不能进候选队列。
func TestMergeCandidatesSkipSameNameDifferentYear(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id, year) VALUES
			(411, '大白鲨', '大白鲨', 1, '1975'), (412, '大白鲨', '大白鲨', 2, '2023');
		INSERT INTO global_types (id, type_name, collect_enabled) VALUES (1, '电影', 1), (2, '纪录片', 1);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	groups, err := ListIdentityMergeCandidates(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("年份冲突的同名片不该成为候选，got %+v", groups)
	}
}

func TestMergeGlobalVideoIdentitiesRepointsEveryReference(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id, year, douban_id, douban_score, douban_search_failures, douban_cooldown_until) VALUES
			(421, '甲', '甲', 0, '', '', '', 3, '2099-01-01 00:00:00'),
			(422, '甲', '甲！', 0, '2020', 'd-2002', '9.0', 1, '');
		INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, lifecycle_state) VALUES
			('src-a', 'a-421', 421, '甲', 'active'),
			('src-b', 'b-421', 421, '甲', 'active'),
			('src-a', 'a-422', 422, '甲', 'active');
		INSERT INTO favorites (global_id, source_key, vod_id) VALUES
			(421, 'src-a', 'a-421'), (422, 'src-a', 'a-422');
		INSERT INTO watch_history (global_id, source_key, vod_id, ep_num, position) VALUES
			(421, 'src-a', 'a-421', 1, 30), (422, 'src-a', 'a-422', 1, 80), (422, 'src-a', 'a-422', 2, 12);
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	keep, merged, err := MergeGlobalVideoIdentities([]int64{421, 422})
	if err != nil {
		t.Fatal(err)
	}
	// 存活行是有豆瓣 ID 且字段更全的 422，而不是 id 更小的 421。
	if keep != 422 || merged != 1 {
		t.Fatalf("keep=%d merged=%d, want 422/1", keep, merged)
	}

	var left int
	if err := database.Get(&left, `SELECT COUNT(*) FROM global_video WHERE id=421`); err != nil || left != 0 {
		t.Fatalf("被并掉的行应删除，left=%d err=%v", left, err)
	}
	var refs []string
	if err := database.Select(&refs, `SELECT source_key||'/'||source_vod_id FROM source_videos WHERE global_id=422 ORDER BY source_key, source_vod_id`); err != nil {
		t.Fatal(err)
	}
	if strings.Join(refs, ",") != "src-a/a-421,src-a/a-422,src-b/b-421" {
		t.Errorf("目录引用=%v, want 三条全部改指 422", refs)
	}
	var favorites int
	if err := database.Get(&favorites, `SELECT COUNT(*) FROM favorites WHERE global_id=422`); err != nil || favorites != 2 {
		t.Fatalf("收藏=%d err=%v, want 2 条并进存活身份", favorites, err)
	}
	type progress struct {
		EpNum    int     `db:"ep_num"`
		Position float64 `db:"position"`
	}
	var history []progress
	if err := database.Select(&history, `SELECT ep_num, position FROM watch_history WHERE global_id=422 ORDER BY ep_num`); err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 || history[0].Position != 80 || history[1].Position != 12 {
		t.Errorf("观看进度=%v, want 第1集取更大的一侧 80，第2集 12", history)
	}
	// 重试状态只增不减：失败次数取更大、冷却保留最晚的那条，
	// 否则合并等于把仍在冷却的记录放回热路径去撞豆瓣。
	var failures int
	var cooldown string
	if err := database.QueryRow(`SELECT douban_search_failures, COALESCE(douban_cooldown_until,'') FROM global_video WHERE id=422`).Scan(&failures, &cooldown); err != nil {
		t.Fatal(err)
	}
	if failures != 3 || cooldown != "2099-01-01 00:00:00" {
		t.Errorf("重试状态 failures=%d cooldown=%q, want 3 / 2099-01-01 00:00:00", failures, cooldown)
	}

	// 合并后 422 在两个源里都有货（src-a 有两条目录行也只算一个源）。
	crossRefs, err := FindSourcesByGlobalId(422)
	if err != nil {
		t.Fatal(err)
	}
	sourceSet := make(map[string]bool, len(crossRefs))
	for _, ref := range crossRefs {
		sourceSet[ref.SourceKey] = true
	}
	if len(crossRefs) != 3 || len(sourceSet) != 2 {
		t.Errorf("合并后换源列表=%d 条 / %d 个源, want 3 条 2 个源：%+v", len(crossRefs), len(sourceSet), crossRefs)
	}
}

func TestMergeGlobalVideoIdentitiesRejectsBadGroups(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id, douban_id) VALUES
			(431, '甲', '甲', 0, 'd-1'), (432, '乙', '乙', 0, 'd-2'), (433, '甲', '甲！', 0, '');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{431}); err == nil {
		t.Error("单条身份不该叫合并")
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{431, 431}); err == nil {
		t.Error("重复的同一条身份不该被接受")
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{431, 432}); err == nil {
		t.Error("既不同名又不同豆瓣 ID 的两条不该被并掉")
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{431, 433}); err != nil {
		t.Errorf("同名一条缺豆瓣 ID，应允许合并：%v", err)
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{431, 999}); err == nil {
		t.Error("不存在的身份不该被接受")
	}
	var count int
	if err := database.Get(&count, `SELECT COUNT(*) FROM global_video`); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("global_video 剩 %d 条, want 失败的那些调用没有改动库", count)
	}
}
