package db

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jmoiron/sqlx"
)

// 跨源合并视图的全部意义在于「一部片一张卡片」，所以这里钉四件事：
// 同身份确实并成一行、代表行可预期、游标翻页不重不漏、筛选改的是卡片集合
// 而不是把同一身份拆回多张。
func TestUnionPageMergesSameGlobalIDAcrossSources(t *testing.T) {
	database := useUnionTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_video (id, vod_name, name_norm, type_id) VALUES
			(101, '甲', '甲', 0), (102, '乙', '乙', 0), (103, '丙', '丙', 0);
		INSERT INTO source_videos (source_key, source_vod_id, global_id, type_name, vod_name, vod_year, vod_area, vod_time, lifecycle_state) VALUES
			('src-a', 'a-101', 101, '剧情', '甲', '2020', '大陆', '2026-01-01 10:00:00', 'active'),
			('src-b', 'b-101', 101, '剧情', '甲', '2020', '大陆', '2026-01-03 10:00:00', 'active'),
			('src-c', 'c-101', 101, '剧情', '甲', '2020', '大陆', '2026-01-02 10:00:00', 'deleted'),
			('src-a', 'a-102', 102, '喜剧', '乙', '2021', '美国', '2026-01-05 10:00:00', 'active'),
			('src-a', 'a-103', 0,  '动作', '丙', '2022', '日本', '2026-01-06 10:00:00', 'active');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	page, err := GetCatalogUnionPage(FilterParams{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	// 6 条目录行 → 2 张卡片：101 三兄弟（其中一条已删）、102 单源，103 没有身份不进视图。
	if page.Total != 2 || len(page.Videos) != 2 {
		t.Fatalf("want 2 merged cards, got total=%d rows=%d", page.Total, len(page.Videos))
	}
	// 排序看的是代表行的时间：乙(01-05) 在 甲(01-03) 前面，尽管 甲 在三个源里都有。
	if page.Videos[0].VodName != "乙" || page.Videos[1].VodName != "甲" {
		t.Fatalf("排序=%q,%q, want 乙,甲", page.Videos[0].VodName, page.Videos[1].VodName)
	}
	first := unionCardByName(t, page.Videos, "甲")
	if first.GlobalID != 101 {
		t.Fatalf("甲 的身份应是 101，got %d", first.GlobalID)
	}
	if first.SourceCount != 2 {
		t.Errorf("已删除的源不该算进源数，got %d", first.SourceCount)
	}
	if strings.Join(first.Sources, ",") != "src-a,src-b" {
		t.Errorf("源列表=%q, want src-a,src-b", strings.Join(first.Sources, ","))
	}
	// 代表行取该身份里 vod_time 最新的一条，详情页与播放页据此选路。
	if first.SourceKey != "src-b" || first.SourceVodID != "b-101" {
		t.Errorf("代表行=%s/%s, want src-b/b-101", first.SourceKey, first.SourceVodID)
	}
	if first.VodTime != "2026-01-03 10:00:00" {
		t.Errorf("代表行时间=%q", first.VodTime)
	}
	if page.NextCursor != "" {
		t.Errorf("没有下一页时不该发游标，got %q", page.NextCursor)
	}
}

func TestUnionPageWalksCursorWithoutRepeats(t *testing.T) {
	database := useUnionTestDB(t)
	// 30 个身份，前 10 个在两个源里各有一条，且两个源的时间戳交错。
	var rows []string
	for i := 1; i <= 30; i++ {
		rows = append(rows, fmt.Sprintf("(%d, '影片%d', '影片%d', 0)", i, i, i))
	}
	if _, err := database.Exec(`INSERT INTO global_video (id, vod_name, name_norm, type_id) VALUES ` + strings.Join(rows, ",")); err != nil {
		t.Fatalf("seed global_video: %v", err)
	}
	var catalog []string
	for i := 1; i <= 30; i++ {
		// 偶数身份在两个源里都有，且 src-b 更新；vod_time 逐条递增，保证翻页顺序唯一。
		hour := i % 24
		catalog = append(catalog, fmt.Sprintf("('src-a', 'a-%[1]d', %[1]d, '影片%[1]d', printf('2026-01-01 %02d:00:00', %[2]d))", i, hour))
		if i%2 == 0 {
			catalog = append(catalog, fmt.Sprintf("('src-b', 'b-%[1]d', %[1]d, '影片%[1]d', printf('2026-01-02 %02d:00:00', %[2]d))", i, hour))
		}
	}
	if _, err := database.Exec(`INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, vod_time) VALUES ` + strings.Join(catalog, ",")); err != nil {
		t.Fatalf("seed source_videos: %v", err)
	}

	seen := make(map[int64]bool)
	filter := FilterParams{PageSize: 7}
	pages := 0
	for {
		page, err := GetCatalogUnionPage(filter)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, video := range page.Videos {
			if seen[video.GlobalID] {
				t.Fatalf("第 %d 页重复返回身份 %d", pages, video.GlobalID)
			}
			seen[video.GlobalID] = true
		}
		if page.NextCursor == "" {
			break
		}
		filter.Cursor = page.NextCursor
		if pages > 10 {
			t.Fatal("游标没有收尾")
		}
	}
	if len(seen) != 30 || pages != 5 {
		t.Fatalf("翻到 %d 页共 %d 张卡片，want 5 页 30 张", pages, len(seen))
	}
}

func TestUnionPageFiltersCardsNotRows(t *testing.T) {
	database := useUnionTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO global_types (id, type_name, collect_enabled) VALUES (7, '电影', 1), (8, '剧集', 1), (9, '禁用', 0);
		INSERT INTO global_video (id, vod_name, name_norm, type_id) VALUES (201, '大白鲨', '大白鲨', 7), (202, '鲨滩', '鲨滩', 7), (203, '鲨海', '鲨海', 8), (204, '禁用片', '禁用片', 9);
		INSERT INTO source_videos (source_key, source_vod_id, global_id, source_type_id, global_type_id, type_name, vod_name, vod_year, vod_area, vod_time) VALUES
			('src-a', 'a-201', 201, 's7', 7, '电影', '大白鲨', '1975', '美国', '2026-02-01 10:00:00'),
			('src-b', 'b-201', 201, 's7', 7, '电影', '大白鲨 加长版', '1975', '美国', '2026-02-02 10:00:00'),
			('src-a', 'a-202', 202, 's7', 7, '电影', '鲨滩', '2012', '美国', '2026-02-03 10:00:00'),
			('src-a', 'a-203', 203, 's8', 8, '剧集', '鲨海', '2020', '英国', '2026-02-04 10:00:00'),
			('src-a', 'a-204', 204, 's9', 9, '禁用片类型', '禁用片', '2021', '大陆', '2026-02-05 10:00:00');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 关键词只命中兄弟行：卡片必须还在，且代表行换成命中的那条。
	keyword, err := GetCatalogUnionPage(FilterParams{Keyword: "加长版", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if keyword.Total != 1 || len(keyword.Videos) != 1 {
		t.Fatalf("关键词命中数=%d 行数=%d, want 1/1", keyword.Total, len(keyword.Videos))
	}
	if got := keyword.Videos[0]; got.SourceKey != "src-b" || got.SourceCount != 2 {
		t.Errorf("代表行=%s 源数=%d, want src-b 与 2 个源", got.SourceKey, got.SourceCount)
	}

	// 类型筛选按 global_type_id：源内编号跨源无意义，不能拿来过滤。
	byType, err := GetCatalogUnionPage(FilterParams{TypeId: "7", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if byType.Total != 2 {
		t.Errorf("类型 7 应有 2 张卡片，got %d", byType.Total)
	}
	if _, err := GetCatalogUnionPage(FilterParams{TypeId: "s7", PageSize: 10}); err != nil {
		t.Errorf("源内类型编号不该进 SQL：%v", err)
	}

	// 全局类型关掉采集后，整张卡片消失，总数同步掉。
	hidden, err := GetCatalogUnionPage(FilterParams{PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if hidden.Total != 3 {
		t.Fatalf("禁用类型不该出现在合并视图，total=%d", hidden.Total)
	}
	for _, video := range hidden.Videos {
		if video.VodName == "禁用片" {
			t.Fatal("禁用类型的卡片漏出来了")
		}
	}
}

// 年/地区选项要跨源汇总：单源 DISTINCT 走索引，逐源合并后去重排序。
func TestGetUnionYearsAndAreasMergesSources(t *testing.T) {
	database := useUnionTestDB(t)
	if _, err := database.Exec(`
		INSERT INTO sources (source_key, name, api_url, enabled) VALUES ('src-a', 'A', 'https://a.example.com', 1), ('src-b', 'B', 'https://b.example.com', 1);
		INSERT INTO global_video (id, vod_name, name_norm, type_id) VALUES (301, '片一', '片一', 0), (302, '片二', '片二', 0);
		INSERT INTO source_videos (source_key, source_vod_id, global_id, vod_name, vod_year, vod_area, lifecycle_state) VALUES
			('src-a', 'a-301', 301, '片一', '2020', '大陆', 'active'),
			('src-b', 'b-302', 302, '片二', '2021', '美国', 'active'),
			('src-b', 'b-301', 301, '片一', '2020', '美国', 'active'),
			('src-b', 'b-deleted', 302, '片二', '1999', '韩国', 'deleted');
	`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	years, areas, err := GetUnionYearsAndAreas()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(years, ",") != "2021,2020" {
		t.Errorf("年份=%v, want 倒序去重", years)
	}
	if strings.Join(areas, ",") != "大陆,美国" {
		t.Errorf("地区=%v, want 升序去重且排除软删除行", areas)
	}
}

// 详情页的「换源看同一部」只认本地身份关联：软删除的源不能当备选，
// 顺序要稳定（按 source_key），没有身份时直接报错而不是返回空列表骗过前端。
func TestFindSourcesByGlobalIdSkipsDeletedRows(t *testing.T) {
	database := useUnionTestDB(t)
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

func unionCardByName(t *testing.T, cards []*UnionVideo, name string) *UnionVideo {
	t.Helper()
	for _, card := range cards {
		if card.VodName == name {
			return card
		}
	}
	t.Fatalf("合并视图里没有 %q，got %d 张卡片", name, len(cards))
	return nil
}

func useUnionTestDB(t *testing.T) *sqlx.DB {
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
