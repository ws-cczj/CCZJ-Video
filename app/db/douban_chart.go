package db

import (
	"cczjVideo/app/applog"
	"fmt"
	"strings"
)

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
