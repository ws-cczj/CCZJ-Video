package db

import (
	"cczjVideo/app/model"
	"cczjVideo/app/util"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func logInfo(msg string)  { safeLog("INFO", msg) }
func logWarn(msg string)  { safeLog("WARN", msg) }
func logError(msg string) { safeLog("ERROR", msg) }

// 兼容：如果没有初始化 applog 就不写，避免循环依赖
func safeLog(level, msg string) {
	defer func() { _ = recover() }()
	logMu.Lock()
	fn := logFn
	logMu.Unlock()
	if fn != nil {
		fn(level, msg)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// normalizeFlex 把 FlexibleString 规范化为字符串，空值返回 ""
func normalizeFlex(v model.FlexibleString) string {
	s := strings.TrimSpace(v.String())
	return s
}

func UpsertVideos(sourceKey string, videos []*model.Video) error {
	if len(videos) == 0 {
		return nil
	}
	if err := EnsureVideoTable(sourceKey); err != nil {
		return err
	}
	tn := VideoTableName(sourceKey)

	cols := []string{"vod_id", "type_id", "type_name", "vod_name", "global_id",
		"vod_class", "vod_remarks", "vod_pic", "vod_play_url", "vod_down_url", "vod_time",
		"vod_douban_id", "vod_douban_score", "vod_hits", "vod_hits_day", "vod_hits_week", "vod_hits_month",
		"vod_pubdate", "vod_version", "vod_state", "vod_score", "vod_score_all", "vod_score_num",
		"vod_isend", "vod_play_from", "vod_play_note", "vod_letter", "vod_sub", "vod_en"}

	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)
		ON CONFLICT(vod_id) DO UPDATE SET
		type_id=excluded.type_id, type_name=excluded.type_name,
		vod_name=excluded.vod_name, global_id=excluded.global_id,
		vod_class=excluded.vod_class, vod_remarks=excluded.vod_remarks,
		vod_pic=excluded.vod_pic,
		vod_play_url=excluded.vod_play_url, vod_down_url=excluded.vod_down_url,
		vod_time=excluded.vod_time,
		vod_douban_id=excluded.vod_douban_id, vod_douban_score=excluded.vod_douban_score,
		vod_hits=excluded.vod_hits, vod_hits_day=excluded.vod_hits_day,
		vod_hits_week=excluded.vod_hits_week, vod_hits_month=excluded.vod_hits_month,
		vod_pubdate=excluded.vod_pubdate, vod_version=excluded.vod_version,
		vod_state=excluded.vod_state, vod_score=excluded.vod_score,
		vod_score_all=excluded.vod_score_all, vod_score_num=excluded.vod_score_num,
		vod_isend=excluded.vod_isend, vod_play_from=excluded.vod_play_from,
		vod_play_note=excluded.vod_play_note, vod_letter=excluded.vod_letter,
		vod_sub=excluded.vod_sub, vod_en=excluded.vod_en`,
		tn, strings.Join(cols, ","),
		":"+strings.Join(cols, ",:"))

	type row struct {
		VodId          string `db:"vod_id"`
		TypeId         string `db:"type_id"`
		TypeName       string `db:"type_name"`
		VodName        string `db:"vod_name"`
		GlobalId       int64  `db:"global_id"`
		VodClass       string `db:"vod_class"`
		VodRemarks     string `db:"vod_remarks"`
		VodPic         string `db:"vod_pic"`
		VodPlayUrl     string `db:"vod_play_url"`
		VodDownUrl     string `db:"vod_down_url"`
		VodTime        string `db:"vod_time"`
		VodDoubanId    string `db:"vod_douban_id"`
		VodDoubanScore string `db:"vod_douban_score"`
		VodHits        string `db:"vod_hits"`
		VodHitsDay     string `db:"vod_hits_day"`
		VodHitsWeek    string `db:"vod_hits_week"`
		VodHitsMonth   string `db:"vod_hits_month"`
		VodPubdate     string `db:"vod_pubdate"`
		VodVersion     string `db:"vod_version"`
		VodState       string `db:"vod_state"`
		VodScore       string `db:"vod_score"`
		VodScoreAll    string `db:"vod_score_all"`
		VodScoreNum    string `db:"vod_score_num"`
		VodIsEnd       string `db:"vod_isend"`
		VodPlayFrom    string `db:"vod_play_from"`
		VodPlayNote    string `db:"vod_play_note"`
		VodLetter      string `db:"vod_letter"`
		VodSub         string `db:"vod_sub"`
		VodEn          string `db:"vod_en"`
	}
	var rows []*row
	var skipped int
	for _, v := range videos {
		if v == nil {
			continue
		}
		vid := normalizeFlex(v.VodId)
		if vid == "" {
			skipped++
			continue
		}

		globalID, err := upsertGlobalVideo(v.VodName, v.VodYear, v.VodArea, v.VodLang,
			v.VodDirector, v.VodActor, v.VodTag, v.VodContent, v.VodPic)
		if err != nil {
			logWarn(fmt.Sprintf("UpsertVideos[%s] upsert global_video failed for '%s': %v", sourceKey, v.VodName, err))
			globalID = 0
		}

		rows = append(rows, &row{
			VodId:          vid,
			TypeId:         normalizeFlex(v.TypeId),
			TypeName:       v.TypeName,
			VodName:        v.VodName,
			GlobalId:       globalID,
			VodClass:       v.VodClass,
			VodRemarks:     v.VodRemarks,
			VodPic:         v.VodPic,
			VodPlayUrl:     v.VodPlayUrl,
			VodDownUrl:     v.VodDownUrl,
			VodTime:        v.VodTime,
			VodDoubanId:    normalizeFlex(v.VodDoubanId),
			VodDoubanScore: normalizeFlex(v.VodDoubanScore),
			VodHits:        normalizeFlex(v.VodHits),
			VodHitsDay:     normalizeFlex(v.VodHitsDay),
			VodHitsWeek:    normalizeFlex(v.VodHitsWeek),
			VodHitsMonth:   normalizeFlex(v.VodHitsMonth),
			VodPubdate:     normalizeFlex(v.VodPubdate),
			VodVersion:     normalizeFlex(v.VodVersion),
			VodState:       normalizeFlex(v.VodState),
			VodScore:       normalizeFlex(v.VodScore),
			VodScoreAll:    normalizeFlex(v.VodScoreAll),
			VodScoreNum:    normalizeFlex(v.VodScoreNum),
			VodIsEnd:       normalizeFlex(v.VodIsEnd),
			VodPlayFrom:    v.VodPlayFrom,
			VodPlayNote:    v.VodPlayNote,
			VodLetter:      v.VodLetter,
			VodSub:         v.VodSub,
			VodEn:          v.VodEn,
		})
	}
	if skipped > 0 {
		logWarn(fmt.Sprintf("UpsertVideos[%s] 跳过 %d 条无 vod_id 的视频", sourceKey, skipped))
	}
	if len(rows) == 0 {
		return nil
	}

	if _, err := instance.NamedExec(q, rows); err != nil {
		logError(fmt.Sprintf("UpsertVideos[%s] failed: %v", sourceKey, err))
		return err
	}
	return nil
}

func upsertGlobalVideo(vodName, year, area, lang, director, actor, tag, content, pic string) (int64, error) {
	globalID, err := GetOrCreateGlobalID(vodName)
	if err != nil {
		return 0, err
	}
	_, err = instance.Exec(`UPDATE global_video SET
		year=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE year END,
		area=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE area END,
		lang=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE lang END,
		director=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE director END,
		actor=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE actor END,
		tag=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE tag END,
		content=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE content END,
		pic=CASE WHEN ? != '' AND ? IS NOT NULL THEN ? ELSE pic END,
		updated_at=CURRENT_TIMESTAMP
		WHERE id = ?`,
		year, year, year, area, area, area, lang, lang, lang,
		director, director, director, actor, actor, actor,
		tag, tag, tag, content, content, content, pic, pic, pic,
		globalID)
	return globalID, err
}

// InsertNewVideos 已废弃，保留为向后兼容
// 请使用 UpsertVideos 或 MergeVideoDetails
func InsertNewVideos(sourceKey string, videos []*model.Video) error {
	return UpsertVideos(sourceKey, videos)
}

// MergeVideoDetails 合并视频详情：先读取数据库已有记录，源站非空字段覆盖数据库值，源站空字段保留数据库值。
// 适用于搜索场景：源站搜索可能返回比数据库中更完整或更新的字段，但某些字段可能为空。
func MergeVideoDetails(sourceKey string, videos []*model.Video) error {
	if len(videos) == 0 {
		return nil
	}
	if err := EnsureVideoTable(sourceKey); err != nil {
		return err
	}

	var mergedVideos []*model.Video
	for _, v := range videos {
		if v == nil { continue }
		vid := normalizeFlex(v.VodId)
		if vid == "" { continue }

		existing, err := GetVideoById(sourceKey, vid)
		if err == nil && existing != nil {
			if v.VodName == "" { v.VodName = existing.VodName }
			if v.TypeId.String() == "" { v.TypeId = existing.TypeId }
			if v.TypeName == "" { v.TypeName = existing.TypeName }
			if v.VodPic == "" { v.VodPic = existing.VodPic }
			if v.VodActor == "" { v.VodActor = existing.VodActor }
			if v.VodDirector == "" { v.VodDirector = existing.VodDirector }
			if v.VodContent == "" { v.VodContent = existing.VodContent }
			if v.VodArea == "" { v.VodArea = existing.VodArea }
			if v.VodYear == "" { v.VodYear = existing.VodYear }
			if v.VodLang == "" { v.VodLang = existing.VodLang }
			if v.VodRemarks == "" { v.VodRemarks = existing.VodRemarks }
			if v.VodPlayUrl == "" { v.VodPlayUrl = existing.VodPlayUrl }
			if v.VodDownUrl == "" { v.VodDownUrl = existing.VodDownUrl }
			if v.VodTime == "" { v.VodTime = existing.VodTime }
			if v.VodDoubanScore.String() == "" { v.VodDoubanScore = existing.VodDoubanScore }
			if v.VodHits.String() == "" { v.VodHits = existing.VodHits }
			if v.VodScore.String() == "" { v.VodScore = existing.VodScore }
			if v.VodPlayFrom == "" { v.VodPlayFrom = existing.VodPlayFrom }
			if v.VodLetter == "" { v.VodLetter = existing.VodLetter }
			if v.VodTag == "" { v.VodTag = existing.VodTag }
			if v.VodSub == "" { v.VodSub = existing.VodSub }
			if v.VodEn == "" { v.VodEn = existing.VodEn }
		}
		mergedVideos = append(mergedVideos, v)
	}

	if len(mergedVideos) == 0 { return nil }
	return UpsertVideos(sourceKey, mergedVideos)
}

// rawVideoRow 仅用于 SQL 扫描，使用原生 Go 类型，避免 FlexibleString 与 sqlx/sqlite3 驱动的兼容性问题
type rawVideoRow struct {
	Id             int    `db:"id"`
	VodId          string `db:"vod_id"`
	TypeId         string `db:"type_id"`
	TypeName       string `db:"type_name"`
	VodName        string `db:"vod_name"`
	VodPic         string `db:"vod_pic"`
	VodRemarks     string `db:"vod_remarks"`
	VodYear        string `db:"vod_year"`
	VodArea        string `db:"vod_area"`
	VodDirector    string `db:"vod_director"`
	VodActor       string `db:"vod_actor"`
	VodDoubanScore string `db:"vod_douban_score"`
	VodHits        string `db:"vod_hits"`
	VodVersion     string `db:"vod_version"`
	VodState       string `db:"vod_state"`
	VodIsEnd       string `db:"vod_isend"`
	GlobalId       int64  `db:"global_id"`
}

func rowToVideo(r rawVideoRow) *model.Video {
	return &model.Video{
		Id:             r.Id,
		VodId:          model.FlexibleString(r.VodId),
		TypeId:         model.FlexibleString(r.TypeId),
		TypeName:       r.TypeName,
		VodName:        r.VodName,
		VodPic:         r.VodPic,
		VodRemarks:     r.VodRemarks,
		VodYear:        r.VodYear,
		VodArea:        r.VodArea,
		VodDirector:    r.VodDirector,
		VodActor:       r.VodActor,
		VodDoubanScore: model.FlexibleString(r.VodDoubanScore),
		VodHits:        model.FlexibleString(r.VodHits),
		VodVersion:     model.FlexibleString(r.VodVersion),
		VodState:       model.FlexibleString(r.VodState),
		VodIsEnd:       model.FlexibleString(r.VodIsEnd),
	}
}

type FilterParams struct {
	TypeId   string
	Year     string
	Area     string
	Keyword  string
	Sort     string // "" 或 "rating"/"hot"
	Page     int
	PageSize int
}

func GetVideos(sourceKey string, filter FilterParams) ([]*model.Video, int, error) {
	EnsureVideoTable(sourceKey)
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PageSize <= 0 {
		filter.PageSize = 20
	}
	tn := VideoTableName(sourceKey)

	var (
		whereParts []string
		args       []interface{}
	)

	if filter.TypeId != "" && filter.TypeId != "0" && filter.TypeId != "all" {
		whereParts = append(whereParts, "(v.type_id = ? OR CAST(v.type_id AS TEXT) = ?)")
		args = append(args, filter.TypeId, filter.TypeId)
	}
	if filter.Year != "" && filter.Year != "all" {
		whereParts = append(whereParts, "(v.vod_year = ? OR g.year = ?)")
		args = append(args, filter.Year, filter.Year)
	}
	if filter.Area != "" && filter.Area != "all" {
		whereParts = append(whereParts, "(v.vod_area = ? OR g.area = ?)")
		args = append(args, filter.Area, filter.Area)
	}
	kw := strings.TrimSpace(filter.Keyword)
	if kw != "" {
		like := "%" + kw + "%"
		whereParts = append(whereParts,
			`(v.vod_name LIKE ? OR v.type_name LIKE ? OR g.actor LIKE ? OR g.director LIKE ? OR v.vod_remarks LIKE ? OR v.vod_year LIKE ? OR v.vod_area LIKE ? OR g.year LIKE ? OR g.area LIKE ?)`)
		args = append(args, like, like, like, like, like, like, like, like, like)
	}

	whereClause := ""
	if len(whereParts) > 0 {
		whereClause = " WHERE " + strings.Join(whereParts, " AND ")
	}

	var total int
	countQ := fmt.Sprintf(`SELECT COUNT(1) FROM %s v LEFT JOIN global_video g ON v.global_id = g.id%s`, tn, whereClause)
	if err := instance.Get(&total, countQ, args...); err != nil {
		logError(fmt.Sprintf("GetVideos[%s] count failed: %v", sourceKey, err))
	}

	orderClause := "ORDER BY v.vod_time DESC"
	switch strings.ToLower(strings.TrimSpace(filter.Sort)) {
	case "rating":
		orderClause = "ORDER BY CASE WHEN v.vod_douban_score = '' OR v.vod_douban_score IS NULL THEN 1 ELSE 0 END ASC, CAST(v.vod_douban_score AS REAL) DESC, v.vod_time DESC"
	case "hot":
		orderClause = "ORDER BY CASE WHEN v.vod_hits = '' OR v.vod_hits IS NULL THEN 1 ELSE 0 END ASC, CAST(v.vod_hits AS INTEGER) DESC, v.vod_time DESC"
	}

	var rows []rawVideoRow
	listQ := fmt.Sprintf(`SELECT v.id, CAST(v.vod_id AS TEXT) AS vod_id, CAST(v.type_id AS TEXT) AS type_id,
		v.type_name, v.vod_name, COALESCE(g.pic, v.vod_pic, '') AS vod_pic, v.vod_remarks, COALESCE(g.year, '') AS vod_year,
		COALESCE(g.area, '') AS vod_area, COALESCE(g.director, '') AS vod_director, COALESCE(g.actor, '') AS vod_actor,
		v.vod_douban_score, v.vod_hits, v.vod_version, v.vod_state, v.vod_isend
		FROM %s v LEFT JOIN global_video g ON v.global_id = g.id%s %s LIMIT ? OFFSET ?`, tn, whereClause, orderClause)
	queryArgs := append(append([]interface{}{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	err := instance.Select(&rows, listQ, queryArgs...)
	if err != nil {
		logError(fmt.Sprintf("GetVideos[%s] list failed: %v", sourceKey, err))
	}

	out := make([]*model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToVideo(r))
	}
	return out, total, nil
}

func GetVideoById(sourceKey string, vodId string) (*model.Video, error) {
	type row struct {
		Id             int    `db:"id"`
		VodId          string `db:"vod_id"`
		TypeId         string `db:"type_id"`
		TypeName       string `db:"type_name"`
		VodName        string `db:"vod_name"`
		VodClass       string `db:"vod_class"`
		VodLang        string `db:"vod_lang"`
		VodActor       string `db:"vod_actor"`
		VodArea        string `db:"vod_area"`
		VodContent     string `db:"vod_content"`
		VodPic         string `db:"vod_pic"`
		VodDirector    string `db:"vod_director"`
		VodRemarks     string `db:"vod_remarks"`
		VodYear        string `db:"vod_year"`
		VodPlayUrl     string `db:"vod_play_url"`
		VodDownUrl     string `db:"vod_down_url"`
		VodTime        string `db:"vod_time"`
		VodDoubanId    string `db:"vod_douban_id"`
		VodDoubanScore string `db:"vod_douban_score"`
		VodHits        string `db:"vod_hits"`
		VodHitsDay     string `db:"vod_hits_day"`
		VodHitsWeek    string `db:"vod_hits_week"`
		VodHitsMonth   string `db:"vod_hits_month"`
		VodPubdate     string `db:"vod_pubdate"`
		VodVersion     string `db:"vod_version"`
		VodState       string `db:"vod_state"`
		VodScore       string `db:"vod_score"`
		VodScoreAll    string `db:"vod_score_all"`
		VodScoreNum    string `db:"vod_score_num"`
		VodIsEnd       string `db:"vod_isend"`
		VodPlayFrom    string `db:"vod_play_from"`
		VodPlayNote    string `db:"vod_play_note"`
		VodLetter      string `db:"vod_letter"`
		VodTag         string `db:"vod_tag"`
		VodSub         string `db:"vod_sub"`
		VodEn          string `db:"vod_en"`
	}
	var r row
	tn := VideoTableName(sourceKey)
	q := fmt.Sprintf(`SELECT 
			v.id, CAST(v.vod_id AS TEXT) AS vod_id, CAST(v.type_id AS TEXT) AS type_id,
			v.type_name, v.vod_name, v.vod_class,
			COALESCE(g.lang, '') AS vod_lang, COALESCE(g.actor, '') AS vod_actor,
			COALESCE(g.area, '') AS vod_area, COALESCE(g.content, '') AS vod_content,
			COALESCE(g.pic, v.vod_pic, '') AS vod_pic, COALESCE(g.director, '') AS vod_director,
			v.vod_remarks, COALESCE(g.year, '') AS vod_year,
			v.vod_play_url, v.vod_down_url, v.vod_time,
			v.vod_douban_id, v.vod_douban_score, v.vod_hits, v.vod_hits_day, v.vod_hits_week, v.vod_hits_month,
			v.vod_pubdate, v.vod_version, v.vod_state, v.vod_score, v.vod_score_all, v.vod_score_num,
			v.vod_isend, v.vod_play_from, v.vod_play_note, v.vod_letter, COALESCE(g.tag, '') AS vod_tag, v.vod_sub, v.vod_en
		FROM %s v
		LEFT JOIN global_video g ON v.global_id = g.id
		WHERE v.vod_id = ? OR CAST(v.vod_id AS TEXT) = ? LIMIT 1`, tn)
	err := instance.Get(&r, q, vodId, vodId)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		logError(fmt.Sprintf("GetVideoById[%s] %s: %v", sourceKey, vodId, err))
		return nil, err
	}

	return &model.Video{
		Id:             r.Id,
		VodId:          model.FlexibleString(r.VodId),
		TypeId:         model.FlexibleString(r.TypeId),
		TypeName:       r.TypeName,
		VodName:        r.VodName,
		VodClass:       r.VodClass,
		VodLang:        r.VodLang,
		VodActor:       r.VodActor,
		VodArea:        r.VodArea,
		VodContent:     r.VodContent,
		VodPic:         r.VodPic,
		VodDirector:    r.VodDirector,
		VodRemarks:     r.VodRemarks,
		VodYear:        r.VodYear,
		VodPlayUrl:     r.VodPlayUrl,
		VodDownUrl:     r.VodDownUrl,
		VodTime:        r.VodTime,
		VodDoubanId:    model.FlexibleString(r.VodDoubanId),
		VodDoubanScore: model.FlexibleString(r.VodDoubanScore),
		VodHits:        model.FlexibleString(r.VodHits),
		VodHitsDay:     model.FlexibleString(r.VodHitsDay),
		VodHitsWeek:    model.FlexibleString(r.VodHitsWeek),
		VodHitsMonth:   model.FlexibleString(r.VodHitsMonth),
		VodPubdate:     model.FlexibleString(r.VodPubdate),
		VodVersion:     model.FlexibleString(r.VodVersion),
		VodState:       model.FlexibleString(r.VodState),
		VodScore:       model.FlexibleString(r.VodScore),
		VodScoreAll:    model.FlexibleString(r.VodScoreAll),
		VodScoreNum:    model.FlexibleString(r.VodScoreNum),
		VodIsEnd:       model.FlexibleString(r.VodIsEnd),
		VodPlayFrom:    r.VodPlayFrom,
		VodPlayNote:    r.VodPlayNote,
		VodLetter:      r.VodLetter,
		VodTag:         r.VodTag,
		VodSub:         r.VodSub,
		VodEn:          r.VodEn,
	}, nil
}

func GetDistinctYears(sourceKey string) ([]string, error) {
	if !TableExists(VideoTableName(sourceKey)) {
		return []string{}, nil
	}
	var values []string
	q := `SELECT DISTINCT year FROM global_video WHERE year IS NOT NULL AND year <> '' ORDER BY year DESC`
	err := instance.Select(&values, q)
	if err != nil {
		logError(fmt.Sprintf("GetDistinctYears[%s] failed: %v", sourceKey, err))
		return []string{}, nil
	}
	return values, nil
}

func GetDistinctAreas(sourceKey string) ([]string, error) {
	if !TableExists(VideoTableName(sourceKey)) {
		return []string{}, nil
	}
	var values []string
	q := `SELECT DISTINCT area FROM global_video WHERE area IS NOT NULL AND area <> '' ORDER BY area ASC`
	err := instance.Select(&values, q)
	if err != nil {
		logError(fmt.Sprintf("GetDistinctAreas[%s] failed: %v", sourceKey, err))
		return []string{}, nil
	}
	return values, nil
}

// GetRandomRecommend 返回最近若干条视频中按"同一类型/年份/地区"加权的推荐列表（去重，最多 limit 条）。
// excludeIds: 要排除的 vod_id 集合（例如"继续观看"里已出现的），避免同一条视频在多个推荐区重复。
func GetRandomRecommend(sourceKey string, limit int, excludeIds []string) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 8
	}
	tn := VideoTableName(sourceKey)
	if !TableExists(tn) {
		return []*model.Video{}, nil
	}

	var rows []rawVideoRow
	q := fmt.Sprintf(`SELECT v.id, CAST(v.vod_id AS TEXT) AS vod_id, CAST(v.type_id AS TEXT) AS type_id,
		v.type_name, v.vod_name, COALESCE(g.pic, '') AS vod_pic, v.vod_remarks, COALESCE(g.year, '') AS vod_year,
		COALESCE(g.area, '') AS vod_area, COALESCE(g.director, '') AS vod_director, COALESCE(g.actor, '') AS vod_actor,
		v.vod_douban_score, v.vod_hits, v.vod_version, v.vod_state, v.vod_isend
		FROM %s v LEFT JOIN global_video g ON v.global_id = g.id
		ORDER BY v.vod_time DESC LIMIT ?`, tn)
	err := instance.Select(&rows, q, 200)
	if err != nil {
		logError(fmt.Sprintf("GetRandomRecommend[%s] failed: %v", sourceKey, err))
		return []*model.Video{}, nil
	}

	excludeSet := make(map[string]bool, len(excludeIds))
	for _, id := range excludeIds {
		if id != "" {
			excludeSet[strings.ToLower(id)] = true
		}
	}

	out := make([]*model.Video, 0, limit)
	seen := make(map[string]bool)
	for _, r := range rows {
		id := strings.ToLower(r.VodId)
		if id == "" || seen[id] || excludeSet[id] {
			continue
		}
		seen[id] = true
		out = append(out, rowToVideo(r))
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func SearchVideos(sourceKey, keyword string, page, pageSize int) ([]*model.Video, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	tn := VideoTableName(sourceKey)
	if !TableExists(tn) {
		return []*model.Video{}, 0, nil
	}
	like := "%" + keyword + "%"
	where := ` WHERE v.vod_name LIKE ? OR v.type_name LIKE ? OR g.actor LIKE ? OR g.director LIKE ? OR v.vod_remarks LIKE ? OR g.year LIKE ? OR g.area LIKE ?`
	likes := []interface{}{like, like, like, like, like, like, like}

	var total int
	countQ := fmt.Sprintf(`SELECT COUNT(1) FROM %s v LEFT JOIN global_video g ON v.global_id = g.id%s`, tn, where)
	_ = instance.Get(&total, countQ, likes...)

	var rows []rawVideoRow
	listQ := fmt.Sprintf(`SELECT v.id, CAST(v.vod_id AS TEXT) AS vod_id, CAST(v.type_id AS TEXT) AS type_id,
		v.type_name, v.vod_name, COALESCE(g.pic, '') AS vod_pic, v.vod_remarks, COALESCE(g.year, '') AS vod_year,
		COALESCE(g.area, '') AS vod_area, COALESCE(g.director, '') AS vod_director, COALESCE(g.actor, '') AS vod_actor,
		v.vod_douban_score, v.vod_hits, v.vod_version, v.vod_state, v.vod_isend
		FROM %s v LEFT JOIN global_video g ON v.global_id = g.id%s ORDER BY v.vod_time DESC LIMIT ? OFFSET ?`, tn, where)
	args := append(append([]interface{}{}, likes...), pageSize, (page-1)*pageSize)
	err := instance.Select(&rows, listQ, args...)
	if err != nil {
		logError(fmt.Sprintf("SearchVideos[%s] kw=%q failed: %v", sourceKey, keyword, err))
	}
	out := make([]*model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, rowToVideo(r))
	}
	return out, total, nil
}

// SearchVideosLegacy 保留老签名以便兼容旧调用（转发到新实现）
func SearchVideosLegacy(sourceKey, keyword string, page, pageSize int) ([]*model.Video, error) {
	list, _, err := SearchVideos(sourceKey, keyword, page, pageSize)
	return list, err
}

// SearchVideoCount 返回搜索命中总数——兼容旧调用方
func SearchVideoCount(sourceKey, keyword string) (int, error) {
	_, total, err := SearchVideos(sourceKey, keyword, 1, 1)
	return total, err
}

func GetVideoCountByType(sourceKey string, typeId string) (int, error) {
	EnsureVideoTable(sourceKey)
	var count int
	tn := VideoTableName(sourceKey)
	var (
		q   string
		err error
	)
	if typeId == "" || typeId == "0" || typeId == "all" {
		q = fmt.Sprintf(`SELECT COUNT(1) FROM %s`, tn)
		err = instance.Get(&count, q)
	} else {
		q = fmt.Sprintf(`SELECT COUNT(1) FROM %s WHERE type_id = ?`, tn)
		err = instance.Get(&count, q, typeId)
	}
	return count, err
}

func InsTypeIfNotExist(sourceKey string, typeId model.FlexibleString, typeName string) error {
	tid := normalizeFlex(typeId)
	if tid == "" {
		// 没有 type_id 就不写入，避免大量空值合并
		return nil
	}
	// 过滤空类型名：源站可能返回 type_name 为空的条目
	tn := strings.TrimSpace(typeName)
	if tn == "" {
		return nil
	}
	EnsureTypeTable(sourceKey)
	q := fmt.Sprintf(`INSERT OR IGNORE INTO %s (type_id, type_name) VALUES (?, ?)`, TypeTableName(sourceKey))
	_, err := instance.Exec(q, tid, tn)
	return err
}

func GetTypes(sourceKey string) ([]*model.VType, error) {
	EnsureTypeTable(sourceKey)
	type typeRow struct {
		Id       int    `db:"id"`
		TypeId   string `db:"type_id"`
		TypeName string `db:"name"`
	}
	var rows []typeRow
	q := fmt.Sprintf(`SELECT id, CAST(type_id AS TEXT) AS type_id, type_name as name FROM %s ORDER BY type_id`, TypeTableName(sourceKey))
	err := instance.Select(&rows, q)
	if err != nil {
		return nil, err
	}
	out := make([]*model.VType, 0, len(rows))
	for _, r := range rows {
		// 过滤空类型名（源站可能返回空名）
		if strings.TrimSpace(r.TypeName) == "" {
			continue
		}
		out = append(out, &model.VType{
			Id:     r.Id,
			TypeId: model.FlexibleString(r.TypeId),
			Name:   r.TypeName,
		})
	}
	return out, nil
}

// UpsertEpisodes 保留为一个空操作以兼容既有调用方。
// 剧集信息已经以 "#" 分隔的形式整体存放在 v_xxx.vod_play_url（并由上层做 brotli 压缩），
// 因此不再需要一集一行的冗余 e_xxx 表。读取时由 GetEpisodes / GetSampleEpisodes 直接解析 vod_play_url。
func UpsertEpisodes(sourceKey string, episodes []*model.Episode) error {
	_ = sourceKey
	_ = episodes
	return nil
}

// DeleteVideo 从指定源的视频表中删除一条视频（同时清理相关剧集、收藏、历史）
func DeleteVideo(sourceKey string, vodId string) error {
	if !TableExists(VideoTableName(sourceKey)) {
		return fmt.Errorf("表不存在")
	}
	tn := VideoTableName(sourceKey)
	res, err := instance.Exec(fmt.Sprintf(`DELETE FROM %s WHERE vod_id = ?`, tn), vodId)
	if err != nil {
		return fmt.Errorf("delete video: %w", err)
	}
	affected, _ := res.RowsAffected()
	if affected == 0 {
		return fmt.Errorf("视频不存在")
	}
	// 清理关联的收藏和历史记录
	instance.Exec(`DELETE FROM favorites WHERE source_key = ? AND vod_id = ?`, sourceKey, vodId)
	instance.Exec(`DELETE FROM watch_history WHERE source_key = ? AND vod_id = ?`, sourceKey, vodId)
	return nil
}

func GetEpisodes(sourceKey string, vodId string) ([]*model.Episode, error) {
	// 1) 优先：从视频行的 vod_play_url 解析（已 br 压缩，对重复 URL 前缀极高效）
	if TableExists(VideoTableName(sourceKey)) {
		var playUrl string
		q := fmt.Sprintf(`SELECT vod_play_url FROM %s WHERE vod_id = ? LIMIT 1`, VideoTableName(sourceKey))
		err := instance.Get(&playUrl, q, vodId)
		if err == nil && playUrl != "" {
			// 可能被 brotli 压缩过；在此解压
			raw := util.DecompressIfNeeded(playUrl)
			eps := parseEpisodesInline(raw, vodId)
			if len(eps) > 0 {
				return eps, nil
			}
		}
	}

	// 2) 回退：对于较旧的数据库，可能仍然在 e_ 表里存了一集一行
	if !TableExists(EpisodeTableName(sourceKey)) {
		return nil, nil
	}
	var eps []*model.Episode
	q := fmt.Sprintf(`SELECT vod_id, ep_num, ep_name, ep_url FROM %s WHERE vod_id = ? ORDER BY ep_num`,
		EpisodeTableName(sourceKey))
	_ = instance.Select(&eps, q, vodId)
	return eps, nil
}

// parseEpisodesInline 在 db 包内实现轻量解析，避免与 collect 包的循环依赖。
// 输入："第01集$https://...#第02集$https://...#..." 的字符串（已解压）
func parseEpisodesInline(playUrl, vodId string) []*model.Episode {
	if playUrl == "" {
		return nil
	}
	parts := strings.Split(playUrl, "#")
	out := make([]*model.Episode, 0, len(parts))
	for i, part := range parts {
		if strings.TrimSpace(part) == "" {
			continue
		}
		name := part
		url := ""
		if idx := strings.Index(part, "$"); idx >= 0 {
			name = part[:idx]
			url = part[idx+1:]
		}
		out = append(out, &model.Episode{
			VodId:  model.FlexibleString(vodId),
			EpNum:  i + 1,
			EpName: strings.TrimSpace(name),
			EpUrl:  strings.TrimSpace(url),
		})
	}
	return out
}

func TableExists(name string) bool {
	var count int
	instance.Get(&count, `SELECT COUNT(1) FROM sqlite_master WHERE type='table' AND name=?`, name)
	return count > 0
}

func GetVideoCount(sourceKey string) int {
	if !TableExists(VideoTableName(sourceKey)) {
		return 0
	}
	var count int
	instance.Get(&count, fmt.Sprintf(`SELECT COUNT(1) FROM %s`, VideoTableName(sourceKey)))
	return count
}

func GetEpisodeCount(sourceKey string) int {
	// 新的存储模式下没有一集一行，"总集数"没有对应表来；改为基于视频表的 vod_play_url 估算。
	// 为了给用户一个"有多少集"的指标，用 SUM( (# 号数量 +1) 来估算总数。
	if !TableExists(VideoTableName(sourceKey)) {
		return 0
	}
	// 先统计所有视频的 "#" 分隔符总数 + 视频数 ≈ 总集数
	type row struct {
		PlayUrl string `db:"vod_play_url"`
	}
	var rows []*row
	q := fmt.Sprintf(`SELECT vod_play_url FROM %s`, VideoTableName(sourceKey))
	_ = instance.Select(&rows, q)
	total := 0
	for _, r := range rows {
		if r == nil || r.PlayUrl == "" {
			continue
		}
		raw := util.DecompressIfNeeded(r.PlayUrl)
		if raw == "" {
			continue
		}
		cnt := strings.Count(raw, "#") + 1
		total += cnt
	}
	return total
}

func GetSetting(key string) (string, error) {
	var val string
	err := instance.Get(&val, `SELECT value FROM settings WHERE key = ?`, key)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return val, err
}

func SetSetting(key, value string) error {
	_, err := instance.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES (?, ?)`, key, value)
	return err
}

// ============================================================
// 数据源管理：列信息 / 示例数据 / 清理 / 重建
// ============================================================

// TableColumn 描述某张表的一列
type TableColumn struct {
	Cid       int    `json:"cid" db:"cid"`
	Name      string `json:"name" db:"name"`
	ColType   string `json:"col_type" db:"col_type"`
	NotNull   int    `json:"notnull" db:"notnull"`
	DfltValue string `json:"dflt_value" db:"dflt_value"`
	Pk        int    `json:"pk" db:"pk"`
}

// GetTableColumns 返回指定表的列定义（基于 SQLite PRAGMA table_info）
func GetTableColumns(tableName string) ([]TableColumn, error) {
	var cols []TableColumn
	err := instance.Select(&cols, fmt.Sprintf(`PRAGMA table_info(%s)`, safeIdent(tableName)))
	return cols, err
}

// GetSampleVideos 返回某个源视频表最近 N 条"示例记录"，用作页面预览
func GetSampleVideos(sourceKey string, limit int) ([]*model.Video, error) {
	if limit <= 0 {
		limit = 5
	}
	EnsureVideoTable(sourceKey)
	tn := VideoTableName(sourceKey)
	type sampleRow struct {
		Id             int    `db:"id"`
		VodId          string `db:"vod_id"`
		TypeId         string `db:"type_id"`
		TypeName       string `db:"type_name"`
		VodName        string `db:"vod_name"`
		VodPic         string `db:"vod_pic"`
		VodRemarks     string `db:"vod_remarks"`
		VodYear        string `db:"vod_year"`
		VodArea        string `db:"vod_area"`
		VodTime        string `db:"vod_time"`
		VodDoubanScore string `db:"vod_douban_score"`
		VodHits        string `db:"vod_hits"`
	}
	var rows []*sampleRow
	q := fmt.Sprintf(`SELECT v.id, CAST(v.vod_id AS TEXT) AS vod_id, CAST(v.type_id AS TEXT) AS type_id,
		v.type_name, v.vod_name, COALESCE(g.pic, '') AS vod_pic, v.vod_remarks, COALESCE(g.year, '') AS vod_year,
		COALESCE(g.area, '') AS vod_area, v.vod_time, v.vod_douban_score, v.vod_hits
		FROM %s v LEFT JOIN global_video g ON v.global_id = g.id ORDER BY v.vod_time DESC LIMIT ?`, tn)
	err := instance.Select(&rows, q, limit)
	if err != nil {
		return nil, err
	}
	out := make([]*model.Video, 0, len(rows))
	for _, r := range rows {
		out = append(out, &model.Video{
			Id:             r.Id,
			VodId:          model.FlexibleString(r.VodId),
			TypeId:         model.FlexibleString(r.TypeId),
			TypeName:       r.TypeName,
			VodName:        r.VodName,
			VodPic:         r.VodPic,
			VodRemarks:     r.VodRemarks,
			VodYear:        r.VodYear,
			VodArea:        r.VodArea,
			VodTime:        r.VodTime,
			VodDoubanScore: model.FlexibleString(r.VodDoubanScore),
			VodHits:        model.FlexibleString(r.VodHits),
		})
	}
	return out, nil
}

// GetVideosMissingDouban 已废弃：现在由全局 douban_info 表管理豆瓣数据
// 保留此函数以兼容旧调用，始终返回空列表
func GetVideosMissingDouban(sourceKey string, limit int) ([]*model.Video, error) {
	return nil, nil
}

// GetSampleEpisodes 返回某个源示例剧集（来自多个视频的 vod_play_url 解析）
func GetSampleEpisodes(sourceKey string, limit int) ([]*model.Episode, error) {
	if limit <= 0 {
		limit = 10
	}
	if !TableExists(VideoTableName(sourceKey)) {
		return nil, nil
	}

	// 从最近若干条视频里拿 vod_id 和 vod_play_url，逐个解析，聚合前 limit 条
	type row struct {
		VodId   string `db:"vod_id"`
		PlayUrl string `db:"vod_play_url"`
	}
	var rows []*row
	q := fmt.Sprintf(`SELECT CAST(vod_id AS TEXT) AS vod_id, vod_play_url FROM %s ORDER BY vod_time DESC LIMIT ?`,
		VideoTableName(sourceKey))
	err := instance.Select(&rows, q, 10)
	if err != nil {
		return nil, err
	}

	out := make([]*model.Episode, 0, limit)
	for _, r := range rows {
		if r == nil || r.PlayUrl == "" {
			continue
		}
		raw := util.DecompressIfNeeded(r.PlayUrl)
		eps := parseEpisodesInline(raw, r.VodId)
		for _, e := range eps {
			out = append(out, e)
			if len(out) >= limit {
				return out, nil
			}
		}
	}
	return out, nil
}

// TruncateSource 仅清空某源的视频/剧集/分类数据，但保留 source 元信息
func TruncateSource(sourceKey string) error {
	vTbl := VideoTableName(sourceKey)
	eTbl := EpisodeTableName(sourceKey)
	tTbl := TypeTableName(sourceKey)
	if TableExists(vTbl) {
		if _, err := instance.Exec(fmt.Sprintf(`DELETE FROM %s`, safeIdent(vTbl))); err != nil {
			return err
		}
	}
	if TableExists(eTbl) {
		if _, err := instance.Exec(fmt.Sprintf(`DELETE FROM %s`, safeIdent(eTbl))); err != nil {
			return err
		}
	}
	if TableExists(tTbl) {
		if _, err := instance.Exec(fmt.Sprintf(`DELETE FROM %s`, safeIdent(tTbl))); err != nil {
			return err
		}
	}
	return nil
}

// DropSourceTables 删除该源的三张子表（视频/剧集/分类）。下次 Ensure*Table 时会重建。
func DropSourceTables(sourceKey string) error {
	for _, name := range []string{VideoTableName(sourceKey), EpisodeTableName(sourceKey), TypeTableName(sourceKey)} {
		if _, err := instance.Exec(fmt.Sprintf(`DROP TABLE IF EXISTS %s`, safeIdent(name))); err != nil {
			return err
		}
	}
	return nil
}

// RecreateSourceTables 删除并重建该源的三张表（数据全部丢失）
func RecreateSourceTables(sourceKey string) error {
	if err := DropSourceTables(sourceKey); err != nil {
		return err
	}
	if err := EnsureVideoTable(sourceKey); err != nil {
		return err
	}
	if err := EnsureEpisodeTable(sourceKey); err != nil {
		return err
	}
	if err := EnsureTypeTable(sourceKey); err != nil {
		return err
	}
	return nil
}

// DeleteOlderThan 删除某源 vod_time 小于阈值的视频（简单字符串比较，格式为 YYYY-MM-DD HH:mm:ss 时可用）
func DeleteOlderThan(sourceKey string, threshold string) (int64, error) {
	tn := VideoTableName(sourceKey)
	res, err := instance.Exec(fmt.Sprintf(`DELETE FROM %s WHERE vod_time < ?`, safeIdent(tn)), threshold)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// DeleteByVodId 精确删除某源中的一条视频记录（同时清理相关剧集）
func DeleteByVodId(sourceKey string, vodId string) error {
	_, err := instance.Exec(
		fmt.Sprintf(`DELETE FROM %s WHERE vod_id = ?`, safeIdent(VideoTableName(sourceKey))),
		vodId,
	)
	if err != nil {
		return err
	}
	if TableExists(EpisodeTableName(sourceKey)) {
		_, err = instance.Exec(
			fmt.Sprintf(`DELETE FROM %s WHERE vod_id = ?`, safeIdent(EpisodeTableName(sourceKey))),
			vodId,
		)
	}
	return err
}

// ===================== 数据源导入导出辅助（用于「导出源/导入源」功能） =====================

// ExportVideoRow 表示导出时一条完整的视频记录（含 vod_play_url 等所有字段）
type ExportVideoRow struct {
	VodId          string `json:"vod_id" db:"vod_id"`
	TypeId         string `json:"type_id" db:"type_id"`
	TypeName       string `json:"type_name" db:"type_name"`
	VodName        string `json:"vod_name" db:"vod_name"`
	VodClass       string `json:"vod_class" db:"vod_class"`
	VodLang        string `json:"vod_lang" db:"vod_lang"`
	VodActor       string `json:"vod_actor" db:"vod_actor"`
	VodArea        string `json:"vod_area" db:"vod_area"`
	VodContent     string `json:"vod_content" db:"vod_content"`
	VodPic         string `json:"vod_pic" db:"vod_pic"`
	VodDirector    string `json:"vod_director" db:"vod_director"`
	VodRemarks     string `json:"vod_remarks" db:"vod_remarks"`
	VodYear        string `json:"vod_year" db:"vod_year"`
	VodPlayUrl     string `json:"vod_play_url" db:"vod_play_url"`
	VodDownUrl     string `json:"vod_down_url" db:"vod_down_url"`
	VodTime        string `json:"vod_time" db:"vod_time"`
	VodDoubanId    string `json:"vod_douban_id" db:"vod_douban_id"`
	VodDoubanScore string `json:"vod_douban_score" db:"vod_douban_score"`
	VodHits        string `json:"vod_hits" db:"vod_hits"`
	VodHitsDay     string `json:"vod_hits_day" db:"vod_hits_day"`
	VodHitsWeek    string `json:"vod_hits_week" db:"vod_hits_week"`
	VodHitsMonth   string `json:"vod_hits_month" db:"vod_hits_month"`
	VodPubdate     string `json:"vod_pubdate" db:"vod_pubdate"`
	VodVersion     string `json:"vod_version" db:"vod_version"`
	VodState       string `json:"vod_state" db:"vod_state"`
	VodScore       string `json:"vod_score" db:"vod_score"`
	VodScoreAll    string `json:"vod_score_all" db:"vod_score_all"`
	VodScoreNum    string `json:"vod_score_num" db:"vod_score_num"`
	VodIsEnd       string `json:"vod_isend" db:"vod_isend"`
	VodPlayFrom    string `json:"vod_play_from" db:"vod_play_from"`
	VodPlayNote    string `json:"vod_play_note" db:"vod_play_note"`
	VodLetter      string `json:"vod_letter" db:"vod_letter"`
	VodTag         string `json:"vod_tag" db:"vod_tag"`
	VodSub         string `json:"vod_sub" db:"vod_sub"`
	VodEn          string `json:"vod_en" db:"vod_en"`
}

// ExportTypeRow 表示导出时一条类型记录
type ExportTypeRow struct {
	TypeId   string `json:"type_id" db:"type_id"`
	TypeName string `json:"type_name" db:"type_name"`
}

// ExportAllVideos 导出某个源的所有视频行（按 vod_time 倒序）
func ExportAllVideos(sourceKey string) ([]*ExportVideoRow, error) {
	if !TableExists(VideoTableName(sourceKey)) {
		return nil, nil
	}
	tn := VideoTableName(sourceKey)
	q := fmt.Sprintf(`SELECT CAST(v.vod_id AS TEXT) AS vod_id,
		CAST(v.type_id AS TEXT) AS type_id,
		v.type_name, v.vod_name, v.vod_class, COALESCE(g.lang, '') AS vod_lang, COALESCE(g.actor, '') AS vod_actor,
		COALESCE(g.area, '') AS vod_area, COALESCE(g.content, '') AS vod_content, COALESCE(g.pic, '') AS vod_pic,
		COALESCE(g.director, '') AS vod_director, v.vod_remarks, COALESCE(g.year, '') AS vod_year,
		v.vod_play_url, v.vod_down_url, v.vod_time,
		v.vod_douban_id, v.vod_douban_score, v.vod_hits, v.vod_hits_day, v.vod_hits_week, v.vod_hits_month,
		v.vod_pubdate, v.vod_version, v.vod_state, v.vod_score, v.vod_score_all, v.vod_score_num,
		v.vod_isend, v.vod_play_from, v.vod_play_note, v.vod_letter, COALESCE(g.tag, '') AS vod_tag, v.vod_sub, v.vod_en
		FROM %s v LEFT JOIN global_video g ON v.global_id = g.id ORDER BY v.vod_time DESC`, tn)
	var rows []*ExportVideoRow
	if err := instance.Select(&rows, q); err != nil {
		return nil, fmt.Errorf("ExportAllVideos[%s] failed: %w", sourceKey, err)
	}
	return rows, nil
}

// ExportAllTypes 导出某个源的所有类型行
func ExportAllTypes(sourceKey string) ([]*ExportTypeRow, error) {
	if !TableExists(TypeTableName(sourceKey)) {
		return nil, nil
	}
	tn := TypeTableName(sourceKey)
	q := fmt.Sprintf(`SELECT CAST(type_id AS TEXT) AS type_id, type_name FROM %s`, tn)
	var rows []*ExportTypeRow
	if err := instance.Select(&rows, q); err != nil {
		return nil, fmt.Errorf("ExportAllTypes[%s] failed: %w", sourceKey, err)
	}
	return rows, nil
}

// ImportVideos 批量 upsert 导入的视频行（绕过 UpsertVideos 的 FlexibleString 处理）
func ImportVideos(sourceKey string, rows []*ExportVideoRow) error {
	if len(rows) == 0 {
		return nil
	}
	if err := EnsureVideoTable(sourceKey); err != nil {
		return err
	}
	tn := VideoTableName(sourceKey)

	type importRow struct {
		VodId          string `db:"vod_id"`
		TypeId         string `db:"type_id"`
		TypeName       string `db:"type_name"`
		VodName        string `db:"vod_name"`
		GlobalId       int64  `db:"global_id"`
		VodClass       string `db:"vod_class"`
		VodRemarks     string `db:"vod_remarks"`
		VodPlayUrl     string `db:"vod_play_url"`
		VodDownUrl     string `db:"vod_down_url"`
		VodTime        string `db:"vod_time"`
		VodDoubanId    string `db:"vod_douban_id"`
		VodDoubanScore string `db:"vod_douban_score"`
		VodHits        string `db:"vod_hits"`
		VodHitsDay     string `db:"vod_hits_day"`
		VodHitsWeek    string `db:"vod_hits_week"`
		VodHitsMonth   string `db:"vod_hits_month"`
		VodPubdate     string `db:"vod_pubdate"`
		VodVersion     string `db:"vod_version"`
		VodState       string `db:"vod_state"`
		VodScore       string `db:"vod_score"`
		VodScoreAll    string `db:"vod_score_all"`
		VodScoreNum    string `db:"vod_score_num"`
		VodIsEnd       string `db:"vod_isend"`
		VodPlayFrom    string `db:"vod_play_from"`
		VodPlayNote    string `db:"vod_play_note"`
		VodLetter      string `db:"vod_letter"`
		VodSub         string `db:"vod_sub"`
		VodEn          string `db:"vod_en"`
	}

	cols := []string{"vod_id", "type_id", "type_name", "vod_name", "global_id",
		"vod_class", "vod_remarks", "vod_play_url", "vod_down_url", "vod_time",
		"vod_douban_id", "vod_douban_score", "vod_hits", "vod_hits_day", "vod_hits_week", "vod_hits_month",
		"vod_pubdate", "vod_version", "vod_state", "vod_score", "vod_score_all", "vod_score_num",
		"vod_isend", "vod_play_from", "vod_play_note", "vod_letter", "vod_sub", "vod_en"}

	q := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)
		ON CONFLICT(vod_id) DO UPDATE SET
		type_id=excluded.type_id, type_name=excluded.type_name,
		vod_name=excluded.vod_name, global_id=excluded.global_id,
		vod_class=excluded.vod_class, vod_remarks=excluded.vod_remarks,
		vod_play_url=excluded.vod_play_url, vod_down_url=excluded.vod_down_url,
		vod_time=excluded.vod_time,
		vod_douban_id=excluded.vod_douban_id, vod_douban_score=excluded.vod_douban_score,
		vod_hits=excluded.vod_hits, vod_hits_day=excluded.vod_hits_day,
		vod_hits_week=excluded.vod_hits_week, vod_hits_month=excluded.vod_hits_month,
		vod_pubdate=excluded.vod_pubdate, vod_version=excluded.vod_version,
		vod_state=excluded.vod_state, vod_score=excluded.vod_score,
		vod_score_all=excluded.vod_score_all, vod_score_num=excluded.vod_score_num,
		vod_isend=excluded.vod_isend, vod_play_from=excluded.vod_play_from,
		vod_play_note=excluded.vod_play_note, vod_letter=excluded.vod_letter,
		vod_sub=excluded.vod_sub, vod_en=excluded.vod_en`,
		tn, strings.Join(cols, ","), ":"+strings.Join(cols, ",:"))

	var importRows []*importRow
	for _, r := range rows {
		globalID, err := upsertGlobalVideo(r.VodName, r.VodYear, r.VodArea, r.VodLang,
			r.VodDirector, r.VodActor, r.VodTag, r.VodContent, r.VodPic)
		if err != nil {
			logWarn(fmt.Sprintf("ImportVideos[%s] upsert global_video failed for '%s': %v", sourceKey, r.VodName, err))
			globalID = 0
		}
		importRows = append(importRows, &importRow{
			VodId:          r.VodId,
			TypeId:         r.TypeId,
			TypeName:       r.TypeName,
			VodName:        r.VodName,
			GlobalId:       globalID,
			VodClass:       r.VodClass,
			VodRemarks:     r.VodRemarks,
			VodPlayUrl:     r.VodPlayUrl,
			VodDownUrl:     r.VodDownUrl,
			VodTime:        r.VodTime,
			VodDoubanId:    r.VodDoubanId,
			VodDoubanScore: r.VodDoubanScore,
			VodHits:        r.VodHits,
			VodHitsDay:     r.VodHitsDay,
			VodHitsWeek:    r.VodHitsWeek,
			VodHitsMonth:   r.VodHitsMonth,
			VodPubdate:     r.VodPubdate,
			VodVersion:     r.VodVersion,
			VodState:       r.VodState,
			VodScore:       r.VodScore,
			VodScoreAll:    r.VodScoreAll,
			VodScoreNum:    r.VodScoreNum,
			VodIsEnd:       r.VodIsEnd,
			VodPlayFrom:    r.VodPlayFrom,
			VodPlayNote:    r.VodPlayNote,
			VodLetter:      r.VodLetter,
			VodSub:         r.VodSub,
			VodEn:          r.VodEn,
		})
	}

	const batchSize = 200
	for i := 0; i < len(importRows); i += batchSize {
		end := i + batchSize
		if end > len(importRows) {
			end = len(importRows)
		}
		batch := importRows[i:end]
		if _, err := instance.NamedExec(q, batch); err != nil {
			return fmt.Errorf("ImportVideos[%s] batch[%d-%d] failed: %w", sourceKey, i, end, err)
		}
	}
	return nil
}

// ImportTypes 批量 upsert 导入的类型行
func ImportTypes(sourceKey string, rows []*ExportTypeRow) error {
	if len(rows) == 0 {
		return nil
	}
	if err := EnsureTypeTable(sourceKey); err != nil {
		return err
	}
	tn := TypeTableName(sourceKey)
	q := fmt.Sprintf(`INSERT OR IGNORE INTO %s (type_id, type_name) VALUES (:type_id, :type_name)`, tn)
	if _, err := instance.NamedExec(q, rows); err != nil {
		return fmt.Errorf("ImportTypes[%s] failed: %w", sourceKey, err)
	}
	return nil
}

// safeIdent 非常简单的标识符安全化，仅允许 [a-z0-9_]。所有表名/列名由内部拼接产生，
// 不接受任意用户输入，这里做一道防御以避免误用。
func safeIdent(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' {
			out = append(out, c)
		}
	}
	return string(out)
}
