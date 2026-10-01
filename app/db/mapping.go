package db

import (
	"fmt"
	"math"
	"strings"
)

// AddFavoriteByIdentity records a global favorite using the catalog identity
// already resolved by the caller. A title is not an identity: using it here
// can merge unrelated films or series with the same name.
func AddFavoriteByIdentity(globalID int64, sourceKey, vodID string) error {
	if globalID <= 0 || strings.TrimSpace(sourceKey) == "" || strings.TrimSpace(vodID) == "" {
		return fmt.Errorf("invalid favorite identity")
	}
	// A favorite is global. Keep one source coordinate only for opening it in
	// the UI, instead of creating duplicate list entries for every mirror.
	_, err := instance.Exec(`INSERT INTO favorites (global_id, source_key, vod_id)
		SELECT ?, ?, ?
		WHERE NOT EXISTS (SELECT 1 FROM favorites WHERE global_id = ?)`, globalID, sourceKey, vodID, globalID)
	return err
}

// --- Favorites (基于 global_id) ---

// RemoveFavoriteByGlobalID 按 global_id 删除所有源的收藏
func RemoveFavoriteByGlobalID(globalID int, sourceKey string) error {
	_ = sourceKey // retained for generated binding compatibility
	_, err := instance.Exec(`DELETE FROM favorites WHERE global_id = ?`, globalID)
	return err
}

// IsFavoriteByGlobalID 按 global_id 检查是否已收藏（跨所有源）
func IsFavoriteByGlobalID(globalID int) bool {
	var count int
	_ = instance.Get(&count, `SELECT COUNT(1) FROM favorites WHERE global_id = ?`, globalID)
	return count > 0
}

// FavWithVideo 收藏条目（含视频信息）
//
// 字段按"卡片要显示什么"来选，不是按收藏表有什么：前端收藏页以前对每条收藏再发一次
// GetVideoDetail 才拿到片名/封面/备注，24 条就是 24 次串行远程请求，页面一直转圈。
// 这些字段 global_video 和 source_videos 里本来就有，一条 JOIN 就够。
type FavWithVideo struct {
	Id         int    `json:"id" db:"id"`
	GlobalID   int    `json:"global_id" db:"global_id"`
	SourceKey  string `json:"source_key" db:"source_key"`
	VodId      string `json:"vod_id" db:"vod_id"`
	VodName    string `json:"vod_name" db:"vod_name"`
	VodPic     string `json:"vod_pic" db:"vod_pic"`
	TypeName   string `json:"type_name" db:"type_name"`
	VodRemarks string `json:"vod_remarks" db:"vod_remarks"`
	VodYear    string `json:"vod_year" db:"vod_year"`
	VodArea    string `json:"vod_area" db:"vod_area"`
	CreatedAt  string `json:"created_at" db:"created_at"`
}

// catalogProjectionFilter 决定一条收藏/历史还能不能在列表里出现。
//
// 收藏与历史按 global_id 存，源坐标只是"从哪打开"的线索，所以三种情况都要放过：
// 该坐标压根没被采集进目录（只有全局元数据，仍然可以看）、目录行活着且类型可见。
// 反过来，被软删进回收站的目录行必须把条目一起藏掉 —— 过去这里只查了类型可见性，
// 没查 lifecycle_state，删掉的视频继续挂在收藏和历史里。
func catalogProjectionFilter(alias string) string {
	return alias + ".id IS NULL OR (" + alias + ".lifecycle_state = 'active' AND " + catalogTypeVisibilityClause(alias) + ")"
}

// GetFavorites 分页获取收藏列表（JOIN global_video + source_videos，一次给齐卡片字段）
func GetFavorites(page, pageSize int) ([]FavWithVideo, error) {
	var results []FavWithVideo
	q := `SELECT f.id, f.global_id, f.source_key, f.vod_id,
		CASE WHEN g.vod_name <> '' THEN g.vod_name ELSE COALESCE(sv.vod_name, '') END as vod_name,
		CASE WHEN g.pic <> '' THEN g.pic ELSE COALESCE(sv.vod_pic, '') END as vod_pic,
		COALESCE(sv.type_name, '') as type_name,
		COALESCE(sv.vod_remarks, '') as vod_remarks,
		COALESCE(sv.vod_year, '') as vod_year,
		COALESCE(sv.vod_area, '') as vod_area,
		f.created_at
		FROM favorites f
		JOIN global_video g ON f.global_id = g.id
		LEFT JOIN source_videos sv ON sv.source_key = f.source_key AND sv.source_vod_id = f.vod_id
		WHERE ` + catalogProjectionFilter("sv") + `
		ORDER BY f.created_at DESC LIMIT ? OFFSET ?`
	err := instance.Select(&results, q, pageSize, (page-1)*pageSize)
	return results, err
}

