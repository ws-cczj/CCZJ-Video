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

// AddFavorite 添加收藏：通过 vod_name 获取 global_id，然后写入 favorites 表
func AddFavorite(sourceKey string, vodId string, vodName string) error {
	globalID, err := GetOrCreateGlobalID(vodName, 0)
	if err != nil {
		return err
	}
	return AddFavoriteByIdentity(globalID, sourceKey, vodId)
}

// RemoveFavoriteByGlobalID 按 global_id 删除所有源的收藏
func RemoveFavoriteByGlobalID(globalID int, sourceKey string) error {
	_ = sourceKey // retained for generated binding compatibility
	_, err := instance.Exec(`DELETE FROM favorites WHERE global_id = ?`, globalID)
	return err
}

// RemoveFavorite 按 vod_name 删除收藏
func RemoveFavorite(vodName string, sourceKey string) error {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return err
	}
	return RemoveFavoriteByGlobalID(row.Id, sourceKey)
}

// IsFavorite 按 vod_name 检查是否已收藏（跨所有源）
func IsFavorite(vodName string, sourceKey string) bool {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return false
	}
	return IsFavoriteByGlobalID(row.Id)
}

// IsFavoriteByGlobalID 按 global_id 检查是否已收藏（跨所有源）
func IsFavoriteByGlobalID(globalID int) bool {
	var count int
	_ = instance.Get(&count, `SELECT COUNT(1) FROM favorites WHERE global_id = ?`, globalID)
	return count > 0
}

// FavWithVideo 收藏条目（含视频信息）
type FavWithVideo struct {
	Id        int    `json:"id" db:"id"`
	GlobalID  int    `json:"global_id" db:"global_id"`
	SourceKey string `json:"source_key" db:"source_key"`
	VodId     string `json:"vod_id" db:"vod_id"`
	VodName   string `json:"vod_name" db:"vod_name"`
	VodPic    string `json:"vod_pic" db:"vod_pic"`
	CreatedAt string `json:"created_at" db:"created_at"`
}

// GetFavorites 分页获取收藏列表（JOIN global_video）
func GetFavorites(page, pageSize int) ([]FavWithVideo, error) {
	var results []FavWithVideo
	q := `SELECT f.id, f.global_id, f.source_key, f.vod_id, g.vod_name, g.pic as vod_pic, f.created_at
		FROM favorites f
		JOIN global_video g ON f.global_id = g.id
		LEFT JOIN source_videos sv ON sv.source_key = f.source_key AND sv.source_vod_id = f.vod_id
		WHERE sv.id IS NULL OR ` + catalogTypeVisibilityClause("sv") + `
		ORDER BY f.created_at DESC LIMIT ? OFFSET ?`
	err := instance.Select(&results, q, pageSize, (page-1)*pageSize)
	return results, err
}

// --- Watch History (基于 global_id) ---

// SaveWatchHistory 保存观看进度
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

func SaveWatchHistory(sourceKey string, vodId string, vodName string, epNum int, position float64) error {
	globalID, err := GetOrCreateGlobalID(vodName, 0)
	if err != nil {
		return err
	}
	return SaveWatchHistoryByIdentity(globalID, sourceKey, vodId, epNum, position)
}

// GetWatchHistory 获取某视频某集的观看位置
func GetWatchHistory(vodName string, epNum int) (float64, error) {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return 0, err
	}
	var pos float64
	err = instance.Get(&pos, `SELECT position FROM watch_history WHERE global_id = ? AND ep_num = ?`, row.Id, epNum)
	return pos, err
}

// GetWatchHistoryByVod 获取某视频所有集的观看位置
func GetWatchHistoryByVod(vodName string) (map[int]float64, error) {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return nil, err
	}
	type entry struct {
		EpNum    int     `db:"ep_num"`
		Position float64 `db:"position"`
	}
	var entries []entry
	err = instance.Select(&entries, `SELECT ep_num, position FROM watch_history WHERE global_id = ? ORDER BY ep_num`, row.Id)
	if err != nil {
		return nil, err
	}
	result := make(map[int]float64)
	for _, e := range entries {
		result[e.EpNum] = e.Position
	}
	return result, nil
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
		WHERE sv.id IS NULL OR ` + catalogTypeVisibilityClause("sv") + `
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

// DeleteHistoryByVodName 删除某个视频的全部观看历史
func DeleteHistoryByVodName(vodName string) error {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return err
	}
	_, err = instance.Exec(`DELETE FROM watch_history WHERE global_id = ?`, row.Id)
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

// GetWatchedEpisodes 返回指定视频已观看的所有集数列表
func GetWatchedEpisodes(vodName string) ([]int, error) {
	row, err := GetGlobalVideoByName(vodName)
	if err != nil {
		return nil, err
	}
	var epNums []int
	err = instance.Select(&epNums, `SELECT ep_num FROM watch_history WHERE global_id = ? ORDER BY ep_num ASC`, row.Id)
	if err != nil {
		return nil, err
	}
	return epNums, nil
}

func GetWatchedEpisodesByIdentity(globalID int64, sourceKey, vodID string) ([]int, error) {
	var epNums []int
	err := instance.Select(&epNums, `SELECT ep_num FROM watch_history WHERE global_id=? AND source_key=? AND vod_id=? ORDER BY ep_num ASC`, globalID, sourceKey, vodID)
	return epNums, err
}

// GetWatchedEpisodesBySrc 兼容旧 API（按 source_key+vod_id）
func GetWatchedEpisodesBySrc(sourceKey string, vodId string) ([]int, error) {
	var epNums []int
	err := instance.Select(&epNums, `SELECT ep_num FROM watch_history WHERE source_key = ? AND vod_id = ? ORDER BY ep_num ASC`, sourceKey, vodId)
	if err != nil {
		return nil, err
	}
	return epNums, nil
}
