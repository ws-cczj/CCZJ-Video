package db

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// GlobalVideoRow 全局视频表行（合并原 douban_info，包含所有共享元数据）
type GlobalVideoRow struct {
	Id                   int     `db:"id"`
	VodName              string  `db:"vod_name"`
	NameNorm             string  `db:"name_norm"`
	TypeId               int     `db:"type_id"`
	Year                 string  `db:"year"`
	Area                 string  `db:"area"`
	Lang                 string  `db:"lang"`
	Writer               string  `db:"writer"`
	Tag                  string  `db:"tag"`
	Pic                  string  `db:"pic"`
	DoubanId             string  `db:"douban_id"`
	DoubanScore          string  `db:"douban_score"`
	DoubanVotes          string  `db:"douban_votes"`
	DoubanHotness        string  `db:"douban_hotness"`
	Genre                string  `db:"genre"`
	ReleaseDate          string  `db:"release_date"`
	Duration             string  `db:"duration"`
	Aka                  string  `db:"aka"`
	Imdb                 string  `db:"imdb"`
	SeasonCount          string  `db:"season_count"`
	EpisodeCount         string  `db:"episode_count"`
	DoubanCooldownUntil  *string `db:"douban_cooldown_until"`
	DoubanSearchFailures int     `db:"douban_search_failures"`
	CreatedAt            string  `db:"created_at"`
	UpdatedAt            string  `db:"updated_at"`
}

// DoubanInfoRow 豆瓣信息视图（映射到 global_video 的豆瓣相关字段，兼容 updater.go 的调用方式）
type DoubanInfoRow struct {
	GlobalID     int    `db:"global_id"`
	SubjectID    string `db:"subject_id"`
	Rating       string `db:"rating"`
	Votes        string `db:"votes"`
	Director     string `db:"director"`
	Writer       string `db:"writer"`
	Actor        string `db:"actor"`
	Genre        string `db:"genre"`
	Country      string `db:"country"`
	Language     string `db:"language"`
	ReleaseDate  string `db:"release_date"`
	SeasonCount  string `db:"season_count"`
	EpisodeCount string `db:"episode_count"`
	Duration     string `db:"duration"`
	Aka          string `db:"aka"`
	Imdb         string `db:"imdb"`
	PosterURL    string `db:"poster_url"`
	UpdatedAt    string `db:"updated_at"`
	VodName      string `db:"vod_name"`
	Year         string `db:"year"`
	VodType      string `db:"vod_type"`
	Hotness      string `db:"douban_hotness"`
}

// normalizeSubjectID 将 subject_id 统一为纯整数字符串。
func normalizeSubjectID(sid string) string {
	sid = strings.TrimSpace(sid)
	if sid == "" || sid == "0" {
		return ""
	}
	if f, err := strconv.ParseFloat(sid, 64); err == nil {
		if f > 0 {
			return strconv.FormatInt(int64(f), 10)
		}
		return ""
	}
	return sid
}

