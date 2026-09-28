package db

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// 跨源合并视图：一张卡片 = 一个 global_id，而不是一个源里的一条目录行。
//
// 单源列表（GetCatalogVideoPage）永远按 source_key 前缀走，所以同一部片在两个源里
// 就是两张互不可见的卡片；这里反过来以 global_id 去重，代表行取该身份里
// orderColumn 最新的那条。筛选条件在主查询、代表行判定和总数三处必须是同一份，
// 否则总数与翻页出来的卡片数不一致，游标还会跳页。

// UnionVideo 是合并列表里的一行。SourceKey/SourceVodID 指向代表行，
// 点进详情或播放就用这一对；Sources 是同一身份还在哪些源里有货。
type UnionVideo struct {
	ID           int64    `json:"id"`
	GlobalID     int64    `json:"global_id"`
	SourceKey    string   `json:"source_key"`
	SourceVodID  string   `json:"vod_id"`
	SourceTypeID string   `json:"type_id"`
	TypeName     string   `json:"type_name"`
	VodName      string   `json:"vod_name"`
	VodPic       string   `json:"vod_pic"`
	VodRemarks   string   `json:"vod_remarks"`
	VodYear      string   `json:"vod_year"`
	VodArea      string   `json:"vod_area"`
	VodTime      string   `json:"vod_time"`
	SourceCount  int      `json:"source_count"`
	Sources      []string `json:"sources"`
}

type UnionPage struct {
	Videos     []*UnionVideo
	Total      int
	NextCursor string
}

// unionFilterClause 把浏览筛选写成带别名的片段和对应参数。
// 类型筛选用 global_type_id：source_type_id 是源内编号，跨源毫无意义。
// global_id>0 是硬前提——没有身份的目录行无从判断是不是同一部片。
func unionFilterClause(alias string, filter FilterParams) (string, []any) {
	col := func(name string) string { return alias + "." + name }
	parts := []string{
		col("lifecycle_state") + "='active'",
		catalogTypeVisibilityClause(alias),
		col("global_id") + ">0",
	}
	args := make([]any, 0, 6)
	if typeID := strings.TrimSpace(filter.TypeId); typeID != "" && typeID != "all" {
		// global_type_id 是整数列，绑文本永远不相等，必须先转。
		if id, err := strconv.ParseInt(typeID, 10, 64); err == nil {
			parts = append(parts, col("global_type_id")+"=?")
			args = append(args, id)
		}
	}
	if year := strings.TrimSpace(filter.Year); year != "" && year != "all" {
		parts = append(parts, col("vod_year")+"=?")
		args = append(args, year)
	}
	if area := strings.TrimSpace(filter.Area); area != "" && area != "all" {
		parts = append(parts, col("vod_area")+"=?")
		args = append(args, area)
	}
	if keyword := strings.TrimSpace(filter.Keyword); keyword != "" {
		like := "%" + keyword + "%"
		parts = append(parts, "("+col("vod_name")+" LIKE ? OR "+col("vod_remarks")+" LIKE ? OR "+col("type_name")+" LIKE ?)")
		args = append(args, like, like, like)
	}
	if filter.RecentDays == 1 || filter.RecentDays == 7 || filter.RecentDays == 30 {
		parts = append(parts, col("updated_at")+">= datetime('now', ?)")
		args = append(args, fmt.Sprintf("-%d days", filter.RecentDays))
	}
	return strings.Join(parts, " AND "), args
}

type unionRow struct {
	ID           int64  `db:"id"`
	GlobalID     int64  `db:"global_id"`
	SourceKey    string `db:"source_key"`
	SourceVodID  string `db:"source_vod_id"`
	SourceTypeID string `db:"source_type_id"`
	TypeName     string `db:"type_name"`
	VodName      string `db:"vod_name"`
	VodPic       string `db:"vod_pic"`
	VodRemarks   string `db:"vod_remarks"`
	VodYear      string `db:"vod_year"`
	VodArea      string `db:"vod_area"`
	VodTime      string `db:"vod_time"`
	SourceCount  int    `db:"source_count"`
	SourceKeys   string `db:"source_keys"`
	CursorValue  string `db:"cursor_value"`
}

// sources 拆开 group_concat 的结果。DISTINCT 拼接不保证顺序，也不保证代表源
// 一定在里面排第一，所以排序后交给界面，卡片上的源名单才是稳定的。
func (r unionRow) sources() []string {
	seen := make(map[string]bool, 4)
	out := make([]string, 0, 4)
	for _, key := range strings.Split(r.SourceKeys, ",") {
		key = strings.TrimSpace(key)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, key)
	}
	if len(out) == 0 && r.SourceKey != "" {
		out = append(out, r.SourceKey)
	}
	sort.Strings(out)
	return out
}

