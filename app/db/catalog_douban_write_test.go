package db

import (
	"testing"

	"cczjVideo/app/model"
)

// 采集页里携带的豆瓣 ID/评分以前由采集后第二趟逐条另开事务写。折进页事务之后，
// 这几条测试盯住两件事：字段真的落库，以及空字段不能把已有值擦掉。

const dwSource = "doubanwrite_src"

type dwGlobalRow struct {
	TypeID      int64  `db:"type_id"`
	Year        string `db:"year"`
	Area        string `db:"area"`
	Lang        string `db:"lang"`
	Tag         string `db:"tag"`
	Pic         string `db:"pic"`
	Genre       string `db:"genre"`
	Aka         string `db:"aka"`
	ReleaseDate string `db:"release_date"`
	DoubanID    string `db:"douban_id"`
	DoubanScore string `db:"douban_score"`
}

func dwCleanup(t *testing.T, names ...string) {
	t.Helper()
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, dwSource); err != nil {
		t.Fatal(err)
	}
	placeholders := ""
	args := make([]any, 0, len(names))
	for i, n := range names {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args = append(args, normalizeTitle(n))
	}
	if len(names) > 0 {
		if _, err := DB().Exec(`DELETE FROM global_video WHERE name_norm IN (`+placeholders+`)`, args...); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := DB().Exec(`DELETE FROM global_video WHERE douban_id IN ('3678901','9990001','9990002','9990003','8800001','8800002')`); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM source_types WHERE source_key=?`, dwSource); err != nil {
		t.Fatal(err)
	}
}

func dwVideo(vodID, title string) *model.Video {
	return &model.Video{
		VodId:          model.FlexibleString(vodID),
		TypeId:         model.FlexibleString("dw-type"),
		TypeName:       "Douban write type",
		VodName:        title,
		VodYear:        "2024",
		VodArea:        "日本",
		VodLang:        "日语",
		VodTag:         "热血,运动",
		VodSub:         "别名甲",
		VodPic:         "https://img.example.com/a.jpg",
		VodTime:        "2026-01-01 00:00:00",
		VodDoubanId:    model.FlexibleString("3678901"),
		VodDoubanScore: model.FlexibleString("8.7"),
	}
}

func dwLoad(t *testing.T, title string) dwGlobalRow {
	t.Helper()
	var row dwGlobalRow
	err := DB().Get(&row, `SELECT type_id,year,area,lang,tag,pic,genre,aka,release_date,douban_id,douban_score
		FROM global_video WHERE name_norm=?`, normalizeTitle(title))
	if err != nil {
		t.Fatalf("读取 %s 的身份行失败: %v", title, err)
	}
	return row
}

func dwExpect(t *testing.T, field, got, want string) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %q，期望 %q", field, got, want)
	}
}

func TestCatalogUpsertWritesDoubanFieldsInPageTransaction(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const title = "豆瓣直挂甲"
	dwCleanup(t, title)
	defer dwCleanup(t, title)

	if err := UpsertCatalogItems(dwSource, []*model.Video{dwVideo("dw-1", title)}); err != nil {
		t.Fatal(err)
	}
	row := dwLoad(t, title)
	if row.TypeID == 0 {
		t.Fatal("type_id 没有解析出来")
	}
	dwExpect(t, "year", row.Year, "2024")
	dwExpect(t, "area", row.Area, "日本")
	dwExpect(t, "lang", row.Lang, "日语")
	dwExpect(t, "tag", row.Tag, "热血,运动")
	dwExpect(t, "pic", row.Pic, "https://img.example.com/a.jpg")
	dwExpect(t, "genre", row.Genre, "热血,运动")
	dwExpect(t, "aka", row.Aka, "别名甲")
	dwExpect(t, "release_date", row.ReleaseDate, "2024")
	dwExpect(t, "douban_id", row.DoubanID, "3678901")
	dwExpect(t, "douban_score", row.DoubanScore, "8.7")
}

// 第二页只带片名：源站列表字段经常缺，缺的不能把上一趟补好的值擦成空。
func TestCatalogUpsertEmptyFieldsKeepExistingValues(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const title = "豆瓣直挂乙"
	dwCleanup(t, title)
	defer dwCleanup(t, title)

	if err := UpsertCatalogItems(dwSource, []*model.Video{dwVideo("dw-1", title)}); err != nil {
		t.Fatal(err)
	}
	bare := &model.Video{
		VodId:    model.FlexibleString("dw-2"),
		TypeId:   model.FlexibleString("dw-type"),
		TypeName: "Douban write type",
		VodName:  title,
		VodTime:  "2026-01-02 00:00:00",
	}
	if err := UpsertCatalogItems(dwSource, []*model.Video{bare}); err != nil {
		t.Fatal(err)
	}
	row := dwLoad(t, title)
	dwExpect(t, "lang", row.Lang, "日语")
	dwExpect(t, "tag", row.Tag, "热血,运动")
	dwExpect(t, "pic", row.Pic, "https://img.example.com/a.jpg")
	dwExpect(t, "aka", row.Aka, "别名甲")
	dwExpect(t, "douban_id", row.DoubanID, "3678901")
	dwExpect(t, "douban_score", row.DoubanScore, "8.7")

	// 同名的两部片仍然只有一条身份，第二部没有另起一行。
	var count int
	if err := DB().Get(&count, `SELECT COUNT(*) FROM global_video WHERE name_norm=?`, normalizeTitle(title)); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("身份行 %d 条，应该是 1 条", count)
	}
}

// dwIDVideo 只改豆瓣两个字段，其余沿用 dwVideo，便于逐条构造源站声明。
func dwIDVideo(vodID, title, doubanID, score string) *model.Video {
	v := dwVideo(vodID, title)
	v.VodDoubanId = model.FlexibleString(doubanID)
	v.VodDoubanScore = model.FlexibleString(score)
	return v
}

// C2：源站自称的豆瓣 ID 只能填空，不能改写已有 ID。库里那条是过了匹配阈值
// 或人工修复留下的，覆盖它就等于用一个没验证过的 ID 换掉一个验证过的 ID。
func TestCatalogUpsertOnlyFillsDoubanID(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const title = "豆瓣ID不改写"
	dwCleanup(t, title)
	defer dwCleanup(t, title)

	for _, step := range []struct {
		vodID, doubanID, score string
		wantID, wantScore      string
	}{
		{vodID: "dw-1", wantID: "", wantScore: ""},                                                // 什么都不带：保持空
		{vodID: "dw-2", doubanID: "77710001", wantID: "77710001"},                                 // 评分先缺着
		{vodID: "dw-3", doubanID: "77710002", score: "6.6", wantID: "77710001", wantScore: ""},    // 别的 ID 的评分不算数
		{vodID: "dw-4", doubanID: "77710001", score: "8.7", wantID: "77710001", wantScore: "8.7"}, // 同 ID 才补洞
	} {
		if err := UpsertCatalogItems(dwSource, []*model.Video{dwIDVideo(step.vodID, title, step.doubanID, step.score)}); err != nil {
			t.Fatal(err)
		}
		row := dwLoad(t, title)
		dwExpect(t, "douban_id", row.DoubanID, step.wantID)
		dwExpect(t, "douban_score", row.DoubanScore, step.wantScore)
	}
}

func dwInsertGlobal(t *testing.T, title, doubanID, createdAt string) int64 {
	t.Helper()
	var id int64
	err := DB().Get(&id, `SELECT id FROM global_video WHERE name_norm=?`, normalizeTitle(title))
	if err == nil {
		if _, err := DB().Exec(`DELETE FROM global_video WHERE id=?`, id); err != nil {
			t.Fatal(err)
		}
	}
	res, err := DB().Exec(`INSERT INTO global_video(vod_name,name_norm,type_id,douban_id,created_at) VALUES(?,?,?,?,?)`,
		title, normalizeTitle(title), 0, doubanID, createdAt)
	if err != nil {
		t.Fatal(err)
	}
	newID, err := res.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return newID
}

func dwHotness(t *testing.T, id int64) string {
	t.Helper()
	var got string
	if err := DB().Get(&got, `SELECT douban_hotness FROM global_video WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	return got
}

