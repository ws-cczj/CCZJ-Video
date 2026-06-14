package db

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GlobalVideoRow 全局视频去重表行
type GlobalVideoRow struct {
	Id        int    `db:"id"`
	VodName   string `db:"vod_name"`
	Title     string `db:"title"`
	Year      string `db:"year"`
	Area      string `db:"area"`
	Lang      string `db:"lang"`
	Director  string `db:"director"`
	Actor     string `db:"actor"`
	Tag       string `db:"tag"`
	Content   string `db:"content"`
	Pic       string `db:"pic"`
	CreatedAt string `db:"created_at"`
	UpdatedAt string `db:"updated_at"`
}

// DoubanInfoRow 全局豆瓣信息表行（通过 global_id 关联 global_video）
type DoubanInfoRow struct {
	Id           int    `db:"id"`
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
	CreatedAt    string `db:"created_at"`
	// JOIN 字段（查询时填充）
	VodName string `db:"vod_name"`
}

// UpsertGlobalVideo 插入或更新全局视频记录（按 vod_name 唯一）
// 返回 global_id
func UpsertGlobalVideo(v *model.Video) (int64, error) {
	if v == nil || v.VodName == "" {
		return 0, fmt.Errorf("vod_name is empty")
	}

	// 先查是否已存在
	existing, err := GetGlobalVideoByName(v.VodName)
	if err == nil && existing != nil {
		// 合并非空字段
		updated := false
		row := *existing
		if v.VodPic != "" && row.Pic == "" {
			row.Pic = v.VodPic
			updated = true
		}
		if v.VodYear != "" && row.Year == "" {
			row.Year = v.VodYear
			updated = true
		}
		if v.VodArea != "" && row.Area == "" {
			row.Area = v.VodArea
			updated = true
		}
		if v.VodLang != "" && row.Lang == "" {
			row.Lang = v.VodLang
			updated = true
		}
		if v.VodDirector != "" && row.Director == "" {
			row.Director = v.VodDirector
			updated = true
		}
		if v.VodActor != "" && row.Actor == "" {
			row.Actor = v.VodActor
			updated = true
		}
		if v.VodTag != "" && row.Tag == "" {
			row.Tag = v.VodTag
			updated = true
		}
		if v.VodContent != "" && row.Content == "" {
			row.Content = v.VodContent
			updated = true
		}
		if updated {
			_, err := instance.Exec(`UPDATE global_video SET pic=?, year=?, area=?, lang=?, director=?, actor=?, tag=?, content=?, updated_at=CURRENT_TIMESTAMP WHERE id=?`,
				row.Pic, row.Year, row.Area, row.Lang, row.Director, row.Actor, row.Tag, row.Content, row.Id)
			if err != nil {
				applog.Error("[Douban] Failed to update global_video for '%s': %v", v.VodName, err)
			}
		}
		return int64(existing.Id), nil
	}

	// 不存在，插入
	res, err := instance.Exec(`INSERT INTO global_video (vod_name, pic, year, area, lang, director, actor, tag, content, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		v.VodName, v.VodPic, v.VodYear, v.VodArea, v.VodLang, v.VodDirector, v.VodActor, v.VodTag, v.VodContent)
	if err != nil {
		applog.Error("[Douban] Failed to insert global_video for '%s': %v", v.VodName, err)
		return 0, err
	}
	id, _ := res.LastInsertId()
	return id, nil
}

// GetGlobalVideoByName 按 vod_name 查询全局视频
func GetGlobalVideoByName(vodName string) (*GlobalVideoRow, error) {
	var row GlobalVideoRow
	err := instance.Get(&row, `SELECT * FROM global_video WHERE vod_name = ?`, vodName)
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

// UpsertDoubanInfo 插入或更新全局豆瓣信息（按 global_id 唯一）
func UpsertDoubanInfo(info *DoubanInfoRow) error {
	if info == nil || info.GlobalID <= 0 {
		return fmt.Errorf("global_id is empty")
	}

	q := `INSERT INTO douban_info (
		global_id, subject_id, rating, votes, director, writer, actor,
		genre, country, language, release_date, season_count,
		episode_count, duration, aka, imdb, poster_url, updated_at
	) VALUES (
		:global_id, :subject_id, :rating, :votes, :director, :writer, :actor,
		:genre, :country, :language, :release_date, :season_count,
		:episode_count, :duration, :aka, :imdb, :poster_url, CURRENT_TIMESTAMP
	) ON CONFLICT(global_id) DO UPDATE SET
		subject_id = CASE WHEN excluded.subject_id != '' THEN excluded.subject_id ELSE douban_info.subject_id END,
		rating = CASE WHEN excluded.rating != '' THEN excluded.rating ELSE douban_info.rating END,
		votes = CASE WHEN excluded.votes != '' THEN excluded.votes ELSE douban_info.votes END,
		director = CASE WHEN excluded.director != '' THEN excluded.director ELSE douban_info.director END,
		writer = CASE WHEN excluded.writer != '' THEN excluded.writer ELSE douban_info.writer END,
		actor = CASE WHEN excluded.actor != '' THEN excluded.actor ELSE douban_info.actor END,
		genre = CASE WHEN excluded.genre != '' THEN excluded.genre ELSE douban_info.genre END,
		country = CASE WHEN excluded.country != '' THEN excluded.country ELSE douban_info.country END,
		language = CASE WHEN excluded.language != '' THEN excluded.language ELSE douban_info.language END,
		release_date = CASE WHEN excluded.release_date != '' THEN excluded.release_date ELSE douban_info.release_date END,
		season_count = CASE WHEN excluded.season_count != '' THEN excluded.season_count ELSE douban_info.season_count END,
		episode_count = CASE WHEN excluded.episode_count != '' THEN excluded.episode_count ELSE douban_info.episode_count END,
		duration = CASE WHEN excluded.duration != '' THEN excluded.duration ELSE douban_info.duration END,
		aka = CASE WHEN excluded.aka != '' THEN excluded.aka ELSE douban_info.aka END,
		imdb = CASE WHEN excluded.imdb != '' THEN excluded.imdb ELSE douban_info.imdb END,
		poster_url = CASE WHEN excluded.poster_url != '' THEN excluded.poster_url ELSE douban_info.poster_url END,
		updated_at = CURRENT_TIMESTAMP`

	_, err := instance.NamedExec(q, info)
	if err != nil {
		applog.Error("[Douban] UpsertDoubanInfo failed for global_id=%d: %v", info.GlobalID, err)
	}
	return err
}

// GetDoubanInfoByGlobalID 按 global_id 查询全局豆瓣信息
func GetDoubanInfoByGlobalID(globalID int) (*DoubanInfoRow, error) {
	var row DoubanInfoRow
	err := instance.Get(&row, `SELECT d.*, g.vod_name FROM douban_info d JOIN global_video g ON d.global_id = g.id WHERE d.global_id = ?`, globalID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetDoubanInfoByVodName 通过 vod_name → global_video → douban_info 查询
func GetDoubanInfoByVodName(vodName string) (*DoubanInfoRow, error) {
	var row DoubanInfoRow
	err := instance.Get(&row, `SELECT d.*, g.vod_name FROM douban_info d
		JOIN global_video g ON d.global_id = g.id
		WHERE g.vod_name = ? LIMIT 1`, vodName)
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
	err = instance.Select(&rows, `SELECT d.*, g.vod_name FROM douban_info d
		JOIN global_video g ON d.global_id = g.id
		WHERE g.vod_name LIKE ? LIMIT 1`, "%"+keyword+"%")
	if err != nil || len(rows) == 0 {
		return nil, sql.ErrNoRows
	}
	return &rows[0], nil
}

// GetIncompleteDoubanInfo 获取豆瓣信息不完整的记录（有 subject_id 但缺评分/导演且30分钟内未更新）
func GetIncompleteDoubanInfo(limit int) ([]*DoubanInfoRow, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*DoubanInfoRow
	q := `SELECT d.*, g.vod_name FROM douban_info d
		JOIN global_video g ON d.global_id = g.id
		WHERE d.subject_id != ''
		AND (d.rating = '' OR d.director = '' OR d.actor = '')
		AND (d.updated_at < ? OR d.updated_at IS NULL)
		ORDER BY d.updated_at ASC LIMIT ?`
	err := instance.Select(&rows, q, time.Now().Add(-30*time.Minute).Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		applog.Error("[Douban] GetIncompleteDoubanInfo query failed: %v", err)
		return nil, err
	}
	applog.Debug("[Douban] GetIncompleteDoubanInfo found %d records (limit=%d)", len(rows), limit)
	return rows, nil
}

// GetDoubanInfoMissingSubjectID 获取 subject_id 为空的记录（需要先搜索豆瓣找到 subject_id）
func GetDoubanInfoMissingSubjectID(limit int) ([]*DoubanInfoRow, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*DoubanInfoRow
	q := `SELECT d.*, g.vod_name FROM douban_info d
		JOIN global_video g ON d.global_id = g.id
		WHERE d.subject_id = ''
		ORDER BY d.id ASC LIMIT ?`
	err := instance.Select(&rows, q, limit)
	if err != nil {
		applog.Error("[Douban] GetDoubanInfoMissingSubjectID query failed: %v", err)
		return nil, err
	}
	applog.Debug("[Douban] GetDoubanInfoMissingSubjectID found %d records (limit=%d)", len(rows), limit)
	return rows, nil
}

// EnrichVideoWithDouban 用全局豆瓣信息和全局视频表补充 Video 对象
func EnrichVideoWithDouban(v *model.Video) {
	if v == nil || v.VodName == "" {
		return
	}

	// 先从全局视频表获取补充信息
	if globalVid, err := GetGlobalVideoByName(v.VodName); err == nil && globalVid != nil {
		if v.VodPic == "" && globalVid.Pic != "" {
			v.VodPic = globalVid.Pic
		}
		if v.VodYear == "" && globalVid.Year != "" {
			v.VodYear = globalVid.Year
		}
		if v.VodArea == "" && globalVid.Area != "" {
			v.VodArea = globalVid.Area
		}
		if v.VodLang == "" && globalVid.Lang != "" {
			v.VodLang = globalVid.Lang
		}
		if v.VodDirector == "" && globalVid.Director != "" {
			v.VodDirector = globalVid.Director
		}
		if v.VodActor == "" && globalVid.Actor != "" {
			v.VodActor = globalVid.Actor
		}
		if v.VodTag == "" && globalVid.Tag != "" {
			v.VodTag = globalVid.Tag
		}
		if v.VodContent == "" && globalVid.Content != "" {
			v.VodContent = globalVid.Content
		}
	}

	row, err := GetDoubanInfoByKeyword(v.VodName)
	if err != nil {
		return
	}

	// 只有源数据中没有的字段才用全局豆瓣数据补充
	if v.VodDoubanId.String() == "" && row.SubjectID != "" {
		v.VodDoubanId = model.FlexibleString(row.SubjectID)
	}
	if v.VodDoubanScore.String() == "" && row.Rating != "" {
		v.VodDoubanScore = model.FlexibleString(row.Rating)
	}
	if v.VodDirector == "" && row.Director != "" {
		v.VodDirector = row.Director
	}
	if v.VodActor == "" && row.Actor != "" {
		v.VodActor = row.Actor
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
	if v.VodPic == "" && row.PosterURL != "" {
		v.VodPic = row.PosterURL
	}
	if v.VodYear == "" && row.ReleaseDate != "" {
		releaseDate := strings.TrimSpace(row.ReleaseDate)
		if len(releaseDate) >= 4 {
			v.VodYear = releaseDate[:4]
		}
	}
}

// SaveDoubanInfoFromVideo 从源视频中提取豆瓣信息存入全局表
// 流程：vod_name → global_video → douban_info
func SaveDoubanInfoFromVideo(v *model.Video) {
	if v == nil || v.VodName == "" {
		return
	}

	// 1. 先确保 global_video 中存在该视频（同时返回 global_id）
	globalID, err := UpsertGlobalVideo(v)
	if err != nil {
		applog.Error("[Douban] Failed to upsert global_video for '%s': %v", v.VodName, err)
		return
	}

	subjectID := v.VodDoubanId.String()

	// 2. 检查 douban_info 是否已存在
	existing, err := GetDoubanInfoByGlobalID(int(globalID))
	if err != nil {
		// 不存在，创建新记录（subject_id 可为空）
		row := &DoubanInfoRow{
			GlobalID:  int(globalID),
			SubjectID: subjectID,
			Rating:    v.VodDoubanScore.String(),
			Director:  v.VodDirector,
			Actor:     v.VodActor,
			Country:   v.VodArea,
			Language:  v.VodLang,
			Genre:     v.VodTag,
			Aka:       v.VodSub,
			PosterURL: v.VodPic,
		}
		if v.VodRemarks != "" {
			row.EpisodeCount = v.VodRemarks
		}
		if v.VodYear != "" {
			row.ReleaseDate = v.VodYear
		}
		if err := UpsertDoubanInfo(row); err != nil {
			applog.Error("[Douban] Failed to insert douban_info for '%s': %v", v.VodName, err)
		} else {
			if subjectID != "" || v.VodDoubanScore.String() != "" {
				applog.Info("[Douban] Created douban_info for '%s': subject_id=%s, rating=%s", v.VodName, subjectID, v.VodDoubanScore.String())
			} else {
				applog.Debug("[Douban] Created placeholder douban_info for '%s' (no douban data yet)", v.VodName)
			}
		}
		return
	}

	// 3. 已存在，补充缺失字段
	updated := *existing
	updatedFields := false

	if updated.SubjectID == "" && subjectID != "" {
		updated.SubjectID = subjectID
		updatedFields = true
	}
	if updated.Rating == "" && v.VodDoubanScore.String() != "" {
		updated.Rating = v.VodDoubanScore.String()
		updatedFields = true
	}
	if updated.Director == "" && v.VodDirector != "" {
		updated.Director = v.VodDirector
		updatedFields = true
	}
	if updated.Actor == "" && v.VodActor != "" {
		updated.Actor = v.VodActor
		updatedFields = true
	}
	if updated.Country == "" && v.VodArea != "" {
		updated.Country = v.VodArea
		updatedFields = true
	}
	if updated.Language == "" && v.VodLang != "" {
		updated.Language = v.VodLang
		updatedFields = true
	}
	if updated.Genre == "" && v.VodTag != "" {
		updated.Genre = v.VodTag
		updatedFields = true
	}
	if updated.PosterURL == "" && v.VodPic != "" {
		updated.PosterURL = v.VodPic
		updatedFields = true
	}

	if updatedFields {
		if err := UpsertDoubanInfo(&updated); err != nil {
			applog.Error("[Douban] Failed to update douban_info for '%s': %v", v.VodName, err)
		}
	}
}

// SaveDoubanInfoFromBatch 批量保存视频的豆瓣信息到全局表
func SaveDoubanInfoFromBatch(videos []*model.Video) {
	if len(videos) == 0 {
		return
	}
	applog.Debug("[Douban] Saving %d videos to global_video + douban_info", len(videos))
	for _, v := range videos {
		SaveDoubanInfoFromVideo(v)
	}
}

// GetAllDoubanInfo 获取所有全局豆瓣信息（JOIN global_video）
func GetAllDoubanInfo() ([]*DoubanInfoRow, error) {
	var rows []*DoubanInfoRow
	err := instance.Select(&rows, `SELECT d.*, g.vod_name FROM douban_info d
		JOIN global_video g ON d.global_id = g.id ORDER BY d.id DESC`)
	return rows, err
}

// MarkDoubanInfoUpdated 标记某条记录的更新时间
func MarkDoubanInfoUpdated(globalID int) error {
	_, err := instance.Exec(`UPDATE douban_info SET updated_at = CURRENT_TIMESTAMP WHERE global_id = ?`, globalID)
	return err
}

// GetOrCreateGlobalID 根据 vod_name 获取或创建 global_video 记录，返回 global_id
func GetOrCreateGlobalID(vodName string) (int64, error) {
	row, err := GetGlobalVideoByName(vodName)
	if err == nil {
		return int64(row.Id), nil
	}
	// 不存在则创建占位符记录
	res, err := instance.Exec(`INSERT INTO global_video (vod_name, updated_at) VALUES (?, CURRENT_TIMESTAMP)`, vodName)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// --- 错误 ---
var ErrDoubanNotFound = errors.New("douban info not found")