// GetCatalogUnionPage 返回跨源合并后的曲库一页。
// 语义与 GetCatalogVideoPage 一致（同一套筛选、同样的游标），差别只在「一行 = 一个 global_id」。
func GetCatalogUnionPage(filter FilterParams) (*UnionPage, error) {
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	orderColumn := "vod_time"
	if filter.RecentDays > 0 {
		orderColumn = "updated_at"
	}

	mainClause, mainArgs := unionFilterClause("sv", filter)
	// 代表行要在同样的筛选里挑：源 A 的条目命中关键词时，不能让源 B 那条
	// 不命中的兄弟冒充代表行，也不能因为它更新就把这张卡片挤掉。
	probeClause, probeArgs := unionFilterClause("s4", filter)

	var total int
	if err := instance.QueryRow("SELECT COUNT(DISTINCT sv.global_id) FROM source_videos sv WHERE "+mainClause, mainArgs...).Scan(&total); err != nil {
		return nil, err
	}

	// 代表行 = 同一身份里 (orderColumn, id) 最大的一条。用 NOT EXISTS 而不是窗口函数，
	// 外层才能顺着时间索引倒着走、在 LIMIT 处停下，不必把整库排序进临时 B 树。
	// 源数统计不带筛选条件：卡片上的「3 个源」说的是一部片有几个来源，
	// 跟用户当下筛了什么无关。
	query := `SELECT sv.id,sv.global_id,sv.source_key,sv.source_vod_id,sv.source_type_id,sv.type_name,
		sv.vod_name,sv.vod_pic,sv.vod_remarks,sv.vod_year,sv.vod_area,sv.vod_time,
		(SELECT COUNT(DISTINCT s2.source_key) FROM source_videos s2 WHERE s2.global_id=sv.global_id AND s2.lifecycle_state='active' AND ` + catalogTypeVisibilityClause("s2") + `) AS source_count,
		(SELECT GROUP_CONCAT(DISTINCT s3.source_key) FROM source_videos s3 WHERE s3.global_id=sv.global_id AND s3.lifecycle_state='active' AND ` + catalogTypeVisibilityClause("s3") + `) AS source_keys,
		sv.` + orderColumn + ` AS cursor_value
		FROM source_videos sv WHERE ` + mainClause + `
		AND NOT EXISTS (SELECT 1 FROM source_videos s4 WHERE s4.global_id=sv.global_id AND ` + probeClause + `
			AND (s4.` + orderColumn + ` > sv.` + orderColumn + ` OR (s4.` + orderColumn + ` = sv.` + orderColumn + ` AND s4.id > sv.id)))`
	args := make([]any, 0, len(mainArgs)+len(probeArgs)+4)
	args = append(args, mainArgs...)
	args = append(args, probeArgs...)
	if filter.Cursor != "" {
		cursorValue, cursorID, err := decodeCatalogCursor(filter.Cursor)
		if err != nil {
			return nil, fmt.Errorf("invalid catalog cursor: %w", err)
		}
		query += " AND (sv." + orderColumn + " < ? OR (sv." + orderColumn + " = ? AND sv.id < ?))"
		args = append(args, cursorValue, cursorValue, cursorID)
	}
	query += " ORDER BY sv." + orderColumn + " DESC, sv.id DESC LIMIT ?"
	args = append(args, filter.PageSize+1)

	var rows []unionRow
	if err := instance.Select(&rows, query, args...); err != nil {
		return nil, err
	}
	hasMore := len(rows) > filter.PageSize
	if hasMore {
		rows = rows[:filter.PageSize]
	}
	out := make([]*UnionVideo, 0, len(rows))
	for _, r := range rows {
		count := r.SourceCount
		if count < 1 {
			count = 1
		}
		out = append(out, &UnionVideo{
			ID: r.ID, GlobalID: r.GlobalID, SourceKey: r.SourceKey, SourceVodID: r.SourceVodID,
			SourceTypeID: r.SourceTypeID, TypeName: r.TypeName, VodName: r.VodName, VodPic: r.VodPic,
			VodRemarks: r.VodRemarks, VodYear: r.VodYear, VodArea: r.VodArea, VodTime: r.VodTime,
			SourceCount: count, Sources: r.sources(),
		})
	}
	nextCursor := ""
	if hasMore && len(rows) > 0 {
		last := rows[len(rows)-1]
		nextCursor = encodeCatalogCursor(last.CursorValue, int(last.ID))
	}
	return &UnionPage{Videos: out, Total: total, NextCursor: nextCursor}, nil
}

// GetUnionYearsAndAreas 汇总所有源里的年份/地区选项。
// 逐源取而不是对整张表 DISTINCT：单源那条走 idx_sv_key_year/idx_sv_key_area，
// 跨源一条要把整库读进临时 B 树。
func GetUnionYearsAndAreas() ([]string, []string, error) {
	sources, err := GetAllSources()
	if err != nil {
		return nil, nil, err
	}
	yearSet := make(map[string]bool)
	areaSet := make(map[string]bool)
	for _, source := range sources {
		if source == nil || strings.TrimSpace(source.SourceKey) == "" {
			continue
		}
		years, areas, err := GetCatalogYearsAndAreas(source.SourceKey)
		if err != nil {
			return nil, nil, err
		}
		for _, year := range years {
			yearSet[year] = true
		}
		for _, area := range areas {
			areaSet[area] = true
		}
	}
	years := make([]string, 0, len(yearSet))
	for year := range yearSet {
		years = append(years, year)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(years)))
	areas := make([]string, 0, len(areaSet))
	for area := range areaSet {
		areas = append(areas, area)
	}
	sort.Strings(areas)
	return years, areas, nil
}