// 同一豆瓣 ID 可能挂着多条本地身份（分季、跨年）。热度只写最早那条，
// 和 GetGlobalVideoByDoubanID 的主记录口径保持一致。
func TestUpdateDoubanHotnessBatchWritesEarliestRowOnly(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	dwCleanup(t)
	defer dwCleanup(t)

	late := dwInsertGlobal(t, "热度后建", "9990001", "2026-06-01 00:00:00")
	early := dwInsertGlobal(t, "热度先建", "9990001", "2025-01-01 00:00:00")
	zero := dwInsertGlobal(t, "热度零值", "9990002", "2025-01-01 00:00:00")

	updated, err := UpdateDoubanHotnessBatch(map[string]string{
		"9990001": "321",
		"9990002": "0",
		"9990003": "654",
		"":        "999",
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated != 1 {
		t.Fatalf("更新了 %d 行，只应写最早那一条", updated)
	}
	dwExpect(t, "最早行热度", dwHotness(t, early), "321")
	dwExpect(t, "后建行热度", dwHotness(t, late), "")
	dwExpect(t, "热度为 0 的行", dwHotness(t, zero), "")
}

func TestUpdateDoubanHotnessBatchNoopInputs(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	for name, input := range map[string]map[string]string{
		"nil map": nil,
		"空 map":   {},
		"全是被过滤项":  {"": "1", "9990002": "0", "9990001": ""},
	} {
		updated, err := UpdateDoubanHotnessBatch(input)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if updated != 0 {
			t.Fatalf("%s 返回 %d，应为 0", name, updated)
		}
	}
}

func TestImportSourceTypesUpsertsInOneTransaction(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	dwCleanup(t)
	defer dwCleanup(t)

	rows := []SourceTypeExport{
		{SourceTypeID: "dw-1", GlobalTypeID: 11, TypeName: "剧情"},
		{SourceTypeID: "dw-2", GlobalTypeID: 12, TypeName: "喜剧"},
	}
	if err := ImportSourceTypes(dwSource, rows); err != nil {
		t.Fatal(err)
	}
	if err := ImportSourceTypes(dwSource, []SourceTypeExport{
		{SourceTypeID: "dw-1", GlobalTypeID: 21, TypeName: "动作"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := ExportSourceTypes(dwSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("导入后 %d 条类型映射，重复主键应被覆盖而不是插入新行", len(got))
	}
	byID := map[string]SourceTypeExport{}
	for _, r := range got {
		byID[r.SourceTypeID] = r
	}
	dwExpect(t, "dw-1 名称", byID["dw-1"].TypeName, "动作")
	if byID["dw-1"].GlobalTypeID != 21 {
		t.Errorf("dw-1 global_type_id = %d，期望 21", byID["dw-1"].GlobalTypeID)
	}
	dwExpect(t, "dw-2 名称", byID["dw-2"].TypeName, "喜剧")

	// 空列表直接返回，不能把已导入的映射清掉。
	if err := ImportSourceTypes(dwSource, nil); err != nil {
		t.Fatal(err)
	}
	after, err := ExportSourceTypes(dwSource)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 2 {
		t.Fatalf("空导入把映射变成了 %d 条", len(after))
	}
}

// 热榜以前逐条 ResolveGlobalVideoID(nil)：一百条就是一百次全表载入 + 一百次自动提交，
// 而且批次内存不共享，同一部片的两条榜单各建一行身份。
func TestUpsertChartItemsSharesOneIdentityAcrossBatch(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const title = "热榜同片"
	dwCleanup(t, title)
	defer dwCleanup(t, title)

	created, updated, err := UpsertChartItems([]ChartDoubanUpdate{
		{SubjectID: "8800001", Title: title, Year: "2026", Area: "中国", ReleaseDate: "2026-05-01", Rating: "7.1", Votes: "1234", PosterURL: "https://poster/1.jpg"},
		{SubjectID: "8800002", Title: title, Year: "2026", Area: "中国", ReleaseDate: "2026-05-01", Rating: "7.2", Votes: "2345", PosterURL: "https://poster/2.jpg"},
		{SubjectID: "", Title: "热榜缺ID", Year: "2026"},
		{SubjectID: "8800003", Title: "  "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 1 || updated != 2 {
		t.Fatalf("新增 %d 更新 %d，应只新建 1 条身份并写 2 行", created, updated)
	}
	var rows int
	if err := DB().Get(&rows, `SELECT COUNT(*) FROM global_video WHERE name_norm=?`, normalizeTitle(title)); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("同一部片建了 %d 条身份", rows)
	}
	row := dwLoad(t, title)
	dwExpect(t, "douban_id 只填首个空缺", row.DoubanID, "8800001")
	dwExpect(t, "douban_score 按热榜刷新", row.DoubanScore, "7.2")
	dwExpect(t, "pic 只填空缺", row.Pic, "https://poster/1.jpg")
	dwExpect(t, "year", row.Year, "2026")
	dwExpect(t, "area", row.Area, "中国")
	dwExpect(t, "release_date", row.ReleaseDate, "2026-05-01")
}

// 热榜不能把采集/详情已经写好的字段擦掉或换掉：除评分票数外全是填空缺。
func TestUpsertChartItemsKeepsExistingCatalogFields(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const title = "热榜补洞"
	dwCleanup(t, title)
	defer dwCleanup(t, title)

	if err := UpsertCatalogItems(dwSource, []*model.Video{dwVideo("dw-1", title)}); err != nil {
		t.Fatal(err)
	}
	created, updated, err := UpsertChartItems([]ChartDoubanUpdate{
		{SubjectID: "8800001", Title: title, Year: "1999", Area: "美国", ReleaseDate: "1999-01-01", Rating: "9.9", Votes: "1", PosterURL: "https://poster/other.jpg"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if created != 0 || updated != 1 {
		t.Fatalf("新增 %d 更新 %d，应命中已有身份", created, updated)
	}
	row := dwLoad(t, title)
	dwExpect(t, "douban_id", row.DoubanID, "3678901")
	dwExpect(t, "pic", row.Pic, "https://img.example.com/a.jpg")
	dwExpect(t, "year", row.Year, "2024")
	dwExpect(t, "area", row.Area, "日本")
	dwExpect(t, "lang", row.Lang, "日语")
	dwExpect(t, "release_date", row.ReleaseDate, "2024")
	dwExpect(t, "douban_score", row.DoubanScore, "9.9")
}

func TestUpsertChartItemsEmptyInput(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	created, updated, err := UpsertChartItems(nil)
	if err != nil || created != 0 || updated != 0 {
		t.Fatalf("空输入返回 %d/%d/%v", created, updated, err)
	}
}
