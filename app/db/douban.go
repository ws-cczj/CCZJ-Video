package db

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"cczjVideo/app/util"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// GlobalVideoRow 全局视频表行（合并原 douban_info，包含所有共享元数据）
type GlobalVideoRow struct {
	Id                  int    `db:"id"`
	VodName             string `db:"vod_name"`
	TypeId              int    `db:"type_id"`
	Year                string `db:"year"`
	Area                string `db:"area"`
	Lang                string `db:"lang"`
	Director            string `db:"director"`
	Writer              string `db:"writer"`
	Actor               string `db:"actor"`
	Tag                 string `db:"tag"`
	Content             string `db:"content"`
	Pic                 string `db:"pic"`
	DoubanId            string `db:"douban_id"`
	DoubanScore         string `db:"douban_score"`
	DoubanVotes         string `db:"douban_votes"`
	Genre               string `db:"genre"`
	ReleaseDate         string `db:"release_date"`
	Duration            string `db:"duration"`
	Aka                 string `db:"aka"`
	Imdb                string `db:"imdb"`
	SeasonCount         string `db:"season_count"`
	EpisodeCount        string `db:"episode_count"`
	DoubanCooldownUntil  *string `db:"douban_cooldown_until"`
	DoubanSearchFailures int    `db:"douban_search_failures"`
	CreatedAt            string `db:"created_at"`
	UpdatedAt            string `db:"updated_at"`
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

// RepairDoubanIDs 启动时修复 global_video.douban_id 中格式异常的值
func RepairDoubanIDs() {
	rows, err := instance.Queryx(`SELECT id, douban_id FROM global_video WHERE douban_id != ''`)
	if err != nil {
		logError(fmt.Sprintf("RepairDoubanIDs: query failed: %v", err))
		return
	}
	defer rows.Close()

	fixed := 0
	cleared := 0
	for rows.Next() {
		var id int
		var sid string
		if err := rows.Scan(&id, &sid); err != nil {
			continue
		}
		cleaned := normalizeSubjectID(sid)
		if cleaned != sid {
			if cleaned == "" {
				_, _ = instance.Exec(`UPDATE global_video SET douban_id = '' WHERE id = ?`, id)
				cleared++
			} else {
				_, _ = instance.Exec(`UPDATE global_video SET douban_id = ? WHERE id = ?`, cleaned, id)
				fixed++
			}
		}
	}
	if fixed > 0 || cleared > 0 {
		logInfo(fmt.Sprintf("[Douban] RepairDoubanIDs: fixed %d, cleared %d invalid douban_ids", fixed, cleared))
	}
}

// UpsertGlobalVideo 插入或更新全局视频记录（通过智能匹配避免重复）
func UpsertGlobalVideo(v *model.Video) (int64, error) {
	if v == nil || v.VodName == "" {
		return 0, fmt.Errorf("vod_name is empty")
	}

	// 传入元数据前先解压缩，否则模糊匹配中的 metadataMatch 无法正确比对
	director := util.DecompressIfNeeded(v.VodDirector)
	actor := util.DecompressIfNeeded(v.VodActor)

	// 解析类型ID
	typeId := resolveGlobalTypeIdInt(v.TypeName)

	// 使用智能匹配获取 global_id（精确→归一化→模糊+元数据）
	globalID, err := GetOrCreateGlobalIDWithMeta(v.VodName, string(v.VodYear), director, actor, typeId)
	if err != nil {
		return 0, err
	}

	// 合并更新非空字段（写入 global_video 的字段保持明文，不压缩）
	content := util.DecompressIfNeeded(v.VodContent)
	_, err = instance.Exec(`UPDATE global_video SET
		type_id=CASE WHEN ? != 0 THEN ? ELSE type_id END,
		pic=CASE WHEN ? != '' THEN ? ELSE pic END,
		year=CASE WHEN ? != '' THEN ? ELSE year END,
		area=CASE WHEN ? != '' THEN ? ELSE area END,
		lang=CASE WHEN ? != '' THEN ? ELSE lang END,
		director=CASE WHEN ? != '' THEN ? ELSE director END,
		actor=CASE WHEN ? != '' THEN ? ELSE actor END,
		tag=CASE WHEN ? != '' THEN ? ELSE tag END,
		content=CASE WHEN ? != '' THEN ? ELSE content END,
		updated_at=CURRENT_TIMESTAMP
		WHERE id = ?`,
		typeId, typeId,
		v.VodPic, v.VodPic, v.VodYear, v.VodYear, v.VodArea, v.VodArea,
		v.VodLang, v.VodLang, director, director, actor, actor,
		v.VodTag, v.VodTag, content, content, globalID)
	if err != nil {
		applog.Error("[Douban] Failed to update global_video for '%s': %v", v.VodName, err)
		return 0, err
	}
	return globalID, nil
}

// GetGlobalVideoByName 按 vod_name 查询全局视频
func GetGlobalVideoByName(vodName string) (*GlobalVideoRow, error) {
	var row GlobalVideoRow
	err := instance.Get(&row, `SELECT id, vod_name, type_id, year, area, lang, director, writer, actor, tag, content, pic, douban_id, douban_score, douban_votes, genre, release_date, duration, aka, imdb, season_count, episode_count, douban_cooldown_until, douban_search_failures, created_at, updated_at FROM global_video WHERE vod_name = ? LIMIT 1`, vodName)
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
		director = CASE WHEN ? != '' THEN ? ELSE director END,
		writer = CASE WHEN ? != '' THEN ? ELSE writer END,
		actor = CASE WHEN ? != '' THEN ? ELSE actor END,
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
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`

	_, err := instance.Exec(q,
		info.SubjectID, info.SubjectID,
		info.Rating, info.Rating,
		info.Votes, info.Votes,
		info.Director, info.Director,
		info.Writer, info.Writer,
		info.Actor, info.Actor,
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
		director, writer, actor, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video WHERE id = ?`, globalID)
	if err != nil {
		return nil, err
	}
	return &row, nil
}

