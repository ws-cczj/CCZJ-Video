package db

import (
	"fmt"
	"math"
	"strings"

	"cczjVideo/app/model"

	"github.com/jmoiron/sqlx"
)

// 备份/恢复用的读写。
//
// 收藏与历史在库里挂的是 global_id，那是本机自增主键，换机或换库后完全对不上号，
// 所以导出时一律换成「标题 + 年份 + 源坐标」，导入时再走唯一的身份解析入口
// （resolveGlobalVideoID）把本机 id 找回来。时间列都按 strftime 取成固定文本，
// 避免 modernc 驱动按 DATETIME decltype 把值改写成 RFC3339 之后没法比较。
//
// 读侧函数都收一个 *sqlx.DB：活库传 DB()，迁移归档传只读句柄，两边共用同一套查询。

// BackupSetting 是一条可搬运的设置。
type BackupSetting struct {
	Key   string `db:"key" json:"key"`
	Value string `db:"value" json:"value"`
}

// BackupFavorite 是收藏的本机无关形式。
type BackupFavorite struct {
	VodName   string `db:"vod_name" json:"vod_name"`
	Year      string `db:"year" json:"year"`
	SourceKey string `db:"source_key" json:"source_key"`
	VodID     string `db:"vod_id" json:"vod_id"`
	CreatedAt string `db:"created_at" json:"created_at"`
}

// BackupHistory 是观看历史的本机无关形式。
type BackupHistory struct {
	VodName   string  `db:"vod_name" json:"vod_name"`
	Year      string  `db:"year" json:"year"`
	SourceKey string  `db:"source_key" json:"source_key"`
	VodID     string  `db:"vod_id" json:"vod_id"`
	EpNum     int     `db:"ep_num" json:"ep_num"`
	Position  float64 `db:"position" json:"position"`
	UpdatedAt string  `db:"updated_at" json:"updated_at"`
}

// BackupVideoMeta 是被收藏或历史引用到的全局元数据。
// 换机后光有标题会只剩空壳条目，海报和评分要等豆瓣队列重跑才有，
// 所以这几列跟着备份走；导入时只补本机空着的列，不覆盖已有值。
type BackupVideoMeta struct {
	VodName       string `db:"vod_name" json:"vod_name"`
	Year          string `db:"year" json:"year"`
	Area          string `db:"area" json:"area"`
	Lang          string `db:"lang" json:"lang"`
	Writer        string `db:"writer" json:"writer"`
	Tag           string `db:"tag" json:"tag"`
	Pic           string `db:"pic" json:"pic"`
	DoubanID      string `db:"douban_id" json:"douban_id"`
	DoubanScore   string `db:"douban_score" json:"douban_score"`
	DoubanVotes   string `db:"douban_votes" json:"douban_votes"`
	DoubanHotness string `db:"douban_hotness" json:"douban_hotness"`
	Genre         string `db:"genre" json:"genre"`
	ReleaseDate   string `db:"release_date" json:"release_date"`
	Duration      string `db:"duration" json:"duration"`
	Aka           string `db:"aka" json:"aka"`
	Imdb          string `db:"imdb" json:"imdb"`
	SeasonCount   string `db:"season_count" json:"season_count"`
	EpisodeCount  string `db:"episode_count" json:"episode_count"`
}

const backupTimeFormat = "%Y-%m-%d %H:%M:%S"

// backupMetaColumns 是元数据列的固定顺序，读侧与写侧共用，避免两处漂移。
var backupMetaColumns = []string{
	"year", "area", "lang", "writer", "tag", "pic", "douban_id", "douban_score",
	"douban_votes", "douban_hotness", "genre", "release_date", "duration", "aka",
	"imdb", "season_count", "episode_count",
}

// metaColumnSQL 把元数据列读成一组固定顺序的字符串（COALESCE 兜 NULL）。
func metaColumnSQL() string {
	parts := make([]string, 0, len(backupMetaColumns)+2)
	parts = append(parts, `COALESCE(g.vod_name,'') vod_name`)
	for _, col := range backupMetaColumns {
		parts = append(parts, "COALESCE(g."+col+",'') "+col)
	}
	return strings.Join(parts, ", ")
}

// ReadAllSettings 返回整张设置表。备份文件要保全，导出侧不做筛选；
// 机器本地键由导入侧跳过。
func ReadAllSettings(database *sqlx.DB) ([]BackupSetting, error) {
	var rows []BackupSetting
	err := database.Select(&rows, `SELECT key, value FROM settings ORDER BY key`)
	return rows, err
}

// ReadFavorites 取出全部收藏，带上解析身份所需的标题与年份。
func ReadFavorites(database *sqlx.DB) ([]BackupFavorite, error) {
	var rows []BackupFavorite
	err := database.Select(&rows, `SELECT COALESCE(g.vod_name,'') vod_name, COALESCE(g.year,'') year,
			f.source_key, f.vod_id,
			COALESCE(strftime('`+backupTimeFormat+`', f.created_at),'') created_at
		FROM favorites f
		LEFT JOIN global_video g ON g.id = f.global_id
		ORDER BY f.id`)
	return rows, err
}

// ReadHistory 取出全部观看历史进度。
func ReadHistory(database *sqlx.DB) ([]BackupHistory, error) {
	var rows []BackupHistory
	err := database.Select(&rows, `SELECT COALESCE(g.vod_name,'') vod_name, COALESCE(g.year,'') year,
			h.source_key, h.vod_id, h.ep_num, h.position,
			COALESCE(strftime('`+backupTimeFormat+`', h.updated_at),'') updated_at
		FROM watch_history h
		LEFT JOIN global_video g ON g.id = h.global_id
		ORDER BY h.id`)
	return rows, err
}

