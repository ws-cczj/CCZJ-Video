package db

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

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
