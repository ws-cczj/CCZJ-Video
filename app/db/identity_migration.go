package db

import (
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
)

// 身份归一化落地的两条迁移：先补 name_norm 列并用唯一一份 Go 实现回填，
// 再把历史上因为 SQL 表达式和 Go 实现漂移而重复的 global_video 行合并掉。
// 分两条是因为合并是有损风险的写操作，必须能在备份里单独回放。

// migrateGlobalVideoNameNorm 补列并按当前归一化实现回填全部 name_norm。
//
// 顺手补齐 global_video 的其它列：过去的升级路线是"删库重建"，停在旧 schema 上
// 的用户库可能缺 year/douban_id 这些应用一直在读的列，显式列名的 SELECT 会直接
// 报 no such column。迁移面向不存在的表时整体跳过，空库交给建表语句。
//
// 回填前必须先删掉归一化索引：库里躺着正是要处理的重复行，索引会当场把 UPDATE
// 挡成 UNIQUE constraint failed，迁移因此永远过不去。索引由迁移结束后
// runMigrations 里的 createIndexes 用新的列语义重建。
func migrateGlobalVideoNameNorm(tx *sqlx.Tx) error {
	exists, err := tableExists(tx, "global_video")
	if err != nil || !exists {
		return err
	}
	// 旧版索引建在 SQL 归一化表达式上，和 name_norm 的语义不同（不认全角感叹号、
	// 问号、零宽空格），同名索引会挡住新索引的建立，必须在这里删干净。
	for _, legacy := range []string{"idx_gv_name_type_norm", "idx_gv_name_norm"} {
		if _, err := tx.Exec("DROP INDEX IF EXISTS " + quoteIdentifier(legacy)); err != nil {
			return fmt.Errorf("drop legacy index %s: %w", legacy, err)
		}
	}
	for _, column := range normMergeTextColumns {
		if err := addColumnIfMissing(tx, "global_video", column, "TEXT DEFAULT ''"); err != nil {
			return err
		}
	}
	if err := addColumnIfMissing(tx, "global_video", mergeFailuresColumn, "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "global_video", mergeCooldownColumn, "DATETIME DEFAULT NULL"); err != nil {
		return err
	}
	if err := addColumnIfMissing(tx, "global_video", "name_norm", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	updated, err := backfillNameNorm(tx)
	if err != nil {
		return err
	}
	logInfo(fmt.Sprintf("已回填 %d 条 global_video 的 name_norm", updated))
	return nil
}

// normMergeTextColumns 是合并重复行时可以互相补齐的文本列。
// 列名写死在这里，不接受任何外部输入。
var normMergeTextColumns = []string{
	"year", "area", "lang", "writer", "tag", "pic",
	"douban_id", "douban_score", "douban_votes", "douban_hotness",
	"genre", "release_date", "duration", "aka", "imdb",
	"season_count", "episode_count",
}

// 失败计数与冷却时间单列处理：它们不是可补齐的元数据，而是"别再去撞豆瓣"的状态，
// 合并时取更大的计数和更晚的冷却，绝不能因为合并把仍在冷却的记录放回热路径。
const (
	mergeFailuresColumn = "douban_search_failures"
	mergeCooldownColumn = "douban_cooldown_until"
)

func mergeColumnIndex(column string) int {
	for i, candidate := range normMergeTextColumns {
		if candidate == column {
			return i
		}
	}
	return -1
}

type globalVideoDupGroup struct {
	NameNorm string `db:"name_norm"`
	TypeID   int64  `db:"type_id"`
}

// dupGlobalVideoRow 是待合并组里的一条记录，Values 与 normMergeTextColumns 对齐。
type dupGlobalVideoRow struct {
	id       int64
	vodName  string
	Values   []string
	failures int
	cooldown string
}

// migrateDedupeGlobalVideo 把归一化后同名的多条记录并成一条，并把收藏、
// 历史、目录引用一起改指向存活行。
//
// 存活行优先选已有豆瓣 ID 的那条（它挂着补全成果），其次选非空字段最多的，
// 最后按最小 id；被并掉行的空字段会补进存活行，观看进度逐集取更大的一侧。
// 整个过程在迁移事务里执行，任何一步失败都整体回滚。
func migrateDedupeGlobalVideo(tx *sqlx.Tx) error {
	exists, err := tableExists(tx, "global_video")
	if err != nil || !exists {
		return err
	}
	// 空标题的行不参与合并：它们没有可比的身份，并起来只会凭空造出一条无名字的记录。
	var groups []globalVideoDupGroup
	err = tx.Select(&groups, `SELECT name_norm, type_id FROM global_video
		WHERE name_norm <> '' GROUP BY name_norm, type_id HAVING COUNT(*) > 1 ORDER BY name_norm, type_id`)
	if err != nil {
		return fmt.Errorf("find duplicate global video groups: %w", err)
	}
	merged := 0
	for _, group := range groups {
		rows, err := loadDupGroup(tx, group.NameNorm, group.TypeID)
		if err != nil {
			return err
		}
		if len(rows) < 2 {
			continue
		}
		count, err := mergeDupGroup(tx, rows)
		if err != nil {
			return err
		}
		merged += count
	}
	if merged > 0 {
		logInfo(fmt.Sprintf("合并了 %d 条重复的 global_video 记录", merged))
	}
	// 这里不建索引：v2 已经把旧的函数索引删掉，(name_norm, type_id) 上的
	// 新唯一索引由迁移结束后 runMigrations 里的 createIndexes 在同一批里建回来。
	return nil
}

func loadDupGroup(tx *sqlx.Tx, nameNorm string, typeID int64) ([]dupGlobalVideoRow, error) {
	query := `SELECT id, COALESCE(vod_name, ''), ` + mergeColumnList() + `,
		COALESCE(` + mergeFailuresColumn + `, 0), COALESCE(` + mergeCooldownColumn + `, '')
		FROM global_video WHERE name_norm=? AND type_id=? ORDER BY id ASC`
	rows, err := tx.Query(query, nameNorm, typeID)
	if err != nil {
		return nil, fmt.Errorf("read duplicate group %q/%d: %w", nameNorm, typeID, err)
	}
	defer rows.Close()

	result := make([]dupGlobalVideoRow, 0, 4)
	for rows.Next() {
		var (
			kind   dupGlobalVideoRow
			name   string
			values = make([]*string, len(normMergeTextColumns))
			rest   = make([]any, 0, 4+len(values))
		)
		rest = append(rest, &kind.id, &name)
		for i := range values {
			values[i] = new(string)
			rest = append(rest, values[i])
		}
		rest = append(rest, &kind.failures, &kind.cooldown)
		if err := rows.Scan(rest...); err != nil {
			return nil, fmt.Errorf("read duplicate row of %q/%d: %w", nameNorm, typeID, err)
		}
		kind.vodName = name
		kind.Values = make([]string, len(values))
		for i, pointer := range values {
			kind.Values[i] = *pointer
		}
		result = append(result, kind)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scan duplicate group %q/%d: %w", nameNorm, typeID, err)
	}
	return result, nil
}

func mergeColumnList() string {
	list := make([]string, 0, len(normMergeTextColumns))
	for _, column := range normMergeTextColumns {
		list = append(list, fmt.Sprintf("COALESCE(%s, '')", column))
	}
	return strings.Join(list, ", ")
}

// pickMergeSurvivor 返回存活行下标：有豆瓣 ID 的优先，其次非空字段最多的，
// 平局保留 id 最小的一条（行已按 id 升序载入，先出现者胜出）。
func pickMergeSurvivor(rows []dupGlobalVideoRow) int {
	best, bestScore := 0, -1
	doubanIdx := mergeColumnIndex("douban_id")
	for i := range rows {
		score := 0
		if doubanIdx >= 0 && rows[i].Values[doubanIdx] != "" {
			score += 100
		}
		for _, value := range rows[i].Values {
			if value != "" {
				score++
			}
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	return best
}

// mergeDupGroup 补齐存活行、改指三张引用表、删掉被并掉的行，返回删除条数。
func mergeDupGroup(tx *sqlx.Tx, rows []dupGlobalVideoRow) (int, error) {
	keep := &rows[pickMergeSurvivor(rows)]
	changed := make(map[int]string, len(normMergeTextColumns))
	victims := make([]*dupGlobalVideoRow, 0, len(rows)-1)
	for i := range rows {
		row := &rows[i]
		if row == keep {
			continue
		}
		victims = append(victims, row)
		for idx, value := range row.Values {
			if value == "" {
				continue
			}
			if keep.Values[idx] != "" {
				continue
			}
			if _, already := changed[idx]; already {
				continue
			}
			changed[idx] = value
			keep.Values[idx] = value
		}
		if row.failures > keep.failures {
			keep.failures = row.failures
		}
		// DATETIME 以 'YYYY-MM-DD HH:MM:SS' 存储，字符串比较即时间比较。
		if row.cooldown > keep.cooldown {
			keep.cooldown = row.cooldown
		}
	}
	if err := writeMergedValues(tx, keep, changed); err != nil {
		return 0, err
	}
	for _, victim := range victims {
		if err := repointCatalogReferences(tx, keep.id, victim.id); err != nil {
			return 0, err
		}
		if err := mergeFavorites(tx, keep.id, victim.id); err != nil {
			return 0, err
		}
		if err := mergeWatchHistory(tx, keep.id, victim.id); err != nil {
			return 0, err
		}
		if _, err := tx.Exec(`DELETE FROM global_video WHERE id=?`, victim.id); err != nil {
			return 0, fmt.Errorf("delete duplicate global_video %d: %w", victim.id, err)
		}
	}
	return len(victims), nil
}

// writeMergedValues 把补齐后的字段与合并后的重试状态写回存活行。列名全部来自
// 代码里写死的常量，值全部走绑定参数。
func writeMergedValues(tx *sqlx.Tx, keep *dupGlobalVideoRow, changed map[int]string) error {
	set := make([]string, 0, len(normMergeTextColumns)+2)
	args := make([]any, 0, len(normMergeTextColumns)+3)
	for idx, column := range normMergeTextColumns {
		value, ok := changed[idx]
		if !ok {
			continue
		}
		set = append(set, column+"=?")
		args = append(args, value)
	}
	// 重试状态无条件参与：只增加、不减少，避免合并后立刻又去撞豆瓣。
	set = append(set, mergeFailuresColumn+"=?", mergeCooldownColumn+"=?")
	args = append(args, keep.failures, keep.cooldown)
	args = append(args, keep.id)

	query := fmt.Sprintf(`UPDATE global_video SET %s, updated_at = CURRENT_TIMESTAMP WHERE id=?`, strings.Join(set, ", "))
	if _, err := tx.Exec(query, args...); err != nil {
		return fmt.Errorf("merge fields into global_video %d: %w", keep.id, err)
	}
	return nil
}

// repointCatalogReferences 改指目录投影。source_videos 的唯一键不含 global_id，
// 所以这里不可能冲突，直接 UPDATE。
func repointCatalogReferences(tx *sqlx.Tx, keepID, victimID int64) error {
	if _, err := tx.Exec(`UPDATE source_videos SET global_id=? WHERE global_id=?`, keepID, victimID); err != nil {
		return fmt.Errorf("repoint source_videos to global_video %d: %w", keepID, err)
	}
	return nil
}

// mergeFavorites 把收藏并到存活行。同一 (source_key, vod_id) 在两侧都收藏过
// 时才会冲突，那时存活行本来就有这条收藏，删掉重复行不丢信息。
func mergeFavorites(tx *sqlx.Tx, keepID, victimID int64) error {
	if _, err := tx.Exec(`UPDATE OR IGNORE favorites SET global_id=? WHERE global_id=?`, keepID, victimID); err != nil {
		return fmt.Errorf("repoint favorites to global_video %d: %w", keepID, err)
	}
	if _, err := tx.Exec(`DELETE FROM favorites WHERE global_id=?`, victimID); err != nil {
		return fmt.Errorf("drop merged favorites of %d: %w", victimID, err)
	}
	return nil
}

// mergeWatchHistory 逐集并观看进度：同一集只在两侧之一出现时直接改指，
// 两侧都看过时保留更大的进度，绝不因为唯一键冲突把用户看到的位置丢掉。
func mergeWatchHistory(tx *sqlx.Tx, keepID, victimID int64) error {
	var entries []struct {
		ID        int64   `db:"id"`
		SourceKey string  `db:"source_key"`
		EpNum     int     `db:"ep_num"`
		Position  float64 `db:"position"`
		UpdatedAt string  `db:"updated_at"`
	}
	if err := tx.Select(&entries, `SELECT id, source_key, ep_num, position, COALESCE(updated_at,'') AS updated_at
		FROM watch_history WHERE global_id=? ORDER BY ep_num ASC`, victimID); err != nil {
		return fmt.Errorf("read watch history of %d: %w", victimID, err)
	}
	for _, entry := range entries {
		var existing struct {
			ID       int64   `db:"id"`
			Position float64 `db:"position"`
		}
		err := tx.Get(&existing, `SELECT id, position FROM watch_history WHERE global_id=? AND source_key=? AND ep_num=? LIMIT 1`,
			keepID, entry.SourceKey, entry.EpNum)
		if err != nil {
			if _, err := tx.Exec(`UPDATE watch_history SET global_id=? WHERE id=?`, keepID, entry.ID); err != nil {
				return fmt.Errorf("repoint watch history to global_video %d: %w", keepID, err)
			}
			continue
		}
		if entry.Position > existing.Position {
			if _, err := tx.Exec(`UPDATE watch_history SET position=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
				entry.Position, existing.ID); err != nil {
				return fmt.Errorf("merge watch progress on global_video %d: %w", keepID, err)
			}
		}
		if _, err := tx.Exec(`DELETE FROM watch_history WHERE id=?`, entry.ID); err != nil {
			return fmt.Errorf("drop merged watch history %d: %w", entry.ID, err)
		}
	}
	return nil
}
