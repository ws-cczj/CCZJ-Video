package db

import (
	"errors"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// 人工确认的身份合并队列。
//
// 迁移 v2 只会自动并掉 name_norm + type_id 完全相同的行；剩下两类它不敢动：
// 同一个豆瓣条目被拆成两条（通常因为标题里多了年份、清晰度或源站后缀），
// 以及同名但挂在不同类型下的同一部剧。这些都需要人看一眼再决定，
// 所以这里只列出候选、并由界面点名单条执行，绝不在启动时批量自动合并。

// MergeCandidateRow 候选组里的一条 global_video。
type MergeCandidateRow struct {
	GlobalID     int64  `json:"global_id"`
	VodName      string `json:"vod_name"`
	Year         string `json:"year"`
	TypeName     string `json:"type_name"`
	DoubanID     string `json:"douban_id"`
	DoubanScore  string `json:"douban_score"`
	HasPic       bool   `json:"has_pic"`
	CatalogRows  int    `json:"catalog_rows"`
	FavoriteRows int    `json:"favorite_rows"`
	HistoryRows  int    `json:"history_rows"`
}

// MergeCandidate 一组疑似同一片的身份。
type MergeCandidate struct {
	Reason string              `json:"reason"`
	Key    string              `json:"key"`
	Rows   []MergeCandidateRow `json:"rows"`
}

const (
	mergeReasonDoubanID = "douban_id"
	mergeReasonSameName = "same_name"
)

type mergeCandidateRow struct {
	GlobalID     int64  `db:"global_id"`
	DoubanID     string `db:"douban_id"`
	NameNorm     string `db:"name_norm"`
	TypeID       int64  `db:"type_id"`
	VodName      string `db:"vod_name"`
	Year         string `db:"year"`
	TypeName     string `db:"type_name"`
	DoubanScore  string `db:"douban_score"`
	Pic          string `db:"pic"`
	CatalogRows  int    `db:"catalog_rows"`
	FavoriteRows int    `db:"favorite_rows"`
	HistoryRows  int    `db:"history_rows"`
}

// ListIdentityMergeCandidates 列出疑似重复的身份组，最多扫描 limit 行。
//
// 排序保证同一组的行相邻：先按归一化标题、再按豆瓣 ID。每组只有两三条，
// 界面按组渲染，一次合并一组，不做批量勾选。
func ListIdentityMergeCandidates(limit int) ([]MergeCandidate, error) {
	if instance == nil {
		return nil, errors.New("database not initialized")
	}
	if limit <= 0 || limit > 2000 {
		limit = 600
	}
	var rows []mergeCandidateRow
	err := instance.Select(&rows, `
		SELECT g.id AS global_id, COALESCE(g.douban_id,'') AS douban_id, COALESCE(g.name_norm,'') AS name_norm,
			COALESCE(g.type_id,0) AS type_id, COALESCE(g.vod_name,'') AS vod_name, COALESCE(g.year,'') AS year,
			COALESCE(gt.type_name,'') AS type_name, COALESCE(g.douban_score,'') AS douban_score, COALESCE(g.pic,'') AS pic,
			(SELECT COUNT(*) FROM source_videos sv WHERE sv.global_id=g.id AND sv.lifecycle_state='active') AS catalog_rows,
			(SELECT COUNT(*) FROM favorites f WHERE f.global_id=g.id) AS favorite_rows,
			(SELECT COUNT(*) FROM watch_history h WHERE h.global_id=g.id) AS history_rows
		FROM global_video g
		LEFT JOIN global_types gt ON gt.id = g.type_id
		WHERE (COALESCE(TRIM(g.douban_id),'') <> '' AND g.douban_id IN (
				SELECT douban_id FROM global_video WHERE TRIM(COALESCE(douban_id,'')) <> '' GROUP BY douban_id HAVING COUNT(*) > 1))
			OR (g.name_norm <> '' AND EXISTS (SELECT 1 FROM global_video d
				WHERE d.name_norm = g.name_norm AND d.type_id <> g.type_id))
		ORDER BY g.name_norm, g.douban_id, g.id
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list identity merge candidates: %w", err)
	}
	return groupMergeCandidates(rows), nil
}

// groupMergeCandidates 把相邻行收成组：优先按豆瓣 ID，没有豆瓣 ID 才按归一化标题。
//
// 只有一条行的组不算候选。同名不同类的组还要再过一道年份一致性：
// 「大白鲨 1975 / 电影」和「大白鲨 2023 / 纪录片」是两部片，
// 年份都对不上就绝不该出现在合并候选里。
func groupMergeCandidates(rows []mergeCandidateRow) []MergeCandidate {
	type key struct {
		reason string
		value  string
	}
	order := make([]key, 0, len(rows))
	groups := make(map[key][]MergeCandidateRow, 32)
	years := make(map[key]map[string]bool, 32)
	for _, row := range rows {
		item := MergeCandidateRow{
			GlobalID: row.GlobalID, VodName: row.VodName, Year: row.Year, TypeName: row.TypeName,
			DoubanID: row.DoubanID, DoubanScore: row.DoubanScore, HasPic: strings.TrimSpace(row.Pic) != "",
			CatalogRows: row.CatalogRows, FavoriteRows: row.FavoriteRows, HistoryRows: row.HistoryRows,
		}
		k := key{mergeReasonSameName, row.NameNorm}
		if trimmed := strings.TrimSpace(row.DoubanID); trimmed != "" {
			k = key{mergeReasonDoubanID, trimmed}
		}
		if _, seen := groups[k]; !seen {
			order = append(order, k)
			years[k] = make(map[string]bool, 2)
		}
		groups[k] = append(groups[k], item)
		if year := strings.TrimSpace(row.Year); year != "" {
			years[k][year] = true
		}
	}
	out := make([]MergeCandidate, 0, len(order))
	for _, k := range order {
		members := groups[k]
		if len(members) < 2 {
			continue
		}
		if k.reason == mergeReasonSameName && len(years[k]) > 1 {
			continue
		}
		out = append(out, MergeCandidate{Reason: k.reason, Key: k.value, Rows: members})
	}
	return out
}

// MergeGlobalVideoIdentities 把用户确认的几条身份并成一条，返回存活 id 与被并掉的条数。
//
// 合并会改指目录投影、收藏和观看进度，任何一步失败整体回滚。
// 只有"确属同一片"的组才允许并：豆瓣 ID 完全一致，或归一化标题完全一致。
// 这一条是给界面兜底的——传进来的 id 列表来自前端，不能什么组合都接受。
func MergeGlobalVideoIdentities(globalIDs []int64) (int64, int, error) {
	if instance == nil {
		return 0, 0, errors.New("database not initialized")
	}
	ids, err := normalizeMergeIDs(globalIDs)
	if err != nil {
		return 0, 0, err
	}
	tx, err := instance.Beginx()
	if err != nil {
		return 0, 0, fmt.Errorf("begin identity merge: %w", err)
	}
	defer tx.Rollback()

	rows, err := loadDupGroupByIDs(tx, ids)
	if err != nil {
		return 0, 0, err
	}
	if len(rows) != len(ids) {
		return 0, 0, fmt.Errorf("有 %d 条身份已经不存在，请刷新后重试", len(ids)-len(rows))
	}
	if !mergeableGroup(rows) {
		return 0, 0, errors.New("这几条既不是同一个豆瓣条目，也不是同一个归一化标题，不能合并")
	}
	merged, err := mergeDupGroup(tx, rows)
	if err != nil {
		return 0, 0, err
	}
	keep := rows[pickMergeSurvivor(rows)].id
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit identity merge: %w", err)
	}
	return keep, merged, nil
}

// IdentitySourceKeys 返回这些身份在目录里用到的源，供缓存失效层决定该清哪些源。
// 必须在合并之前查：身份一旦被并掉，被并那条用过哪些源就再也查不回来了。
func IdentitySourceKeys(globalIDs []int64) ([]string, error) {
	if instance == nil {
		return nil, errors.New("database not initialized")
	}
	ids := make([]any, 0, len(globalIDs))
	marks := make([]string, 0, len(globalIDs))
	for _, id := range globalIDs {
		if id <= 0 {
			continue
		}
		marks = append(marks, "?")
		ids = append(ids, id)
	}
	if len(marks) == 0 {
		return nil, nil
	}
	var keys []string
	query := `SELECT DISTINCT source_key FROM source_videos WHERE global_id IN (` + strings.Join(marks, ",") + `)`
	if err := instance.Select(&keys, query, ids...); err != nil {
		return nil, fmt.Errorf("read sources of merged identities: %w", err)
	}
	return keys, nil
}

// mergeableGroup 判断这组身份是否可比：所有非空豆瓣 ID 必须一致；
// 一致不了时，归一化标题必须全部相同。
func mergeableGroup(rows []dupGlobalVideoRow) bool {
	doubanIdx := mergeColumnIndex("douban_id")
	norms := make(map[string]bool, len(rows))
	doubans := make(map[string]bool, len(rows))
	for i := range rows {
		norms[normalizeTitle(rows[i].vodName)] = true
		if doubanIdx >= 0 {
			if value := strings.TrimSpace(rows[i].Values[doubanIdx]); value != "" {
				doubans[value] = true
			}
		}
	}
	if len(doubans) == 1 {
		return true
	}
	return len(doubans) == 0 && len(norms) == 1
}

// loadDupGroupByIDs 按显式 id 列表读出一组待合并记录，列顺序与 loadDupGroup 相同。
func loadDupGroupByIDs(tx *sqlx.Tx, ids []int64) ([]dupGlobalVideoRow, error) {
	marks := make([]string, 0, len(ids))
	args := make([]any, 0, len(ids))
	for _, id := range ids {
		marks = append(marks, "?")
		args = append(args, id)
	}
	query := `SELECT id, COALESCE(vod_name, ''), ` + mergeColumnList() + `,
		COALESCE(` + mergeFailuresColumn + `, 0), COALESCE(` + mergeCooldownColumn + `, '')
		FROM global_video WHERE id IN (` + strings.Join(marks, ",") + `) ORDER BY id ASC`
	rows, err := tx.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("read merge group: %w", err)
	}
	defer rows.Close()

	result := make([]dupGlobalVideoRow, 0, len(ids))
	for rows.Next() {
		var kind dupGlobalVideoRow
		var name string
		values := make([]*string, len(normMergeTextColumns))
		scanArgs := make([]any, 0, 4+len(values))
		scanArgs = append(scanArgs, &kind.id, &name)
		for i := range values {
			values[i] = new(string)
			scanArgs = append(scanArgs, values[i])
		}
		scanArgs = append(scanArgs, &kind.failures, &kind.cooldown)
		if err := rows.Scan(scanArgs...); err != nil {
			return nil, fmt.Errorf("scan merge row: %w", err)
		}
		kind.vodName = name
		kind.Values = make([]string, len(values))
		for i, pointer := range values {
			kind.Values[i] = *pointer
		}
		result = append(result, kind)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read merge group: %w", err)
	}
	return result, nil
}

func normalizeMergeIDs(globalIDs []int64) ([]int64, error) {
	seen := make(map[int64]bool, len(globalIDs))
	out := make([]int64, 0, len(globalIDs))
	for _, id := range globalIDs {
		if id <= 0 {
			return nil, fmt.Errorf("非法的 global_id: %d", id)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) < 2 {
		return nil, errors.New("至少需要两个不同的身份才能合并")
	}
	if len(out) > 8 {
		return nil, fmt.Errorf("一次最多合并 8 条身份，当前 %d 条", len(out))
	}
	return out, nil
}