// GetDoubanInfoByVodName 通过 vod_name 查询豆瓣信息
func GetDoubanInfoByVodName(vodName string) (*DoubanInfoRow, error) {
	var row DoubanInfoRow
	err := instance.Get(&row, `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		director, writer, actor, genre,
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
		director, writer, actor, genre,
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
func GetIncompleteDoubanInfo(limit int) ([]*DoubanInfoRow, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*DoubanInfoRow
	q := `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		director, writer, actor, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video
		WHERE douban_id != '' AND douban_id != '0'
		AND (douban_score = '' OR director = '' OR actor = '')
		AND (douban_cooldown_until IS NULL OR douban_cooldown_until < ?)
		ORDER BY updated_at ASC LIMIT ?`
	err := instance.Select(&rows, q,
		time.Now().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		applog.Error("[Douban] GetIncompleteDoubanInfo query failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// GetDoubanInfoMissingSubjectID 获取 douban_id 为空的记录（排除冷却期内的记录）
func GetDoubanInfoMissingSubjectID(limit int) ([]*DoubanInfoRow, error) {
	if limit <= 0 {
		limit = 5
	}
	var rows []*DoubanInfoRow
	q := `SELECT
		id AS global_id,
		douban_id AS subject_id, douban_score AS rating, douban_votes AS votes,
		director, writer, actor, genre,
		area AS country, lang AS language,
		release_date, season_count, episode_count, duration,
		aka, imdb, pic AS poster_url, updated_at, vod_name
		FROM global_video
		WHERE (douban_id = '' OR douban_id IS NULL)
		AND (douban_cooldown_until IS NULL OR douban_cooldown_until < ?)
		ORDER BY id ASC LIMIT ?`
	err := instance.Select(&rows, q,
		time.Now().Format("2006-01-02 15:04:05"), limit)
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
			pic, director, actor, area, lang, tag AS genre,
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
		Director     string `db:"director"`
		Actor        string `db:"actor"`
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
		if v.VodDirector == "" && r.Director != "" {
			v.VodDirector = r.Director
		}
		if v.VodActor == "" && r.Actor != "" {
			v.VodActor = r.Actor
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

// SaveDoubanInfoFromVideo 从源视频中提取豆瓣信息存入 global_video
func SaveDoubanInfoFromVideo(v *model.Video) {
	if v == nil || v.VodName == "" {
		return
	}

	globalID, err := UpsertGlobalVideo(v)
	if err != nil {
		applog.Error("[Douban] Failed to upsert global_video for '%s': %v", v.VodName, err)
		return
	}

	subjectID := normalizeSubjectID(v.VodDoubanId.String())

	// 更新 global_video 中的豆瓣相关字段
	_, err = instance.Exec(`UPDATE global_video SET
		douban_id = CASE WHEN ? != '' THEN ? ELSE douban_id END,
		douban_score = CASE WHEN ? != '' THEN ? ELSE douban_score END,
		genre = CASE WHEN ? != '' THEN ? ELSE genre END,
		area = CASE WHEN ? != '' THEN ? ELSE area END,
		lang = CASE WHEN ? != '' THEN ? ELSE lang END,
		aka = CASE WHEN ? != '' THEN ? ELSE aka END,
		pic = CASE WHEN ? != '' THEN ? ELSE pic END,
		release_date = CASE WHEN ? != '' THEN ? ELSE release_date END,
		updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		subjectID, subjectID,
		v.VodDoubanScore.String(), v.VodDoubanScore.String(),
		v.VodTag, v.VodTag,
		v.VodArea, v.VodArea,
		v.VodLang, v.VodLang,
		v.VodSub, v.VodSub,
		v.VodPic, v.VodPic,
		v.VodYear, v.VodYear,
		globalID)
	if err != nil {
		applog.Error("[Douban] Failed to update douban fields for '%s': %v", v.VodName, err)
	}
}