// --- Watch History (基于 global_id) ---

// SaveWatchHistoryByIdentity saves a source episode under an already resolved
// catalog identity. The unique key is source-aware, so all read/delete paths
// use the same complete coordinate.
func SaveWatchHistoryByIdentity(globalID int64, sourceKey, vodID string, epNum int, position float64) error {
	if globalID <= 0 || strings.TrimSpace(sourceKey) == "" || strings.TrimSpace(vodID) == "" || epNum <= 0 {
		return fmt.Errorf("invalid watch history identity")
	}
	if math.IsNaN(position) || math.IsInf(position, 0) || position < 0 {
		return fmt.Errorf("invalid watch position")
	}
	_, err := instance.Exec(`INSERT INTO watch_history (global_id, source_key, vod_id, ep_num, position, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(global_id, source_key, ep_num) DO UPDATE SET
		vod_id=excluded.vod_id, position=excluded.position, updated_at=CURRENT_TIMESTAMP`, globalID, sourceKey, vodID, epNum, position)
	return err
}

func GetWatchHistoryByIdentity(globalID int64, sourceKey, vodID string, epNum int) (float64, error) {
	var position float64
	err := instance.Get(&position, `SELECT position FROM watch_history WHERE global_id=? AND source_key=? AND vod_id=? AND ep_num=?`, globalID, sourceKey, vodID, epNum)
	return position, err
}

// HistEntry 观看历史条目
type HistEntry struct {
	GlobalID  int     `json:"global_id" db:"global_id"`
	SourceKey string  `json:"source_key" db:"source_key"`
	VodId     string  `json:"vod_id" db:"vod_id"`
	VodName   string  `json:"vod_name" db:"vod_name"`
	VodPic    string  `json:"vod_pic" db:"vod_pic"`
	EpNum     int     `json:"ep_num" db:"ep_num"`
	Position  float64 `json:"position" db:"position"`
	UpdatedAt string  `json:"updated_at" db:"updated_at"`
}

// GetRecentHistory 获取最近观看历史
func GetRecentHistory(limit int) ([]HistEntry, error) {
	var entries []HistEntry
	q := `SELECT h.global_id, h.source_key, h.vod_id, g.vod_name, g.pic as vod_pic, h.ep_num, h.position, h.updated_at
		FROM watch_history h
		JOIN global_video g ON h.global_id = g.id
		LEFT JOIN source_videos sv ON sv.source_key = h.source_key AND sv.source_vod_id = h.vod_id
		WHERE ` + catalogProjectionFilter("sv") + `
		ORDER BY h.updated_at DESC LIMIT ?`
	err := instance.Select(&entries, q, limit)
	return entries, err
}

// DeleteHistoryItem 删除单条观看历史
func DeleteHistoryItem(globalID int, epNum int) error {
	_, err := instance.Exec(`DELETE FROM watch_history WHERE global_id = ? AND ep_num = ?`, globalID, epNum)
	return err
}

func DeleteHistoryItemByIdentity(globalID int64, sourceKey, vodID string, epNum int) error {
	_, err := instance.Exec(`DELETE FROM watch_history WHERE global_id=? AND source_key=? AND vod_id=? AND ep_num=?`, globalID, sourceKey, vodID, epNum)
	return err
}

// DeleteHistoryByVideo 按 source_key+vod_id 删除（兼容旧调用）
func DeleteHistoryByVideo(sourceKey string, vodId string) error {
	_, err := instance.Exec(`DELETE FROM watch_history WHERE source_key = ? AND vod_id = ?`, sourceKey, vodId)
	return err
}

// ClearAllHistory 清空全部观看历史
func ClearAllHistory() (int64, error) {
	res, err := instance.Exec(`DELETE FROM watch_history`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func GetWatchedEpisodesByIdentity(globalID int64, sourceKey, vodID string) ([]int, error) {
	var epNums []int
	err := instance.Select(&epNums, `SELECT ep_num FROM watch_history WHERE global_id=? AND source_key=? AND vod_id=? ORDER BY ep_num ASC`, globalID, sourceKey, vodID)
	return epNums, err
}
