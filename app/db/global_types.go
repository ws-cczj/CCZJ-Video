package db

import (
	"cczjVideo/app/applog"
	"fmt"
	"strings"
	"unicode/utf8"
)

// GlobalTypeRow 全局类型行
type GlobalTypeRow struct {
	Id              int    `db:"id"`
	TypeName        string `db:"type_name"`
	CollectEnabled  int    `db:"collect_enabled"`
	MagnetEnabled   int    `db:"magnet_enabled"`
	Sort            int    `db:"sort"`
	CreatedAt       string `db:"created_at"`
}

// GetAllGlobalTypes 获取所有全局类型
func GetAllGlobalTypes() ([]*GlobalTypeRow, error) {
	var rows []*GlobalTypeRow
	err := instance.Select(&rows, `SELECT * FROM global_types ORDER BY sort ASC, id ASC`)
	if err != nil {
		applog.Error("[GlobalTypes] GetAllGlobalTypes failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// GetEnabledGlobalTypes 获取所有启用了磁力链接获取的类型
func GetEnabledGlobalTypes() ([]*GlobalTypeRow, error) {
	var rows []*GlobalTypeRow
	err := instance.Select(&rows, `SELECT * FROM global_types WHERE magnet_enabled = 1 ORDER BY sort ASC, id ASC`)
	if err != nil {
		applog.Error("[GlobalTypes] GetEnabledGlobalTypes failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// SetGlobalTypeCollectEnabled 设置某个类型的采集状态
func SetGlobalTypeCollectEnabled(typeName string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := instance.Exec(`UPDATE global_types SET collect_enabled = ? WHERE type_name = ?`, val, typeName)
	if err != nil {
		applog.Error("[GlobalTypes] SetGlobalTypeCollectEnabled failed for '%s': %v", typeName, err)
	}
	return err
}

// SetGlobalTypeMagnetEnabled 设置某个类型的磁力链接获取状态
func SetGlobalTypeMagnetEnabled(typeName string, enabled bool) error {
	val := 0
	if enabled {
		val = 1
	}
	_, err := instance.Exec(`UPDATE global_types SET magnet_enabled = ? WHERE type_name = ?`, val, typeName)
	if err != nil {
		applog.Error("[GlobalTypes] SetGlobalTypeMagnetEnabled failed for '%s': %v", typeName, err)
	}
	return err
}

// normalizeTypeName 类型名归一化：去空白 + 小写 + 去常见后缀 + 同义词映射
func normalizeTypeName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), "")
	s = strings.ToLower(s)
	// 去除常见后缀/修饰词
	s = strings.TrimSuffix(s, "类")
	s = strings.TrimSuffix(s, "片")
	s = strings.TrimSuffix(s, "剧")
	// 同义词映射
	synonyms := map[string]string{
		"连续剧": "电视剧",
		"电视":  "电视剧",
		"动画":  "动漫",
		"电影":  "电影",
		"记录片": "纪录片",
	}
	if v, ok := synonyms[s]; ok {
		return v
	}
	return s
}

// getAllGlobalTypeCandidates 获取所有全局类型候选项（id + type_name）
func getAllGlobalTypeCandidates() ([]struct {
	Id       int64  `db:"id"`
	TypeName string `db:"type_name"`
}, error) {
	var candidates []struct {
		Id       int64  `db:"id"`
		TypeName string `db:"type_name"`
	}
	err := instance.Select(&candidates, `SELECT id, type_name FROM global_types`)
	return candidates, err
}

// UpsertGlobalType 插入或更新全局类型（从采集数据中同步）
// 先做归一化匹配，若已存在同义类型则不重复插入
func UpsertGlobalType(typeName string) error {
	if typeName == "" {
		return nil
	}
	norm := normalizeTypeName(typeName)
	candidates, _ := getAllGlobalTypeCandidates()
	for _, c := range candidates {
		if normalizeTypeName(c.TypeName) == norm {
			return nil // 已存在同义类型
		}
	}
	_, err := instance.Exec(`INSERT OR IGNORE INTO global_types (type_name) VALUES (?)`, typeName)
	if err != nil {
		applog.Error("[GlobalTypes] UpsertGlobalType failed for '%s': %v", typeName, err)
	}
	return err
}

// GetOrCreateGlobalTypeId 根据类型名查询或创建全局类型，返回其 id
// 匹配策略：精确 → 归一化 → 短字符串包含关系模糊 → 插入新条目
// 采集入库时必须调用此函数，确保 type_id 为 global_types.id 而非源站原始值
func GetOrCreateGlobalTypeId(typeName string) (int64, error) {
	if typeName == "" {
		return 0, fmt.Errorf("type_name is empty")
	}

	// 1. 精确匹配
	var id int64
	err := instance.Get(&id, `SELECT id FROM global_types WHERE type_name = ?`, typeName)
	if err == nil && id > 0 {
		return id, nil
	}

	// 2. 归一化匹配（遍历所有候选，Go 侧比较）
	norm := normalizeTypeName(typeName)
	candidates, _ := getAllGlobalTypeCandidates()
	for _, c := range candidates {
		if normalizeTypeName(c.TypeName) == norm {
			applog.Info("[GlobalTypes] 归一化匹配: %q -> id=%d (%q)", typeName, c.Id, c.TypeName)
			return c.Id, nil
		}
	}

	// 3. 短字符串包含关系模糊匹配（类型名通常 ≤10 字，用包含关系而非编辑距离）
	for _, c := range candidates {
		cn := normalizeTypeName(c.TypeName)
		if cn == "" {
			continue
		}
		// 包含关系：一个完全包含另一个且长度差不超过 1
		if strings.Contains(cn, norm) || strings.Contains(norm, cn) {
			lenDiff := abs(utf8.RuneCountInString(cn) - utf8.RuneCountInString(norm))
			if lenDiff <= 1 {
				applog.Info("[GlobalTypes] 模糊匹配: %q -> id=%d (%q)", typeName, c.Id, c.TypeName)
				return c.Id, nil
			}
		}
	}

	// 4. 不存在则插入
	res, err := instance.Exec(`INSERT OR IGNORE INTO global_types (type_name) VALUES (?)`, typeName)
	if err != nil {
		return 0, fmt.Errorf("insert global_types: %w", err)
	}
	if lid, err := res.LastInsertId(); err == nil && lid > 0 {
		return lid, nil
	}
	// INSERT OR IGNORE 可能因 UNIQUE 冲突而跳过，再查一次
	err = instance.Get(&id, `SELECT id FROM global_types WHERE type_name = ?`, typeName)
	if err != nil {
		return 0, fmt.Errorf("select global_types after insert: %w", err)
	}
	return id, nil
}

// abs 返回整数绝对值
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// SyncGlobalTypesFromSources 从所有源的旧 t_* 类型表中迁移数据到全局类型表
// 仅在从旧版本迁移时使用；新版本采集数据直接写入 global_types
func SyncGlobalTypesFromSources() (int, error) {
	// 获取所有源
	sources, err := getAllSourceKeys()
	if err != nil {
		return 0, fmt.Errorf("get sources: %w", err)
	}

	total := 0
	for _, src := range sources {
		tn := "t_" + src.SourceKey
		rows, qerr := instance.Queryx(fmt.Sprintf(`SELECT DISTINCT type_name FROM %s`, tn))
		if qerr != nil {
			// 表可能不存在，跳过
			continue
		}
		for rows.Next() {
			var typeName string
			if err := rows.Scan(&typeName); err != nil {
				continue
			}
			if typeName != "" {
				if err := UpsertGlobalType(typeName); err == nil {
					total++
				}
			}
		}
		rows.Close()
	}
	return total, nil
}

// GetEnabledCollectTypes 获取所有启用了采集的类型
func GetEnabledCollectTypes() ([]*GlobalTypeRow, error) {
	var rows []*GlobalTypeRow
	err := instance.Select(&rows, `SELECT * FROM global_types WHERE collect_enabled = 1 ORDER BY sort ASC, id ASC`)
	if err != nil {
		applog.Error("[GlobalTypes] GetEnabledCollectTypes failed: %v", err)
		return nil, err
	}
	return rows, nil
}

// IsTypeCollectEnabled 检查某个类型是否启用了采集
// 默认值：类型不存在时返回 true（默认启用采集）
func IsTypeCollectEnabled(typeName string) bool {
	var count int
	err := instance.Get(&count, `SELECT COUNT(*) FROM global_types WHERE type_name = ?`, typeName)
	if err != nil || count == 0 {
		return true
	}
	var enabled int
	err = instance.Get(&enabled, `SELECT collect_enabled FROM global_types WHERE type_name = ?`, typeName)
	if err != nil {
		return true
	}
	return enabled == 1
}

// IsTypeMagnetEnabled 检查某个类型是否启用了磁力链接获取
// 默认值：类型不存在时返回 false（默认禁用磁力）
func IsTypeMagnetEnabled(typeName string) bool {
	var count int
	err := instance.Get(&count, `SELECT COUNT(*) FROM global_types WHERE type_name = ?`, typeName)
	if err != nil || count == 0 {
		return false
	}
	var enabled int
	err = instance.Get(&enabled, `SELECT magnet_enabled FROM global_types WHERE type_name = ?`, typeName)
	if err != nil {
		return false
	}
	return enabled == 1
}