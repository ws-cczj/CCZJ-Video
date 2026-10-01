package db

import (
	"fmt"

	"github.com/jmoiron/sqlx"
)

// 三条数据修正迁移：地区写法收拢、按新的标题归一化重算身份、把同一个豆瓣条目
// 被拆成两条的身份并回去。
//
// 都走迁移而不是启动时顺手做，是因为它们改的是用户已有的数据：迁移有自己的事务、
// 有 schema 版本号，失败会整体回滚，而且 runMigrations 会先备份一份库
// （见 backupBeforeMigrations），改坏了还能从 schema-backups 里捞回来。

// areaMigrationTargets 是要归一化地区的表与列。列名写死，不接受外部输入。
var areaMigrationTargets = []struct {
	table  string
	column string
}{
	{"global_video", "area"},
	{"source_videos", "vod_area"},
}

// migrateNormalizeAreaValues 把历史行里的地区写法归到规范名。
//
// 按「不同值」而不是按行更新：全库几百行里通常只有十来个不同写法，
// 一个值一条 UPDATE 比逐行绑定少几个数量级的语句。
func migrateNormalizeAreaValues(tx *sqlx.Tx) error {
	changed := 0
	for _, target := range areaMigrationTargets {
		exists, err := tableExists(tx, target.table)
		if err != nil || !exists {
			return err
		}
		var values []string
		query := fmt.Sprintf(`SELECT DISTINCT COALESCE(%s, '') FROM %s`,
			quoteIdentifier(target.column), quoteIdentifier(target.table))
		if err := tx.Select(&values, query); err != nil {
			return fmt.Errorf("read distinct %s.%s: %w", target.table, target.column, err)
		}
		for _, raw := range values {
			normalized := normalizeArea(raw)
			if normalized == raw {
				continue
			}
			update := fmt.Sprintf(`UPDATE %s SET %s=? WHERE COALESCE(%s, '')=?`,
				quoteIdentifier(target.table), quoteIdentifier(target.column), quoteIdentifier(target.column))
			if _, err := tx.Exec(update, normalized, raw); err != nil {
				return fmt.Errorf("normalize %s.%s %q: %w", target.table, target.column, raw, err)
			}
			changed++
		}
	}
	if changed > 0 {
		logInfo(fmt.Sprintf("已归一 %d 组地区写法", changed))
	}
	return nil
}

// migrateRetitleGlobalVideo 用新的标题归一化重算 name_norm，然后合并因此撞上同一身份的
// 行。和 v2/v3 同一套动作，差别只在于这次必须先把唯一索引删掉：待合并的重复行
// 正躺在库里，索引会当场把 UPDATE 挡成 UNIQUE constraint failed。
// 索引由迁移结束后 runMigrations 里的 createIndexes 在同一批建回来。
func migrateRetitleGlobalVideo(tx *sqlx.Tx) error {
	exists, err := tableExists(tx, "global_video")
	if err != nil || !exists {
		return err
	}
	if _, err := tx.Exec("DROP INDEX IF EXISTS " + quoteIdentifier("idx_gv_name_type_norm")); err != nil {
		return fmt.Errorf("drop unique title index: %w", err)
	}
	updated, err := backfillNameNorm(tx)
	if err != nil {
		return err
	}
	logInfo(fmt.Sprintf("按新的标题归一化重算了 %d 条 name_norm", updated))
	merged, err := mergeDuplicateGlobalVideos(tx)
	if err != nil {
		return err
	}
	if merged > 0 {
		logInfo(fmt.Sprintf("归一化后合并了 %d 条重复的 global_video 记录", merged))
	}
	return nil
}

// doubanIdentityGroup 是同一豆瓣条目下被拆开的几条身份。
type doubanIdentityGroup struct {
	DoubanID string `db:"douban_id"`
	NameNorm string `db:"name_norm"`
}

// migrateMergeSameDoubanIdentity 把「同一个豆瓣条目挂了两条 global_video」并成一条。
//
// 为什么不能只靠标题归一化：v8 那条迁移能并掉「雷神4：爱与雷霆_1080P_」这类
// 画质尾巴，但两条记录在源站的类型可以完全不同（一边报动作片、一边报喜剧片），
// 而 (name_norm, type_id) 的唯一索引本来就是允许同名不同类型共存的。
// 豆瓣条目 ID 是比类型更强的身份，所以这里按它分组。
//
// 两道保险，缺一条就不并，宁可留给界面里的人工合并队列：
//  1. 归一化标题必须完全一致——防止豆瓣抓取错挂，把一个 ID 撒到两部片上；
//  2. 非空年份最多只能有一种——「老友记 1994」和重拍版各是一条目。
func migrateMergeSameDoubanIdentity(tx *sqlx.Tx) error {
	exists, err := tableExists(tx, "global_video")
	if err != nil || !exists {
		return err
	}
	var groups []doubanIdentityGroup
	err = tx.Select(&groups, `SELECT TRIM(douban_id) AS douban_id, name_norm FROM global_video
		WHERE COALESCE(TRIM(douban_id), '') <> '' AND COALESCE(name_norm, '') <> ''
		GROUP BY TRIM(douban_id), name_norm
		HAVING COUNT(*) > 1
			AND COUNT(DISTINCT CASE WHEN COALESCE(TRIM(year), '') <> '' THEN TRIM(year) END) <= 1
		ORDER BY TRIM(douban_id), name_norm`)
	if err != nil {
		return fmt.Errorf("find same-douban-id groups: %w", err)
	}
	merged := 0
	for _, group := range groups {
		var ids []int64
		err := tx.Select(&ids, `SELECT id FROM global_video
			WHERE TRIM(COALESCE(douban_id, ''))=? AND name_norm=? ORDER BY id ASC`,
			group.DoubanID, group.NameNorm)
		if err != nil {
			return fmt.Errorf("read same-douban-id group %q: %w", group.DoubanID, err)
		}
		if len(ids) < 2 {
			continue
		}
		rows, err := loadDupGroupByIDs(tx, ids)
		if err != nil {
			return err
		}
		count, err := mergeDupGroup(tx, rows)
		if err != nil {
			return err
		}
		merged += count
		logInfo(fmt.Sprintf("豆瓣条目 %s 的 %d 条身份已合并", group.DoubanID, count+1))
	}
	if merged > 0 {
		logInfo(fmt.Sprintf("按豆瓣条目 ID 合并了 %d 条重复的 global_video 记录", merged))
	}
	return nil
}
