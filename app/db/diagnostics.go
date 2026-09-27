package db

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// 诊断页的只读查询。这里只做 SELECT：任何会写库的体检手段（VACUUM、
// 字段继承、修复 ID）都不放进来，避免"看一眼就改了数据"。

// TableStat 单张用户表的行数。
type TableStat struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// DoubanHealth 豆瓣数据完整性快照。
type DoubanHealth struct {
	TotalVideos       int64 `json:"total_videos"`
	WithDoubanID      int64 `json:"with_douban_id"`
	MissingScore      int64 `json:"missing_score"`
	MissingSubjectID  int64 `json:"missing_subject_id"`
	OnCooldown        int64 `json:"on_cooldown"`
	DuplicateGroups   int64 `json:"duplicate_groups"`
	DuplicateRows     int64 `json:"duplicate_rows"`
	InheritCandidates int64 `json:"inherit_candidates"`
}

// DoubanDuplicateGroup 一个被拆成多条的豆瓣 ID。
type DoubanDuplicateGroup struct {
	DoubanID string `db:"douban_id" json:"douban_id"`
	Rows     int    `db:"rows" json:"rows"`
	Names    string `db:"names" json:"names"`
	Missing  int    `db:"missing" json:"missing"`
}

// TableStats 返回所有用户表的行数。表名来自 sqlite_master 且做了标识符校验，
// 不能参数绑定的地方只拼这个白名单里的名字。
func TableStats() ([]TableStat, error) {
	if instance == nil {
		return nil, errors.New("database not initialized")
	}
	var names []string
	if err := instance.Select(&names, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`); err != nil {
		return nil, err
	}
	out := make([]TableStat, 0, len(names))
	for _, name := range names {
		if !isSQLiteIdentifier(name) {
			continue
		}
		var rows int64
		if err := instance.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM "%s"`, name)).Scan(&rows); err != nil {
			return nil, fmt.Errorf("统计 %s 失败: %w", name, err)
		}
		out = append(out, TableStat{Name: name, Rows: rows})
	}
	return out, nil
}

func isSQLiteIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		alpha := r == '_' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !alpha || (i == 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

// GetDoubanHealth 汇总 global_video 里豆瓣数据的完整度。
//
// MissingScore 里包含两类：确实还没抓到的，以及被同 ID 兄弟"已有数据"挡掉而
// 永远轮不到的饿死记录；后者用 InheritCandidates 单独列出，能直接看出
// InheritDoubanFieldsFromSiblings 还能救多少条。
func GetDoubanHealth() (DoubanHealth, error) {
	var h DoubanHealth
	if instance == nil {
		return h, errors.New("database not initialized")
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	scans := []struct {
		query string
		args  []any
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM global_video`, nil, &h.TotalVideos},
		{`SELECT COUNT(*) FROM global_video WHERE TRIM(COALESCE(douban_id, '')) <> ''`, nil, &h.WithDoubanID},
		{`SELECT COUNT(*) FROM global_video WHERE TRIM(COALESCE(douban_id, '')) <> '' AND TRIM(COALESCE(douban_score, '')) = ''`, nil, &h.MissingScore},
		{`SELECT COUNT(*) FROM global_video WHERE TRIM(COALESCE(douban_id, '')) = ''`, nil, &h.MissingSubjectID},
		{`SELECT COUNT(*) FROM global_video WHERE douban_cooldown_until IS NOT NULL AND douban_cooldown_until > ?`, []any{now}, &h.OnCooldown},
		{`SELECT COUNT(*) FROM (SELECT douban_id FROM global_video WHERE TRIM(COALESCE(douban_id, '')) <> '' GROUP BY douban_id HAVING COUNT(*) > 1)`, nil, &h.DuplicateGroups},
		{`SELECT COUNT(*) FROM global_video WHERE TRIM(COALESCE(douban_id, '')) <> '' AND douban_id IN (SELECT douban_id FROM global_video WHERE TRIM(COALESCE(douban_id, '')) <> '' GROUP BY douban_id HAVING COUNT(*) > 1)`, nil, &h.DuplicateRows},
		{`SELECT COUNT(*) FROM global_video g WHERE TRIM(COALESCE(g.douban_id, '')) <> '' AND TRIM(COALESCE(g.douban_score, '')) = '' AND EXISTS (SELECT 1 FROM global_video s WHERE s.douban_id = g.douban_id AND TRIM(COALESCE(s.douban_score, '')) <> '')`, nil, &h.InheritCandidates},
	}
	for _, q := range scans {
		if err := instance.QueryRow(q.query, q.args...).Scan(q.dest); err != nil {
			return h, err
		}
	}
	return h, nil
}

// ListDoubanDuplicateGroups 列出被拆成多条的豆瓣 ID，最多 limit 组。
func ListDoubanDuplicateGroups(limit int) ([]DoubanDuplicateGroup, error) {
	if instance == nil {
		return nil, errors.New("database not initialized")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var rows []DoubanDuplicateGroup
	err := instance.Select(&rows, `
		SELECT g.douban_id AS douban_id,
		       COUNT(*)    AS rows,
		       GROUP_CONCAT(g.vod_name, ' / ') AS names,
		       SUM(CASE WHEN TRIM(COALESCE(g.douban_score, '')) = '' THEN 1 ELSE 0 END) AS missing
		FROM global_video g
		WHERE TRIM(COALESCE(g.douban_id, '')) <> ''
		GROUP BY g.douban_id
		HAVING COUNT(*) > 1
		ORDER BY missing DESC, g.douban_id
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	for i := range rows {
		rows[i].Names = strings.TrimSpace(rows[i].Names)
	}
	return rows, nil
}
