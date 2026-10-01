package db

import (
	"cczjVideo/app/applog"
	"fmt"
	"strconv"
	"strings"
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

	// 豆瓣的国家列表和源站的 vod_area 写法不一致（「中国大陆」/「大陆」），
	// 两侧都过同一个归一化，地区筛选才不会把一个产地切成两个桶。
	subjectCountry := normalizeArea(info.Country)
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
		subjectCountry, subjectCountry,
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