// SaveDoubanInfoFromBatch 批量保存视频的豆瓣信息到全局表
func SaveDoubanInfoFromBatch(videos []*model.Video) {
	if len(videos) == 0 {
		return
	}
	for _, v := range videos {
		SaveDoubanInfoFromVideo(v)
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
		director, writer, actor, genre,
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

// normalizeForCompare 去除所有 Unicode 空白字符 + 全角转半角 + 转小写，用于名称比对
// 注意：此函数的逻辑必须与 SQL 索引 idx_gv_name_norm 和 sqlNorm() 完全一致
func normalizeForCompare(s string) string {
	s = removeAllWhitespace(s)
	s = normalizeFullWidth(s)
	return strings.ToLower(s)
}

// removeAllWhitespace 去除所有 Unicode 空白字符（包括全角空格、不间断空格、零宽空格）
func removeAllWhitespace(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) && r != '\u200B' && r != '\u00A0' {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// normalizeFullWidth 全角标点转半角
func normalizeFullWidth(s string) string {
	s = strings.ReplaceAll(s, "\uFF1A", ":")  // ：→ :
	s = strings.ReplaceAll(s, "\uFF08", "(")  // （→ (
	s = strings.ReplaceAll(s, "\uFF09", ")")  // ）→ )
	s = strings.ReplaceAll(s, "\uFF01", "!")  // ！→ !
	s = strings.ReplaceAll(s, "\uFF1F", "?")  // ？→ ?
	s = strings.ReplaceAll(s, "\u3000", "")   // 全角空格去除
	return s
}

// sqlNormExpr 返回与 normalizeForCompare 完全一致的 SQL 归一化表达式
// 用于所有涉及 vod_name 归一化的 SQL 查询，确保与索引定义一致
func sqlNormExpr() string {
	return `LOWER(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(REPLACE(vod_name, ' ', ''), char(9), ''), char(10), ''), char(13), ''), char(12288), ''), char(160), ''), char(65306), ':'), char(65288), '('), char(65289), ')'))`
}

// sqlNorm 对字符串执行与 SQL 索引相同的归一化（用于 Go 侧计算）
func sqlNorm(s string) string {
	s = removeAllWhitespace(s)
	s = normalizeFullWidth(s)
	return strings.ToLower(s)
}

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

// nameSimilarity 计算两个名称的相似度 0.0~1.0
// 去空格+小写后计算编辑距离
func nameSimilarity(a, b string) float64 {
	na := normalizeForCompare(a)
	nb := normalizeForCompare(b)
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
	na := normalizeForCompare(a)
	nb := normalizeForCompare(b)
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

// GetOrCreateGlobalID 根据 vod_name 获取或创建 global_video 记录，返回 global_id
// 匹配策略：精确 → 去空格 → 90%相似度+元数据交叉验证 → 创建新条目
// selectGlobalID 只查 id + LIMIT 1，避免 SELECT * 因列数/行数不匹配失败
func selectGlobalID(whereClause string, args ...interface{}) (int64, error) {
	var id int64
	err := instance.Get(&id, fmt.Sprintf(`SELECT id FROM global_video WHERE %s LIMIT 1`, whereClause), args...)
	if err != nil {
		return 0, err
	}
	return id, nil
}

// globalVideoIDAndName 只查 id 和 vod_name，避免 SELECT * 因列不匹配失败
func globalVideoIDAndName(whereClause string, args ...interface{}) (int64, string, error) {
	type pair struct {
		ID      int64  `db:"id"`
		VodName string `db:"vod_name"`
	}
	var p pair
	err := instance.Get(&p, fmt.Sprintf(`SELECT id, vod_name FROM global_video WHERE %s LIMIT 1`, whereClause), args...)
	if err != nil {
		return 0, "", err
	}
	return p.ID, p.VodName, nil
}

// globalVideoIDAndNameWithType 查询 id, vod_name, type_id
func globalVideoIDAndNameWithType(whereClause string, args ...interface{}) (int64, string, int64, error) {
	type triplet struct {
		ID      int64  `db:"id"`
		VodName string `db:"vod_name"`
		TypeId  int64  `db:"type_id"`
	}
	var t triplet
	err := instance.Get(&t, fmt.Sprintf(`SELECT id, vod_name, type_id FROM global_video WHERE %s LIMIT 1`, whereClause), args...)
	if err != nil {
		return 0, "", 0, err
	}
	return t.ID, t.VodName, t.TypeId, nil
}

// selectAllGlobalCandidates 查询所有候选行（仅取 id、vod_name 和元数据字段）
// 不做长度预过滤，由 nameSimilarity + metadataMatch 完成精确匹配
func selectAllGlobalCandidates() ([]GlobalVideoRow, error) {
	var rows []struct {
		Id       int    `db:"id"`
		VodName  string `db:"vod_name"`
		TypeId   int    `db:"type_id"`
		Year     string `db:"year"`
		Director string `db:"director"`
		Actor    string `db:"actor"`
	}
	err := instance.Select(&rows, `SELECT id, vod_name, type_id, year, director, actor FROM global_video`)
	if err != nil {
		return nil, err
	}
	result := make([]GlobalVideoRow, len(rows))
	for i, r := range rows {
		result[i] = GlobalVideoRow{Id: r.Id, VodName: r.VodName, TypeId: r.TypeId, Year: r.Year, Director: r.Director, Actor: r.Actor}
	}
	return result, nil
}

func GetOrCreateGlobalID(vodName string, typeId int64) (int64, error) {
	// 1. 精确匹配
	row, err := GetGlobalVideoByName(vodName)
	if err == nil {
		if typeId == 0 || int64(row.TypeId) == typeId {
			return int64(row.Id), nil
		}
		// 类型不匹配，继续尝试
	}

	// 2. 归一化匹配（去除所有空白字符 + 小写）
	normalized := sqlNorm(vodName)
	if typeId > 0 {
		id, _, _, err := globalVideoIDAndNameWithType(
			fmt.Sprintf(`%s = ? AND type_id = ?`, sqlNormExpr()), normalized, typeId)
		if err == nil {
			applog.Info("[global] 归一化匹配: %q -> global_id=%d (type_id=%d)", vodName, id, typeId)
			return id, nil
		}
	} else {
		id, normName, err := globalVideoIDAndName(
			fmt.Sprintf(`%s = ?`, sqlNormExpr()), normalized)
		if err == nil {
			applog.Info("[global] 归一化匹配: %q -> global_id=%d (原名: %q)", vodName, id, normName)
			return id, nil
		}
	}

	// 3. 创建新条目
	normVal := sqlNorm(vodName)
	_, _ = instance.Exec(`INSERT OR IGNORE INTO global_video (vod_name, type_id, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, vodName, typeId)
	// 通过归一化 SELECT 获取 ID
	if typeId > 0 {
		if id, _, _, e := globalVideoIDAndNameWithType(
			fmt.Sprintf(`%s = ? AND type_id = ?`, sqlNormExpr()), normVal, typeId); e == nil && id > 0 {
			return id, nil
		}
	} else {
		if id, _, e := globalVideoIDAndName(
			fmt.Sprintf(`%s = ?`, sqlNormExpr()), normVal); e == nil && id > 0 {
			return id, nil
		}
	}
	// 回退：精确名称查询
	if id, e := selectGlobalID(`vod_name = ?`, vodName); e == nil && id > 0 {
		return id, nil
	}
	// 最终回退
	res, insertErr := instance.Exec(`INSERT OR IGNORE INTO global_video (vod_name, type_id, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, vodName, typeId)
	if insertErr == nil {
		if lid, err := res.LastInsertId(); err == nil && lid > 0 {
			return lid, nil
		}
	}
	return 0, fmt.Errorf("INSERT OR IGNORE 后仍未找到条目: %s", vodName)
}

// GetOrCreateGlobalIDWithMeta 带元数据的智能匹配，用于采集入库时
// 当名称 90%+ 相似且元数据（年份+导演/演员+类型）吻合时，视为同一视频
// 注意：传入的 director/actor 应为明文（调用方负责解压缩）
func GetOrCreateGlobalIDWithMeta(vodName, year, director, actor string, typeId int64) (int64, error) {
	// 1. 精确匹配
	row, err := GetGlobalVideoByName(vodName)
	if err == nil {
		if int64(row.TypeId) == typeId {
			return int64(row.Id), nil
		}
		// ⭐ 类型不匹配：同名但不同类型，视为不同视频
		applog.Info("[global] 精确匹配命中但类型不匹配: %q (现有type_id=%d, 新type_id=%d), 跳过", vodName, row.TypeId, typeId)
	}

	// 2. 归一化匹配（去除所有空白字符 + 小写）
	normalized := sqlNorm(vodName)
	id, normName, normTypeId, err := globalVideoIDAndNameWithType(
		fmt.Sprintf(`%s = ?`, sqlNormExpr()), normalized)
	if err == nil {
		if normTypeId == typeId {
			applog.Info("[global] 归一化匹配: %q -> global_id=%d (%q, type_id=%d)", vodName, id, normName, normTypeId)
			return id, nil
		}
		applog.Info("[global] 归一化匹配命中但类型不匹配: %q (现有type_id=%d, 新type_id=%d), 跳过", vodName, normTypeId, typeId)
	}

	// 3. 模糊匹配（90%+ 相似度 + 元数据交叉验证），全表扫描不做长度预过滤
	candidates, _ := selectAllGlobalCandidates()

	var bestMatch *GlobalVideoRow
	var bestSim float64

	for i := range candidates {
		c := &candidates[i]
		// ⭐ 类型预检：类型不匹配的直接跳过
		if int64(c.TypeId) != typeId {
			continue
		}
		sim := nameSimilarity(vodName, c.VodName)
		if sim < 0.90 {
			continue
		}
		// 防止不同季/部/期被误判为同一视频（如"权力的游戏 第一季" vs "权力的游戏 第二季"）
		if hasSeasonSuffix(vodName, c.VodName) {
			continue
		}
		// 90%+ 相似度，检查元数据
		if metadataMatch(year, director, actor, c.Year, c.Director, c.Actor) {
			applog.Info("[global] 模糊+元数据匹配(%.0f%%): %q -> global_id=%d (%q, type_id=%d)",
				sim*100, vodName, c.Id, c.VodName, c.TypeId)
			return int64(c.Id), nil
		}
		// 95%+ 相似度但元数据不匹配（可能是同一视频但元数据不完整）
		if sim >= 0.95 && sim > bestSim {
			bestSim = sim
			bestMatch = c
		}
	}

	// 如果名称极度相似(≥95%)且没有元数据冲突，视为同一视频
	if bestMatch != nil {
		applog.Info("[global] 高相似度回退(%.0f%%): %q -> global_id=%d (%q, type_id=%d)",
			bestSim*100, vodName, bestMatch.Id, bestMatch.VodName, bestMatch.TypeId)
		return int64(bestMatch.Id), nil
	}

	// 4. 创建新条目（使用 INSERT OR IGNORE 避免 UNIQUE 冲突报错，包含 type_id）
	normVal := sqlNorm(vodName)
	_, _ = instance.Exec(`INSERT OR IGNORE INTO global_video (vod_name, type_id, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, vodName, typeId)
	// 通过归一化 + type_id 精确 SELECT 获取 ID
	if id, _, _, e := globalVideoIDAndNameWithType(
		fmt.Sprintf(`%s = ? AND type_id = ?`, sqlNormExpr()), normVal, typeId); e == nil && id > 0 {
		applog.Info("[global] 新建/已有条目: %q -> global_id=%d (type_id=%d)", vodName, id, typeId)
		return id, nil
	}
	// 回退：精确名称查询
	if id, e := selectGlobalID(`vod_name = ?`, vodName); e == nil && id > 0 {
		return id, nil
	}
	// 最终回退：重新 INSERT 并取 lastInsertId
	res, insertErr := instance.Exec(`INSERT OR IGNORE INTO global_video (vod_name, type_id, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)`, vodName, typeId)
	if insertErr == nil {
		if lid, err := res.LastInsertId(); err == nil && lid > 0 {
			applog.Info("[global] 新建条目(lastInsertId): %q -> global_id=%d (type_id=%d)", vodName, lid, typeId)
			return lid, nil
		}
	}
	return 0, fmt.Errorf("INSERT OR IGNORE 后仍未找到条目: %s", vodName)
}

// timeNowMinus30Min 返回30分钟前的时间字符串
func timeNowMinus30Min() string {
	return time.Now().Add(-30 * time.Minute).Format("2006-01-02 15:04:05")
}

// SetDoubanCooldown 为指定 global_id 设置24小时冷静期（用于搜索失败或详情无评分）
func SetDoubanCooldown(globalID int) error {
	cooldownUntil := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := instance.Exec(`UPDATE global_video SET douban_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		cooldownUntil, globalID)
	if err != nil {
		applog.Error("[Douban] SetDoubanCooldown failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// SetDoubanCooldownByVodName 通过 vod_name 设置冷静期
func SetDoubanCooldownByVodName(vodName string) error {
	cooldownUntil := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := instance.Exec(`UPDATE global_video SET douban_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE vod_name = ?`,
		cooldownUntil, vodName)
	if err != nil {
		applog.Error("[Douban] SetDoubanCooldownByVodName failed for '%s': %v", vodName, err)
	}
	return err
}

// IncrementSearchFailures 增加搜索失败计数，达到上限时设置冷静期
func IncrementSearchFailures(vodName string) error {
	// 使用智能匹配获取 global_id（避免直接 INSERT 导致唯一索引冲突）
	globalID, err := GetOrCreateGlobalID(vodName, 0)
	if err != nil {
		applog.Error("[Douban] IncrementSearchFailures GetOrCreateGlobalID failed for '%s': %v", vodName, err)
		return err
	}

	// 获取当前计数
	var currentFailures int
	_ = instance.Get(&currentFailures, `SELECT douban_search_failures FROM global_video WHERE id = ?`, globalID)

	newCount := currentFailures + 1
	if newCount >= 5 {
		cooldownUntil := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
		_, err = instance.Exec(`UPDATE global_video SET douban_search_failures = ?, douban_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			newCount, cooldownUntil, globalID)
		if err != nil {
			applog.Error("[Douban] IncrementSearchFailures cooldown failed for '%s': %v", vodName, err)
		}
		applog.Info("[Douban] Search cooldown activated for '%s' (failures=%d, until=%s)", vodName, newCount, cooldownUntil)
	} else {
		_, err = instance.Exec(`UPDATE global_video SET douban_search_failures = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			newCount, globalID)
		if err != nil {
			applog.Error("[Douban] IncrementSearchFailures update failed for '%s': %v", vodName, err)
		}
	}
	return err
}

// ClearSearchFailures 清除搜索失败记录（搜索成功时调用）
func ClearSearchFailures(vodName string) error {
	_, err := instance.Exec(`UPDATE global_video SET douban_search_failures = 0, douban_cooldown_until = NULL, updated_at = CURRENT_TIMESTAMP WHERE vod_name = ?`, vodName)
	if err != nil {
		applog.Error("[Douban] ClearSearchFailures failed for '%s': %v", vodName, err)
	}
	return err
}

// IsDoubanOnCooldown 检查指定 vod_name 是否在冷却期内
func IsDoubanOnCooldown(vodName string) bool {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return false
	}
	if row.DoubanCooldownUntil == nil {
		return false
	}
	cooldownTime, err := time.Parse("2006-01-02 15:04:05", *row.DoubanCooldownUntil)
	if err != nil {
		return false
	}
	return time.Now().Before(cooldownTime)
}

// ErrDoubanNotFound 错误
var ErrDoubanNotFound = errors.New("douban info not found")
