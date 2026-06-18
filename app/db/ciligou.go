package db

import (
	"cczjVideo/app/applog"
	"fmt"
	"time"
)

// GetVideosMissingMagnetLink 获取缺少磁力链接的记录（排除冷却期内，且类型在启用列表中）
func GetVideosMissingMagnetLink(limit int) ([]*GlobalVideoRow, error) {
	if limit <= 0 {
		limit = 3
	}
	var rows []*GlobalVideoRow
	q := `SELECT gv.* FROM global_video gv
		INNER JOIN global_types gt ON gt.type_name = 
			(SELECT GROUP_CONCAT(DISTINCT v.type_name) FROM v_xxx WHERE 1=0 OR 1=1)
		WHERE (gv.magnet_link = '' OR gv.magnet_link IS NULL)
		AND (gv.magnet_cooldown_until IS NULL OR gv.magnet_cooldown_until < ?)
		ORDER BY gv.id ASC LIMIT ?`

	// 上面的查询比较复杂，简化为：获取所有需要磁力链接的 global_video 记录，
	// 然后在爬虫层按类型过滤
	q = `SELECT gv.* FROM global_video gv
		WHERE (gv.magnet_link = '' OR gv.magnet_link IS NULL)
		AND (gv.magnet_cooldown_until IS NULL OR gv.magnet_cooldown_until < ?)
		ORDER BY gv.id ASC LIMIT ?`
	err := instance.Select(&rows, q,
		time.Now().Format("2006-01-02 15:04:05"), limit)
	if err != nil {
		applog.Error("[Ciligou] GetVideosMissingMagnetLink query failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// SaveMagnetLink 保存磁力链接到 global_video
func SaveMagnetLink(globalID int, magnetLink string) error {
	_, err := instance.Exec(`UPDATE global_video SET magnet_link = ?, magnet_search_failures = 0, magnet_cooldown_until = NULL, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		magnetLink, globalID)
	if err != nil {
		applog.Error("[Ciligou] SaveMagnetLink failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// SetMagnetCooldown 为指定 global_id 设置磁力链接冷却期
func SetMagnetCooldown(globalID int) error {
	cooldownUntil := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
	_, err := instance.Exec(`UPDATE global_video SET magnet_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		cooldownUntil, globalID)
	if err != nil {
		applog.Error("[Ciligou] SetMagnetCooldown failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// IncrementMagnetSearchFailures 增加磁力链接搜索失败计数
func IncrementMagnetSearchFailures(vodName string) error {
	// 使用智能匹配获取 global_id（避免直接 INSERT 导致唯一索引冲突）
	globalID, err := GetOrCreateGlobalID(vodName)
	if err != nil {
		applog.Error("[Ciligou] IncrementMagnetSearchFailures GetOrCreateGlobalID failed for '%s': %v", vodName, err)
		return err
	}

	var currentFailures int
	_ = instance.Get(&currentFailures, `SELECT magnet_search_failures FROM global_video WHERE id = ?`, globalID)

	newCount := currentFailures + 1
	if newCount >= 5 {
		cooldownUntil := time.Now().Add(24 * time.Hour).Format("2006-01-02 15:04:05")
		_, err = instance.Exec(`UPDATE global_video SET magnet_search_failures = ?, magnet_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			newCount, cooldownUntil, globalID)
		if err != nil {
			applog.Error("[Ciligou] IncrementMagnetSearchFailures cooldown failed for '%s': %v", vodName, err)
		}
		applog.Info("[Ciligou] Magnet search cooldown activated for '%s' (failures=%d, until=%s)", vodName, newCount, cooldownUntil)
	} else {
		_, err = instance.Exec(`UPDATE global_video SET magnet_search_failures = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
			newCount, globalID)
		if err != nil {
			applog.Error("[Ciligou] IncrementMagnetSearchFailures update failed for '%s': %v", vodName, err)
		}
	}
	return err
}

// GetGlobalVideoTypeNames 获取某个 global_video 记录关联的所有 type_name
// 通过查询所有源视频表来获取
func GetGlobalVideoTypeNames(vodName string) ([]string, error) {
	sources, err := getAllSourceKeys()
	if err != nil {
		return nil, err
	}

	typeNames := make(map[string]bool)
	for _, src := range sources {
		tn := VideoTableName(src.SourceKey)
		var names []string
		q := fmt.Sprintf(`SELECT DISTINCT type_name FROM %s WHERE vod_name = ?`, tn)
		if err := instance.Select(&names, q, vodName); err != nil {
			continue
		}
		for _, n := range names {
			if n != "" {
				typeNames[n] = true
			}
		}
	}

	result := make([]string, 0, len(typeNames))
	for n := range typeNames {
		result = append(result, n)
	}
	return result, nil
}

// getAllSourceKeys 获取所有源的 source_key（内部使用，避免与 GetAllSources 冲突）
func getAllSourceKeys() ([]struct {
	SourceKey string `db:"source_key"`
	Name      string `db:"name"`
}, error) {
	var rows []struct {
		SourceKey string `db:"source_key"`
		Name      string `db:"name"`
	}
	err := instance.Select(&rows, `SELECT source_key, name FROM sources`)
	return rows, err
}