// ReadLinkedVideoMeta 只导出被收藏或历史引用到的全局元数据：
// 全表 v_video 可以有几十万行，搬进备份文件既没必要也会让文件失控。
func ReadLinkedVideoMeta(database *sqlx.DB) ([]BackupVideoMeta, error) {
	var rows []BackupVideoMeta
	err := database.Select(&rows, `SELECT `+metaColumnSQL()+` FROM global_video g
		WHERE g.id IN (SELECT global_id FROM favorites UNION SELECT global_id FROM watch_history)
		ORDER BY g.id`)
	return rows, err
}

// ReadSources 让归档读取和活库共用同一条查询。
func ReadSources(database *sqlx.DB) ([]*model.Source, error) {
	var sources []*model.Source
	err := database.Select(&sources, `SELECT * FROM sources ORDER BY id`)
	return sources, err
}

// BackupIdentity 是导入侧待解析的一条标题身份。
type BackupIdentity struct {
	VodName string
	Year    string
}

// ResolveBackupIdentities 在一次事务里用同一份候选索引解析整批标题。
// 必须复用索引：GetOrCreateGlobalIDWithMeta 每次调用都会重读 global_video 全表，
// 在导入循环里逐条调用等于 O(导入条数 x 表大小)。
func ResolveBackupIdentities(items []BackupIdentity) ([]int64, error) {
	if len(items) == 0 {
		return nil, nil
	}
	tx, err := instance.Beginx()
	if err != nil {
		return nil, fmt.Errorf("begin identity resolve: %w", err)
	}
	rollback := func() { _ = tx.Rollback() }
	candidates, err := loadGlobalCandidates(tx)
	if err != nil {
		rollback()
		return nil, err
	}
	index := newGlobalVideoIndex(candidates)
	ids := make([]int64, len(items))
	for i, item := range items {
		id, _, err := resolveGlobalVideoID(tx, index, item.VodName, item.Year, 0)
		if err != nil {
			rollback()
			return nil, fmt.Errorf("resolve %q: %w", item.VodName, err)
		}
		ids[i] = id
	}
	if err := tx.Commit(); err != nil {
		rollback()
		return nil, fmt.Errorf("commit identity resolve: %w", err)
	}
	return ids, nil
}

// MergeFavoriteFromBackup 写入一条收藏，已有同一 global_id 时保持原样。
// 收藏是「只增不删」的：备份里没有的条目不能被视为用户想删掉。
// 返回是否真的写入。
func MergeFavoriteFromBackup(globalID int64, sourceKey, vodID, createdAt string) (bool, error) {
	if globalID <= 0 || sourceKey == "" || vodID == "" {
		return false, nil
	}
	res, err := instance.Exec(`INSERT INTO favorites (global_id, source_key, vod_id, created_at)
		SELECT ?, ?, ?, CASE ? WHEN '' THEN CURRENT_TIMESTAMP ELSE ? END
		WHERE NOT EXISTS (SELECT 1 FROM favorites WHERE global_id = ?)`,
		globalID, sourceKey, vodID, createdAt, createdAt, globalID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// MergeHistoryFromBackup 写入一条进度，仅当导入的 updated_at 比本机更新。
// 返回是否真的写入。
func MergeHistoryFromBackup(globalID int64, sourceKey, vodID string, epNum int, position float64, updatedAt string) (bool, error) {
	if globalID <= 0 || sourceKey == "" || vodID == "" || epNum <= 0 {
		return false, nil
	}
	if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 {
		return false, nil
	}
	res, err := instance.Exec(`INSERT INTO watch_history (global_id, source_key, vod_id, ep_num, position, updated_at)
		VALUES (?, ?, ?, ?, ?, CASE ? WHEN '' THEN CURRENT_TIMESTAMP ELSE ? END)
		ON CONFLICT(global_id, source_key, ep_num) DO UPDATE SET
		vod_id=excluded.vod_id, position=excluded.position, updated_at=excluded.updated_at
		WHERE watch_history.updated_at < excluded.updated_at`,
		globalID, sourceKey, vodID, epNum, position, updatedAt, updatedAt)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}

// MergeVideoMetaFromBackup 只在某列本机为空时填入备份里的值。
// 本机已有的值一律保留：导入是补全，不是覆盖。
// 返回是否有列被改动。
func MergeVideoMetaFromBackup(globalID int64, meta BackupVideoMeta) (bool, error) {
	if globalID <= 0 {
		return false, nil
	}
	values := []string{meta.Year, meta.Area, meta.Lang, meta.Writer, meta.Tag, meta.Pic,
		meta.DoubanID, meta.DoubanScore, meta.DoubanVotes, meta.DoubanHotness, meta.Genre,
		meta.ReleaseDate, meta.Duration, meta.Aka, meta.Imdb, meta.SeasonCount, meta.EpisodeCount}
	setParts := make([]string, 0, len(values))
	args := make([]interface{}, 0, len(values)+1)
	for i, col := range backupMetaColumns {
		if strings.TrimSpace(values[i]) == "" {
			continue
		}
		setParts = append(setParts, col+"=CASE WHEN COALESCE("+col+",'')='' THEN ? ELSE "+col+" END")
		args = append(args, values[i])
	}
	if len(setParts) == 0 {
		return false, nil
	}
	args = append(args, globalID)
	res, err := instance.Exec(`UPDATE global_video SET `+strings.Join(setParts, ", ")+` WHERE id=?`, args...)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	return affected > 0, err
}