// GetGlobalVideoByName 按 vod_name 查询全局视频
func GetGlobalVideoByName(vodName string) (*GlobalVideoRow, error) {
	var row GlobalVideoRow
	err := instance.Get(&row, `SELECT id, vod_name, type_id, year, area, lang, writer, tag, pic, douban_id, douban_score, douban_votes, genre, release_date, duration, aka, imdb, season_count, episode_count, douban_cooldown_until, douban_search_failures, created_at, updated_at FROM global_video WHERE vod_name = ? LIMIT 1`, vodName)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetGlobalVideoByID 按 id 查询全局视频
func GetGlobalVideoByID(id int) (*GlobalVideoRow, error) {
	var row GlobalVideoRow
	err := instance.Get(&row, `SELECT * FROM global_video WHERE id = ?`, id)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// UpsertDoubanInfo 更新 global_video 中的豆瓣相关字段（兼容 updater.go 的调用方式）
func UpsertDoubanInfo(info *DoubanInfoRow) error {
	if info == nil || info.GlobalID <= 0 {
		return fmt.Errorf("global_id is empty")
	}

	q := `UPDATE global_video SET
		douban_id = CASE WHEN ? != '' THEN ? ELSE douban_id END,
		douban_score = CASE WHEN ? != '' THEN ? ELSE douban_score END,
		douban_votes = CASE WHEN ? != '' THEN ? ELSE douban_votes END,
		writer = CASE WHEN ? != '' THEN ? ELSE writer END,
		genre = CASE WHEN ? != '' THEN ? ELSE genre END,
		area = CASE WHEN ? != '' THEN ? ELSE area END,
		lang = CASE WHEN ? != '' THEN ? ELSE lang END,
		release_date = CASE WHEN ? != '' THEN ? ELSE release_date END,
		season_count = CASE WHEN ? != '' THEN ? ELSE season_count END,
		episode_count = CASE WHEN ? != '' THEN ? ELSE episode_count END,
		duration = CASE WHEN ? != '' THEN ? ELSE duration END,
		aka = CASE WHEN ? != '' THEN ? ELSE aka END,
		imdb = CASE WHEN ? != '' THEN ? ELSE imdb END,
		pic = CASE WHEN ? != '' THEN ? ELSE pic END,
		douban_hotness = CASE WHEN ? != '' AND ? != '0' THEN ? ELSE douban_hotness END,
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`

	_, err := instance.Exec(q,
		info.SubjectID, info.SubjectID,
		info.Rating, info.Rating,
		info.Votes, info.Votes,
		info.Writer, info.Writer,
		info.Genre, info.Genre,
		info.Country, info.Country,
		info.Language, info.Language,
		info.ReleaseDate, info.ReleaseDate,
		info.SeasonCount, info.SeasonCount,
		info.EpisodeCount, info.EpisodeCount,
		info.Duration, info.Duration,
		info.Aka, info.Aka,
		info.Imdb, info.Imdb,
		info.PosterURL, info.PosterURL,
		info.Hotness, info.Hotness, info.Hotness,
		info.GlobalID)
	if err != nil {
		applog.Error("[Douban] UpsertDoubanInfo failed for global_id=%d: %v", info.GlobalID, err)
	}
	return err
}

// GetDoubanInfoByGlobalID 按 global_id 查询豆瓣信息（从 global_video 读取）
func GetDoubanInfoByGlobalID(globalID int) (*DoubanInfoRow, error) {
	var row DoubanInfoRow
	err := instance.Get(&row, `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		writer, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video WHERE id = ?`, globalID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetGlobalIDByDoubanSubject 通过豆瓣 subject_id 查找对应的 global_video.id
func GetGlobalIDByDoubanSubject(subjectID string) int {
	if subjectID == "" {
		return 0
	}
	var globalID int
	err := instance.Get(&globalID, `
		SELECT id
		FROM global_video
		WHERE douban_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT 1`, subjectID)
	if err != nil {
		return 0
	}
	return globalID
}

// doubanSiblingDonor 是同一条 douban_id 下已经补全过的记录，用来给缺字段的兄弟记录回填。
type doubanSiblingDonor struct {
	GlobalID      int64  `db:"global_id"`
	DoubanID      string `db:"douban_id"`
	Score         string `db:"score"`
	Votes         string `db:"votes"`
	Writer        string `db:"writer"`
	Genre         string `db:"genre"`
	Area          string `db:"area"`
	Lang          string `db:"lang"`
	ReleaseDate   string `db:"release_date"`
	SeasonCount   string `db:"season_count"`
	EpisodeCount  string `db:"episode_count"`
	Duration      string `db:"duration"`
	Aka           string `db:"aka"`
	Imdb          string `db:"imdb"`
	Pic           string `db:"pic"`
	DoubanHotness string `db:"douban_hotness"`
}

// InheritDoubanFieldsFromSiblings 把同 douban_id 兄弟记录已有的豆瓣字段补齐给缺字段的记录。
//
// 归一化只比较标题，所以「求救信号」和「求救信号2026」会各自建一条 global_video；
// 两条都被搜到同一个 subject_id 后，GetIncompleteDoubanInfo 又会用 NOT EXISTS 把
// 后一条永久排除掉（兄弟已有数据就不必再打豆瓣），于是它永远停在缺评分的状态。
// 这里补上缺失的那一步：不去合并本地主记录，只把兄弟的豆瓣字段抄过来。
// 只填空、不覆盖，且不动 vod_name/type_id/year 这些采集侧字段。
func InheritDoubanFieldsFromSiblings() (int, error) {
	q := `SELECT
		g.id AS global_id,
		g.douban_id,
		COALESCE(s.douban_score, '') AS score,
		COALESCE(s.douban_votes, '') AS votes,
		COALESCE(s.writer, '') AS writer,
		COALESCE(s.genre, '') AS genre,
		COALESCE(s.area, '') AS area,
		COALESCE(s.lang, '') AS lang,
		COALESCE(s.release_date, '') AS release_date,
		COALESCE(s.season_count, '') AS season_count,
		COALESCE(s.episode_count, '') AS episode_count,
		COALESCE(s.duration, '') AS duration,
		COALESCE(s.aka, '') AS aka,
		COALESCE(s.imdb, '') AS imdb,
		COALESCE(s.pic, '') AS pic,
		COALESCE(s.douban_hotness, '') AS douban_hotness
		FROM global_video g
		JOIN global_video s ON s.id = (
			SELECT donor.id
			FROM global_video donor
			WHERE donor.douban_id = g.douban_id
			  AND donor.id != g.id
			  AND TRIM(COALESCE(donor.douban_score, '')) != ''
			ORDER BY donor.updated_at ASC, donor.id ASC
			LIMIT 1
		)
		WHERE TRIM(COALESCE(g.douban_score, '')) = ''`

	var donors []*doubanSiblingDonor
	if err := instance.Select(&donors, q); err != nil {
		applog.Error("[Douban] InheritDoubanFieldsFromSiblings query failed: %v", err)
		return 0, err
	}
	if len(donors) == 0 {
		return 0, nil
	}

	update := `UPDATE global_video SET
		douban_score = CASE WHEN TRIM(COALESCE(douban_score, '')) = '' THEN ? ELSE douban_score END,
		douban_votes = CASE WHEN TRIM(COALESCE(douban_votes, '')) = '' THEN ? ELSE douban_votes END,
		writer = CASE WHEN TRIM(COALESCE(writer, '')) = '' THEN ? ELSE writer END,
		genre = CASE WHEN TRIM(COALESCE(genre, '')) = '' THEN ? ELSE genre END,
		area = CASE WHEN TRIM(COALESCE(area, '')) = '' THEN ? ELSE area END,
		lang = CASE WHEN TRIM(COALESCE(lang, '')) = '' THEN ? ELSE lang END,
		release_date = CASE WHEN TRIM(COALESCE(release_date, '')) = '' THEN ? ELSE release_date END,
		season_count = CASE WHEN TRIM(COALESCE(season_count, '')) = '' THEN ? ELSE season_count END,
		episode_count = CASE WHEN TRIM(COALESCE(episode_count, '')) = '' THEN ? ELSE episode_count END,
		duration = CASE WHEN TRIM(COALESCE(duration, '')) = '' THEN ? ELSE duration END,
		aka = CASE WHEN TRIM(COALESCE(aka, '')) = '' THEN ? ELSE aka END,
		imdb = CASE WHEN TRIM(COALESCE(imdb, '')) = '' THEN ? ELSE imdb END,
		pic = CASE WHEN TRIM(COALESCE(pic, '')) = '' THEN ? ELSE pic END,
		douban_hotness = CASE WHEN TRIM(COALESCE(douban_hotness, '')) = '' THEN ? ELSE douban_hotness END
		WHERE id = ?`

	// 一次回填可能涉及上千条兄弟记录：逐条自动提交就是上千次 fsync，
	// 整批包进一个事务才和它「批量补数据」的身份相称。
	tx, err := instance.Beginx()
	if err != nil {
		return 0, fmt.Errorf("begin sibling inherit: %w", err)
	}
	defer tx.Rollback()

	inherited := 0
	for _, d := range donors {
		if d == nil {
			continue
		}
		if _, err := tx.Exec(update,
			d.Score, d.Votes, d.Writer, d.Genre, d.Area, d.Lang, d.ReleaseDate,
			d.SeasonCount, d.EpisodeCount, d.Duration, d.Aka, d.Imdb, d.Pic, d.DoubanHotness,
			d.GlobalID); err != nil {
			applog.Error("[Douban] 同豆瓣ID回填失败 global_id=%d douban_id=%s: %v", d.GlobalID, d.DoubanID, err)
			continue
		}
		inherited++
		applog.Info("[Douban] 同豆瓣ID %s：global_id=%d 继承了兄弟记录的豆瓣字段", d.DoubanID, d.GlobalID)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit sibling inherit: %w", err)
	}
	return inherited, nil
}

// ChartDoubanUpdate 是一条热榜条目要写进 global_video 的字段。解析（评级校验、
// info 拆分）留在 douban 包，这里只负责落库。
type ChartDoubanUpdate struct {
	SubjectID   string
	Title       string
	Year        string
	Area        string
	ReleaseDate string
	Rating      string
	Votes       string
	PosterURL   string
}

// UpsertChartItems 一轮热榜一次入库：载入一遍 global_video 身份、开一个事务写完。
// 以前每条各自 ResolveGlobalVideoID(nil)：热榜一百条就是把整张 global_video 读一百遍
// 并重建一百次索引，然后摊成一百次自动提交。
// 返回新建与写入的行数；任何一步失败都会回滚整批并报错，不留半截热榜。
func UpsertChartItems(items []ChartDoubanUpdate) (int, int, error) {
	if len(items) == 0 {
		return 0, 0, nil
	}
	tx, err := instance.Beginx()
	if err != nil {
		return 0, 0, fmt.Errorf("begin chart upsert: %w", err)
	}
	defer tx.Rollback()
	candidates, err := loadGlobalCandidates(tx)
	if err != nil {
		return 0, 0, fmt.Errorf("load global video identities: %w", err)
	}
	index := newGlobalVideoIndex(candidates)
	created, updated := 0, 0
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if item.SubjectID == "" || title == "" {
			continue
		}
		// 类型未知时传 0：热榜条目和采集页共用同一套归一化身份阶梯。
		id, tier, err := resolveGlobalVideoID(tx, index, title, item.Year, 0)
		if err != nil {
			return 0, 0, fmt.Errorf("resolve chart title %q: %w", title, err)
		}
		if tier == tierNew {
			created++
		}
		// 已有值只填空缺，评分/票数按热榜刷新：热榜不能把详情抓回来的字段擦掉。
		if _, err := tx.Exec(`UPDATE global_video SET
			douban_id = CASE WHEN douban_id = '' THEN ? ELSE douban_id END,
			douban_score = CASE WHEN ? != '' THEN ? ELSE douban_score END,
			douban_votes = CASE WHEN ? != '' THEN ? ELSE douban_votes END,
			pic = CASE WHEN pic = '' AND ? != '' THEN ? ELSE pic END,
			year = CASE WHEN year = '' AND ? != '' THEN ? ELSE year END,
			area = CASE WHEN area = '' AND ? != '' THEN ? ELSE area END,
			release_date = CASE WHEN release_date = '' AND ? != '' THEN ? ELSE release_date END,
			updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			item.SubjectID,
			item.Rating, item.Rating,
			item.Votes, item.Votes,
			item.PosterURL, item.PosterURL,
			item.Year, item.Year,
			item.Area, item.Area,
			item.ReleaseDate, item.ReleaseDate,
			id); err != nil {
			return 0, 0, fmt.Errorf("write chart fields for id=%d: %w", id, err)
		}
		updated++
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, fmt.Errorf("commit chart upsert: %w", err)
	}
	return created, updated, nil
}

// UpdateDoubanHotnessBatch 一轮热榜写完整表热度：先把 subject_id 一次查回
// global_id，再在一个事务里写完。热榜每轮几十上百条，逐条查 + 逐条自动提交
// 就是几百次往返和同样多次的 fsync。
// 同一个 douban_id 有多条本地记录时写最早建立的那条，与 GetGlobalIDByDoubanSubject 一致。
func UpdateDoubanHotnessBatch(hotnessBySubject map[string]string) (int, error) {
	subjects := make([]string, 0, len(hotnessBySubject))
	for subject, hotness := range hotnessBySubject {
		if subject == "" || hotness == "" || hotness == "0" {
			continue
		}
		subjects = append(subjects, subject)
	}
	if len(subjects) == 0 {
		return 0, nil
	}
	args := make([]any, 0, len(subjects))
	for _, subject := range subjects {
		args = append(args, subject)
	}
	var rows []doubanHotnessRow
	q := `SELECT id, created_at, douban_id AS subject FROM global_video WHERE douban_id IN (` +
		strings.TrimSuffix(strings.Repeat("?,", len(subjects)), ",") + `)`
	if err := instance.Select(&rows, q, args...); err != nil {
		applog.Error("[Douban] UpdateDoubanHotnessBatch 查 global_id 失败: %v", err)
		return 0, err
	}
	earliest := make(map[string]doubanHotnessRow, len(rows))
	for _, row := range rows {
		if kept, ok := earliest[row.Subject]; !ok || row.CreatedAt < kept.CreatedAt || (row.CreatedAt == kept.CreatedAt && row.ID < kept.ID) {
			earliest[row.Subject] = row
		}
	}
	if len(earliest) == 0 {
		return 0, nil
	}

	tx, err := instance.Beginx()
	if err != nil {
		return 0, fmt.Errorf("begin hotness update: %w", err)
	}
	defer tx.Rollback()
	updated := 0
	for subject, row := range earliest {
		hotness := hotnessBySubject[subject]
		if _, err := tx.Exec(`UPDATE global_video SET douban_hotness = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`, hotness, row.ID); err != nil {
			applog.Error("[Douban] UpdateDoubanHotnessBatch 写入失败 global_id=%d: %v", row.ID, err)
			continue
		}
		updated++
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit hotness update: %w", err)
	}
	return updated, nil
}

type doubanHotnessRow struct {
	ID        int64  `db:"id"`
	Subject   string `db:"subject"`
	CreatedAt string `db:"created_at"`
}

// GetGlobalVideoByDoubanID 通过豆瓣 subject_id 查找最早建立的本地主记录。
// 豆瓣 ID 只用于外部关联，不能因为另一条记录字段更完整就改变本地主记录。
func GetGlobalVideoByDoubanID(doubanID string) (*GlobalVideoRow, error) {
	if doubanID == "" {
		return nil, fmt.Errorf("douban_id is empty")
	}
	var row GlobalVideoRow
	err := instance.Get(&row, `
		SELECT *
		FROM global_video
		WHERE douban_id = ?
		ORDER BY created_at ASC, id ASC
		LIMIT 1`, doubanID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// getBestGlobalVideoByDoubanID 只用于展示/回填时挑选字段最完整的副本，
// 不参与 global_id 归属判断，也不改变本地主记录。
func getBestGlobalVideoByDoubanID(doubanID string) (*GlobalVideoRow, error) {
	if doubanID == "" {
		return nil, fmt.Errorf("douban_id is empty")
	}
	var row GlobalVideoRow
	err := instance.Get(&row, `
		SELECT *
		FROM global_video
		WHERE douban_id = ?
		ORDER BY
			CASE WHEN TRIM(COALESCE(douban_score, '')) != '' THEN 0 ELSE 1 END,
			CASE WHEN TRIM(COALESCE(douban_votes, '')) != '' THEN 0 ELSE 1 END,
			CASE WHEN TRIM(COALESCE(pic, '')) != '' THEN 0 ELSE 1 END,
			updated_at DESC,
			id ASC
		LIMIT 1`, doubanID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetRecentGlobalVideos 获取最近更新的全局视频列表（用于保底填充）
func GetRecentGlobalVideos(limit int) ([]GlobalVideoRow, error) {
	if limit <= 0 {
		limit = 10
	}
	var rows []GlobalVideoRow
	err := instance.Select(&rows, `SELECT * FROM global_video WHERE pic != '' ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// GetCachedDoubanChartVideos returns the last chart-like rows persisted by the
// chart updater. It is intentionally a read-only fallback for when Douban is
// temporarily unavailable or presents an anti-crawl page.
func GetCachedDoubanChartVideos(limit int) ([]GlobalVideoRow, error) {
	if limit <= 0 {
		limit = 20
	}
	var rows []GlobalVideoRow
	err := instance.Select(&rows, `
		SELECT *
		FROM global_video
		WHERE TRIM(COALESCE(douban_id, '')) != ''
		  AND TRIM(COALESCE(pic, '')) != ''
		ORDER BY CAST(douban_hotness AS INTEGER) DESC, updated_at DESC, id ASC
		LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// SearchVideoInSourceTable 在指定源的视频表中按名称搜索
func SearchVideoInSourceTable(sourceKey, title string) (string, bool) {
	if sourceKey == "" || title == "" {
		return "", false
	}
	var vodID string
	err := instance.Get(&vodID, `SELECT source_vod_id FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos")+` AND vod_name=? LIMIT 1`, sourceKey, title)
	if err == nil && vodID != "" {
		return vodID, true
	}

	// 模糊匹配：标题包含
	err = instance.Get(&vodID, `SELECT source_vod_id FROM source_videos WHERE source_key=? AND lifecycle_state='active' AND `+catalogTypeVisibilityClause("source_videos")+` AND vod_name LIKE ? LIMIT 1`, sourceKey, "%"+title+"%")
	if err == nil && vodID != "" {
		return vodID, true
	}

	return "", false
}

// GetDoubanInfoByVodName 通过 vod_name 查询豆瓣信息
func GetDoubanInfoByVodName(vodName string) (*DoubanInfoRow, error) {
	var row DoubanInfoRow
	err := instance.Get(&row, `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		writer, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video WHERE vod_name = ? LIMIT 1`, vodName)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetDoubanInfoByKeyword 先精确匹配 vod_name，再 LIKE 模糊匹配
func GetDoubanInfoByKeyword(keyword string) (*DoubanInfoRow, error) {
	row, err := GetDoubanInfoByVodName(keyword)
	if err == nil {
		return row, nil
	}

	var rows []DoubanInfoRow
	err = instance.Select(&rows, `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		writer, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video WHERE vod_name LIKE ? LIMIT 1`, "%"+keyword+"%")
	if err != nil || len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return &rows[0], nil
}

// GetIncompleteDoubanInfo 获取豆瓣信息不完整的记录（有 douban_id 但缺评分/导演且未在冷却期内）
// 冷却期：24小时内已尝试过且未获取到评分的记录暂不重试
//
// 排序按「上次被豆瓣试过」的时刻升序：SQLite 的 ASC 把 NULL 排在最前，所以从没试过
// 的新记录优先，试过但没补上的行会自动退到队尾，下一轮才轮到别的记录。
// 原来是按 updated_at（或 id）排，失败行不动那些列，于是每轮都取到同一批队头，
// 后面的记录永远饿死。
func GetIncompleteDoubanInfo(limit int) ([]*DoubanInfoRow, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*DoubanInfoRow
	q := `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		writer, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video
		WHERE douban_id != '' AND douban_id != '0'
		AND douban_score = ''
		AND NOT EXISTS (
			SELECT 1
			FROM global_video complete
			WHERE complete.douban_id = global_video.douban_id
			  AND complete.id != global_video.id
			  AND (
				TRIM(COALESCE(complete.douban_score, '')) != ''
				OR TRIM(COALESCE(complete.douban_votes, '')) != ''
			  )
		)
		AND (douban_cooldown_until IS NULL OR douban_cooldown_until < ?)
		ORDER BY douban_last_attempt_at ASC, updated_at ASC LIMIT ?`
	err := instance.Select(&rows, q,
		time.Now().Format(doubanCooldownFormat), limit)
	if err != nil {
		applog.Error("[Douban] GetIncompleteDoubanInfo query failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// GetDoubanInfoMissingSubjectID 获取 douban_id 为空的记录（排除冷却期内的记录）
// 排序同样按「上次被豆瓣试过」的时刻轮转，理由见 GetIncompleteDoubanInfo。
func GetDoubanInfoMissingSubjectID(limit int) ([]*DoubanInfoRow, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*DoubanInfoRow
	q := `SELECT
		gv.id AS global_id,
		COALESCE(gv.douban_id, '') AS subject_id,
		COALESCE(gv.douban_score, '') AS rating,
		COALESCE(gv.douban_votes, '') AS votes,
		COALESCE(gv.writer, '') AS writer,
		COALESCE(gv.genre, '') AS genre,
		COALESCE(gv.area, '') AS country,
		COALESCE(gv.lang, '') AS language,
		COALESCE(gv.release_date, '') AS release_date,
		COALESCE(gv.season_count, '') AS season_count,
		COALESCE(gv.episode_count, '') AS episode_count,
		COALESCE(gv.duration, '') AS duration,
		COALESCE(gv.aka, '') AS aka,
		COALESCE(gv.imdb, '') AS imdb,
		COALESCE(gv.pic, '') AS poster_url,
		gv.updated_at, gv.vod_name,
		COALESCE(gv.year, '') AS year,
		COALESCE(gt.type_name, '') AS vod_type
		FROM global_video gv
		LEFT JOIN global_types gt ON gv.type_id = gt.id
		WHERE (gv.douban_id = '' OR gv.douban_id IS NULL)
		AND (gv.douban_cooldown_until IS NULL OR gv.douban_cooldown_until < ?)
		ORDER BY gv.douban_last_attempt_at ASC, gv.id ASC LIMIT ?`
	err := instance.Select(&rows, q,
		time.Now().Format(doubanCooldownFormat), limit)
	if err != nil {
		applog.Error("[Douban] GetDoubanInfoMissingSubjectID query failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// EnrichVideoWithDouban 用全局视频表补充 Video 对象的缺失字段
func EnrichVideoWithDouban(v *model.Video) {
	if v == nil || v.VodName == "" {
		return
	}

	row, err := GetDoubanInfoByKeyword(v.VodName)
	if err != nil {
		return
	}

	// global_video 字段
	if v.VodPic == "" && row.PosterURL != "" {
		v.VodPic = row.PosterURL
	}
	if v.VodArea == "" && row.Country != "" {
		v.VodArea = row.Country
	}
	if v.VodLang == "" && row.Language != "" {
		v.VodLang = row.Language
	}
	if v.VodTag == "" && row.Genre != "" {
		v.VodTag = row.Genre
	}
	if v.VodSub == "" && row.Aka != "" {
		v.VodSub = row.Aka
	}
	if v.VodRemarks == "" && row.EpisodeCount != "" {
		v.VodRemarks = "共" + row.EpisodeCount + "集"
	}
	if v.VodDoubanId.String() == "" && row.SubjectID != "" {
		v.VodDoubanId = model.FlexibleString(row.SubjectID)
	}
	if v.VodDoubanScore.String() == "" && row.Rating != "" {
		v.VodDoubanScore = model.FlexibleString(row.Rating)
	}
	if v.VodYear == "" && row.ReleaseDate != "" {
		releaseDate := strings.TrimSpace(row.ReleaseDate)
		if len(releaseDate) >= 4 {
			v.VodYear = releaseDate[:4]
		}
	}
}

// EnrichVideoWithDoubanByGlobalID 按 catalog 的 global_id 精确回填豆瓣字段。
// 详情按需拉取后源站数据通常不带 vod_douban_id，必须从 global_video 回填，
// 否则播放页的豆瓣评论入口无法显示。global_id 不可用时退回按名称匹配。
func EnrichVideoWithDoubanByGlobalID(v *model.Video, globalID int64) {
	if v == nil || instance == nil {
		return
	}
	if globalID <= 0 {
		EnrichVideoWithDouban(v)
		return
	}
	row, err := GetGlobalVideoByID(int(globalID))
	if err != nil {
		EnrichVideoWithDouban(v)
		return
	}
	// 同一个豆瓣 subject 可能因标题标点差异产生多条 global_video 记录。
	// 这里仍保留当前 global_id，只借用完整副本的字段供展示，不改变本地身份。
	if row.DoubanId != "" {
		if best, bestErr := getBestGlobalVideoByDoubanID(row.DoubanId); bestErr == nil {
			bestCopy := *best
			bestCopy.Id = row.Id
			row = &bestCopy
		}
	}
	if v.VodPic == "" && row.Pic != "" {
		v.VodPic = row.Pic
	}
	if v.VodArea == "" && row.Area != "" {
		v.VodArea = row.Area
	}
	if v.VodLang == "" && row.Lang != "" {
		v.VodLang = row.Lang
	}
	genre := row.Genre
	if genre == "" {
		genre = row.Tag
	}
	if v.VodTag == "" && genre != "" {
		v.VodTag = genre
	}
	if v.VodSub == "" && row.Aka != "" {
		v.VodSub = row.Aka
	}
	if v.VodRemarks == "" && row.EpisodeCount != "" {
		v.VodRemarks = "共" + row.EpisodeCount + "集"
	}
	if v.VodDoubanId.String() == "" && row.DoubanId != "" {
		v.VodDoubanId = model.FlexibleString(row.DoubanId)
	}
	if v.VodDoubanScore.String() == "" && row.DoubanScore != "" {
		v.VodDoubanScore = model.FlexibleString(row.DoubanScore)
	}
	if v.VodYear == "" && row.Year != "" {
		v.VodYear = row.Year
	}
}

// EnrichVideosWithDouban 批量版 enrich：用 1 次查询替代 N 次
func EnrichVideosWithDouban(videos []*model.Video) {
	names := make([]string, 0, len(videos))
	seen := make(map[string]bool, len(videos))
	for _, v := range videos {
		if v == nil || v.VodName == "" || seen[v.VodName] {
			continue
		}
		seen[v.VodName] = true
		names = append(names, v.VodName)
	}
	if len(names) == 0 {
		return
	}

	query, args, err := sqlx.In(`
		SELECT
			vod_name,
			pic, area, lang, tag AS genre,
			aka, episode_count, douban_id, douban_score, release_date
		FROM global_video
		WHERE vod_name IN (?)`, names)
	if err != nil {
		for _, v := range videos {
			EnrichVideoWithDouban(v)
		}
		return
	}
	query = instance.Rebind(query)

	type enrichRow struct {
		VodName      string `db:"vod_name"`
		Pic          string `db:"pic"`
		Area         string `db:"area"`
		Lang         string `db:"lang"`
		Genre        string `db:"genre"`
		Aka          string `db:"aka"`
		EpisodeCount string `db:"episode_count"`
		DoubanId     string `db:"douban_id"`
		DoubanScore  string `db:"douban_score"`
		ReleaseDate  string `db:"release_date"`
	}

	var rows []enrichRow
	if err := instance.Select(&rows, query, args...); err != nil {
		for _, v := range videos {
			EnrichVideoWithDouban(v)
		}
		return
	}

	m := make(map[string]*enrichRow, len(rows))
	for i := range rows {
		m[rows[i].VodName] = &rows[i]
	}

	for _, v := range videos {
		if v == nil || v.VodName == "" {
			continue
		}
		r := m[v.VodName]
		if r == nil {
			continue
		}
		if v.VodPic == "" && r.Pic != "" {
			v.VodPic = r.Pic
		}
		if v.VodArea == "" && r.Area != "" {
			v.VodArea = r.Area
		}
		if v.VodLang == "" && r.Lang != "" {
			v.VodLang = r.Lang
		}
		if v.VodTag == "" && r.Genre != "" {
			v.VodTag = r.Genre
		}
		if v.VodSub == "" && r.Aka != "" {
			v.VodSub = r.Aka
		}
		if v.VodRemarks == "" && r.EpisodeCount != "" {
			v.VodRemarks = "共" + r.EpisodeCount + "集"
		}
		if v.VodDoubanId.String() == "" && r.DoubanId != "" {
			v.VodDoubanId = model.FlexibleString(r.DoubanId)
		}
		if v.VodDoubanScore.String() == "" && r.DoubanScore != "" {
			v.VodDoubanScore = model.FlexibleString(r.DoubanScore)
		}
		if v.VodYear == "" && r.ReleaseDate != "" {
			releaseDate := strings.TrimSpace(r.ReleaseDate)
			if len(releaseDate) >= 4 {
				v.VodYear = releaseDate[:4]
			}
		}
	}
}

// GetAllDoubanInfo 获取所有有豆瓣数据的记录
func GetAllDoubanInfo() ([]*DoubanInfoRow, error) {
	rows, _, err := GetAllDoubanInfoPaginated(1, 0)
	return rows, err
}

// GetAllDoubanInfoPaginated 分页获取豆瓣数据，pageSize=0 表示不分页
func GetAllDoubanInfoPaginated(page, pageSize int) ([]*DoubanInfoRow, int, error) {
	// 先查总数
	var total int
	err := instance.Get(&total, `SELECT COUNT(*) FROM global_video WHERE douban_id != '' OR douban_score != ''`)
	if err != nil {
		return nil, 0, err
	}

	var rows []*DoubanInfoRow
	query := `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		writer, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video
		WHERE douban_id != '' OR douban_score != ''
		ORDER BY id DESC`

	if pageSize > 0 {
		offset := (page - 1) * pageSize
		if offset < 0 {
			offset = 0
		}
		query += fmt.Sprintf(" LIMIT %d OFFSET %d", pageSize, offset)
	}

	err = instance.Select(&rows, query)
	if err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// MarkDoubanInfoUpdated 标记某条记录的更新时间
func MarkDoubanInfoUpdated(globalID int) error {
	_, err := instance.Exec(`UPDATE global_video SET updated_at = CURRENT_TIMESTAMP WHERE id = ?`, globalID)
	return err
}

// ==================== 字符串相似度计算 ====================
// 名称归一化只在 identity.go 的 normalizeTitle 里实现一次：Go 算出 key 存进 global_video.name_norm，
// 索引和查询都读那一列，不再在 SQL 里重抄一遍表达式。

// editDistance 计算两个字符串的编辑距离（Levenshtein）
func editDistance(a, b string) int {
	la, lb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	ra := []rune(a)
	rb := []rune(b)

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// normalizedSimilarity 计算两个「已归一化」名称的相似度 0.0~1.0。
// 归一化由调用方负责（identity.go 的 normalizeTitle），这样身份阶梯里同一对
// 字符串不会被反复归一化；公式仍是 1 - 编辑距离/最大长度。
func normalizedSimilarity(na, nb string) float64 {
	if na == nb {
		return 1.0
	}
	if len(na) == 0 || len(nb) == 0 {
		return 0.0
	}
	maxLen := utf8.RuneCountInString(na)
	if nbLen := utf8.RuneCountInString(nb); nbLen > maxLen {
		maxLen = nbLen
	}
	dist := editDistance(na, nb)
	return 1.0 - float64(dist)/float64(maxLen)
}

// seasonSuffixPattern 匹配季/部/期/卷等后缀模式
// 用于防止“权力的游戏 第一季”和“权力的游戏 第二季”被误判为同一视频
var seasonSuffixPattern = regexp.MustCompile(`第[一二三四五六七八九十百千\d]+[季部期卷集]|season\s*\d+|s\d+|part\s*\d+|[（(]\s*\d+\s*[）)]|[ⅠⅡⅢⅣⅤⅥⅦⅧⅨⅩ]+|[上下][集部篇]?`)

// hasSeasonSuffix 检查两个名称的差异部分是否包含季/部/期等后缀
// 如果 a 和 b 的差异仅在于季/部/期后缀不同，返回 true
func hasSeasonSuffix(a, b string) bool {
	na := normalizeTitle(a)
	nb := normalizeTitle(b)
	if na == nb {
		return false
	}
	// 检查较长的名称中是否包含季/部/期后缀，且较短的名称中不包含
	var longer, shorter string
	if len([]rune(na)) > len([]rune(nb)) {
		longer, shorter = na, nb
	} else {
		longer, shorter = nb, na
	}
	// 如果较长名称有季/部/期后缀但较短名称没有，视为不同视频
	hasLong := seasonSuffixPattern.MatchString(longer)
	hasShort := seasonSuffixPattern.MatchString(shorter)
	if hasLong && !hasShort {
		return true
	}
	// 如果两者都有季/部/期后缀但后缀不同（如"第一季" vs "第二季"），也视为不同视频
	if hasLong && hasShort {
		// 提取后缀进行比较
		lm := seasonSuffixPattern.FindString(longer)
		sm := seasonSuffixPattern.FindString(shorter)
		if lm != "" && sm != "" && lm != sm {
			return true
		}
	}
	return false
}

// metadataMatch 比对视频的关键元数据是否吻合
// 要求年份相同，且导演或演员至少有一个非空交集
func metadataMatch(yearA, directorA, actorA, yearB, directorB, actorB string) bool {
	// 年份必须匹配（如果双方都有年份）
	if yearA != "" && yearB != "" && yearA != yearB {
		return false
	}
	// 导演或演员至少有一个交集
	dirOverlap := hasCommonToken(directorA, directorB)
	actOverlap := hasCommonToken(actorA, actorB)
	return dirOverlap || actOverlap
}

// hasCommonToken 检查两个逗号/空格分隔的字符串是否有共同项
func hasCommonToken(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	split := func(s string) []string {
		s = strings.ReplaceAll(s, ",", "\x00")
		s = strings.ReplaceAll(s, "/", "\x00")
		return strings.Split(s, "\x00")
	}
	tokensA := split(strings.ToLower(a))
	tokensB := split(strings.ToLower(b))
	set := make(map[string]bool, len(tokensA))
	for _, t := range tokensA {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = true
		}
	}
	for _, t := range tokensB {
		t = strings.TrimSpace(t)
		if t != "" && set[t] {
			return true
		}
	}
	return false
}

// GetOrCreateGlobalID 按标题取或建 global_video 记录。阶梯只有一份实现，见 identity.go。
func GetOrCreateGlobalID(vodName string, typeId int64) (int64, error) {
	return GetOrCreateGlobalIDWithMeta(vodName, "", typeId)
}

// GetOrCreateGlobalIDWithMeta 带元数据的身份解析，用于采集入库、豆瓣补全、收藏与历史。
// year 只参与别名档和相似度档的校验；typeId=0 表示「不知道类型」，此时不限类型匹配。
func GetOrCreateGlobalIDWithMeta(vodName, year string, typeId int64) (int64, error) {
	id, tier, err := resolveGlobalVideoID(instance, nil, vodName, year, typeId)
	if err != nil {
		return 0, err
	}
	if tier != tierNew {
		applog.Info("[global] 身份匹配(%s): %q (year=%q, type_id=%d) -> global_id=%d", tier, vodName, year, typeId, id)
	} else {
		applog.Info("[global] 新建条目: %q (year=%q, type_id=%d) -> global_id=%d", vodName, year, typeId, id)
	}
	return id, nil
}

// doubanAttemptColumn 是「这一行上次被豆瓣队列试过」的时刻，只由 MarkDoubanAttempt 写。
const doubanAttemptColumn = "douban_last_attempt_at"

// doubanCooldownFormat 是冷却/尝试时间落库的格式。SQLite 里按字符串比较，
// 读侧解析必须用同一个布局。
const doubanCooldownFormat = "2006-01-02 15:04:05"

// doubanCooldownHours 是一次「确定没查到」之后搁置多久。查无此片的记录不该每轮批量都
// 去撞一次豆瓣；但也不能永久否决——豆瓣随时可能收录。
const doubanCooldownHours = 24

// searchFailureCooldownThreshold 是累计多少次正常页无结果才进冷却（沿用旧策略）。
const searchFailureCooldownThreshold = 5

// SetDoubanCooldown 为指定 global_id 设置24小时冷静期（用于详情拿到但无评分）
func SetDoubanCooldown(globalID int) error {
	cooldownUntil := time.Now().Add(doubanCooldownHours * time.Hour).Format(doubanCooldownFormat)
	_, err := instance.Exec(`UPDATE global_video SET douban_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		cooldownUntil, globalID)
	if err != nil {
		applog.Error("[Douban] SetDoubanCooldown failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// MarkDoubanAttempt 记下「这一行刚试过」。
//
// 空跑也必须打点：批量队列按最近尝试时间轮转，只在成功时打点等于让持续失败的行
// 永远霸占队头。刻意不动 updated_at——那一列是兄弟记录继承和列表排序的依据。
func MarkDoubanAttempt(globalID int) error {
	if globalID <= 0 {
		return nil
	}
	_, err := instance.Exec(`UPDATE global_video SET `+doubanAttemptColumn+` = ? WHERE id = ?`,
		time.Now().Format(doubanCooldownFormat), globalID)
	if err != nil {
		applog.Error("[Douban] MarkDoubanAttempt failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// doubanFailuresRow 只带失败计数：冷却时刻留在库里比，不读到 Go 再解析（见
// IsDoubanSearchOnCooldown）。
type doubanFailuresRow struct {
	Failures int `db:"douban_search_failures"`
}

// IsDoubanSearchOnCooldown 检查这一行是否还在搜索冷却期内。
// globalID <= 0（没有行身份，例如用户在界面上手搜）视为不在冷却期。
//
// 比较必须在 SQL 里做完，不能把 douban_cooldown_until 读进 Go 再 time.Parse：
// 那一列声明成 DATETIME，modernc 驱动按 decltype 把读回的值当日期处理，扫进
// *string 得到的是 "2026-09-29T01:50:25Z"，而不是写进去的 "2026-09-29 01:50:25"，
// 解析永远失败。以前这里解析失败就静默 return false，于是 24 小时冷却一次也没生效过，
// 查无此片的记录每轮批量都重新去撞豆瓣。库里存的就是文本，和写侧同一个布局，
// SQL 的字符串比较即时间先后。
func IsDoubanSearchOnCooldown(globalID int) bool {
	if globalID <= 0 {
		return false
	}
	var onCooldown int
	// NULL > ? 落到 CASE 的 ELSE，所以「没冷却」和「冷却已过」共用一条语句。
	err := instance.Get(&onCooldown, `SELECT CASE WHEN douban_cooldown_until > ? THEN 1 ELSE 0 END
		FROM global_video WHERE id = ?`, time.Now().Format(doubanCooldownFormat), globalID)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		applog.Error("[Douban] IsDoubanSearchOnCooldown global_id=%d 查询失败，按未冷却处理: %v", globalID, err)
		return false
	}
	return onCooldown == 1
}

// MarkDoubanSearchFailure 给这一行累计一次「正常页但没认出头」的失败，达到阈值进冷却。
//
// 调用方必须已经确认拿到过正常搜索页——网络错误或验证页不该走到这里。
// 这里也不再 GetOrCreateGlobalID：为一次失败的搜索去新建 global_video 行，
// 等于让采集结果凭空多出一条永远补不全的记录。
func MarkDoubanSearchFailure(globalID int) error {
	if globalID <= 0 {
		return nil
	}
	var row doubanFailuresRow
	if err := instance.Get(&row, `SELECT COALESCE(douban_search_failures, 0) AS douban_search_failures
		FROM global_video WHERE id = ?`, globalID); err != nil {
		return err
	}
	newCount := row.Failures + 1
	cooldownUntil := ""
	if newCount >= searchFailureCooldownThreshold {
		cooldownUntil = time.Now().Add(doubanCooldownHours * time.Hour).Format(doubanCooldownFormat)
	}
	_, err := instance.Exec(`UPDATE global_video SET douban_search_failures = ?, douban_cooldown_until = COALESCE(NULLIF(?, ''), douban_cooldown_until) WHERE id = ?`,
		newCount, cooldownUntil, globalID)
	if err != nil {
		applog.Error("[Douban] MarkDoubanSearchFailure failed for global_id=%d: %v", globalID, err)
		return err
	}
	if cooldownUntil != "" {
		applog.Info("[Douban] 搜索冷却生效 global_id=%d (failures=%d, until=%s)", globalID, newCount, cooldownUntil)
	}
	return nil
}

// ClearDoubanSearchFailure 搜索成功时清掉本行的失败计数与冷却。
// 只清这一行：同名的其它记录各有各的尝试结果，不该跟着一起放行。
func ClearDoubanSearchFailure(globalID int) error {
	if globalID <= 0 {
		return nil
	}
	_, err := instance.Exec(`UPDATE global_video SET douban_search_failures = 0, douban_cooldown_until = NULL WHERE id = ?`, globalID)
	if err != nil {
		applog.Error("[Douban] ClearDoubanSearchFailure failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// ErrDoubanNotFound 错误
var ErrDoubanNotFound = errors.New("douban info not found")
