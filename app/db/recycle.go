package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// RecycleItem 是回收站里的一行：被软删除（lifecycle_state='deleted'）的目录条目。
// 回收站跨源展示，所以每行自带源坐标，不能依赖调用方当前选中的源。
type RecycleItem struct {
	ID        int    `db:"id" json:"id"`
	SourceKey string `db:"source_key" json:"source_key"`
	VodId     string `db:"vod_id" json:"vod_id"`
	GlobalId  int64  `db:"global_id" json:"global_id"`
	VodName   string `db:"vod_name" json:"vod_name"`
	VodPic    string `db:"vod_pic" json:"vod_pic"`
	VodYear   string `db:"vod_year" json:"vod_year"`
	TypeName  string `db:"type_name" json:"type_name"`
	DeletedAt string `db:"deleted_at" json:"deleted_at"`
}

// recycleColumns 是回收站一行的取列清单。source_vod_id 必须 AS 成 vod_id 才能落到
// RecycleItem.VodId 上；时间列走 strftime 读成文本，避开驱动把 DATETIME 改写成 RFC3339。
// 末尾的 Z 不能省：updated_at 是 CURRENT_TIMESTAMP 写进去的 UTC，少了时区标记前端
// formatTime 会按本地时间解析，删除时间整整早 8 小时。
const recycleColumns = `id, source_key, source_vod_id AS vod_id, global_id, vod_name, vod_pic, vod_year, type_name,
	strftime('%Y-%m-%dT%H:%M:%SZ', updated_at) AS deleted_at`

// ListRecycleBin 按删除时间倒序列出软删除条目。这里故意不套
// catalogTypeVisibilityClause：用户在「视频类型」里关掉某个类型后，回收站里的
// 对应条目仍然要能看见并恢复，否则等于悄悄把数据锁在库里。
func ListRecycleBin(sourceKey string, page, pageSize int) ([]RecycleItem, int, error) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 200 {
		pageSize = 50
	}
	where := []string{"lifecycle_state='deleted'"}
	args := []any{}
	if sourceKey != "" {
		where = append(where, "source_key=?")
		args = append(args, sourceKey)
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := instance.Get(&total, "SELECT COUNT(*) FROM source_videos WHERE "+clause, args...); err != nil {
		return nil, 0, err
	}
	query := `SELECT ` + recycleColumns + `
		FROM source_videos WHERE ` + clause + ` ORDER BY updated_at DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, pageSize, (page-1)*pageSize)

	var items []RecycleItem
	if err := instance.Select(&items, query, args...); err != nil {
		return nil, 0, fmt.Errorf("list recycle bin: %w", err)
	}
	return items, total, nil
}

// GetRecycleItem 读一条回收站条目。不能复用 GetCatalogItem：它过滤 lifecycle_state 和
// 类型可见性，而恢复/彻底删除恰恰要在行是 deleted 时拿到它的 global_id 去失效缓存。
func GetRecycleItem(sourceKey, vodID string) (*RecycleItem, error) {
	var item RecycleItem
	err := instance.Get(&item, `SELECT `+recycleColumns+`
		FROM source_videos WHERE source_key=? AND source_vod_id=? AND lifecycle_state='deleted'`, sourceKey, vodID)
	if err != nil {
		return nil, err
	}
	return &item, nil
}

// RestoreCatalogVideo 把一条软删除的目录行放回视频库。
func RestoreCatalogVideo(sourceKey, vodID string) error {
	result, err := instance.Exec(`UPDATE source_videos SET lifecycle_state='active', updated_at=CURRENT_TIMESTAMP
		WHERE source_key=? AND source_vod_id=? AND lifecycle_state='deleted'`, sourceKey, vodID)
	if err != nil {
		return err
	}
	return requireRecycleRow(result)
}

// PurgeCatalogVideo 彻底删除一条已在回收站里的目录行。带 lifecycle_state 条件，
// 所以这个入口不可能误删还在库里的条目。
func PurgeCatalogVideo(sourceKey, vodID string) (int, error) {
	result, err := instance.Exec(`DELETE FROM source_videos WHERE source_key=? AND source_vod_id=? AND lifecycle_state='deleted'`, sourceKey, vodID)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if affected == 0 {
		return 0, fmt.Errorf("recycle bin has no %s:%s", sourceKey, vodID)
	}
	return int(affected), nil
}

// ClearRecycleBin 彻底删除回收站里的全部条目（可按源过滤），返回删除行数。
// 只碰 lifecycle_state='deleted' 的行，视频库本体不受影响。
func ClearRecycleBin(sourceKey string) (int, error) {
	query := `DELETE FROM source_videos WHERE lifecycle_state='deleted'`
	args := []any{}
	if sourceKey != "" {
		query += ` AND source_key=?`
		args = append(args, sourceKey)
	}
	result, err := instance.Exec(query, args...)
	if err != nil {
		return 0, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// RecycleSourceKeys 列出回收站里涉及哪些源。清空前调用一次，删除之后就能按源
// 精确失效缓存，不用把每一行的 global_id 都翻出来。
func RecycleSourceKeys(sourceKey string) ([]string, error) {
	query := `SELECT DISTINCT source_key FROM source_videos WHERE lifecycle_state='deleted'`
	args := []any{}
	if sourceKey != "" {
		query += ` AND source_key=?`
		args = append(args, sourceKey)
	}
	var keys []string
	if err := instance.Select(&keys, query, args...); err != nil {
		return nil, err
	}
	return keys, nil
}

func requireRecycleRow(result sql.Result) error {
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return fmt.Errorf("recycle bin has no such entry")
	}
	return nil
}
