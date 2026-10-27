package db

import (
	"cczjVideo/app/applog"
	"database/sql/driver"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
	"modernc.org/sqlite"
)

func init() {
	// Reads and incoming pages must compare aliases identically, including old
	// rows with missing/wrong global_type_id. This pure function does no DB I/O.
	sqlite.MustRegisterDeterministicScalarFunction("cczj_type_key", 1, func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
		name, _ := args[0].(string)
		return normalizeTypeName(name), nil
	})
}

// An explicit denial wins over an enabled spelling variant and stale identity
// mapping. Retained rows remain intact, but cannot leak through another reader.
func catalogTypeVisibilityClause(alias string) string {
	if strings.TrimSpace(alias) == "" {
		alias = "source_videos"
	}
	return globalTypeVisibilityClause(alias) + fmt.Sprintf(` AND NOT EXISTS (
	 SELECT 1 FROM source_types policy_source_type WHERE policy_source_type.source_key=%[1]s.source_key
	 AND policy_source_type.source_type_id=%[1]s.source_type_id AND NOT (%[2]s))`, alias, globalTypeVisibilityClause("policy_source_type"))
}

func globalTypeVisibilityClause(alias string) string {
	return fmt.Sprintf(`NOT EXISTS (SELECT 1 FROM global_types denied_type WHERE denied_type.collect_enabled=0 AND (
		denied_type.id=%[1]s.global_type_id
		OR cczj_type_key(denied_type.type_name)=cczj_type_key(%[1]s.type_name)
		OR cczj_type_key(denied_type.type_name)=(SELECT cczj_type_key(mapped_type.type_name) FROM global_types mapped_type WHERE mapped_type.id=%[1]s.global_type_id)))`, alias)
}

// GlobalTypeRow 全局类型行
type GlobalTypeRow struct {
	Id             int    `db:"id"`
	TypeName       string `db:"type_name"`
	CollectEnabled int    `db:"collect_enabled"`
	Sort           int    `db:"sort"`
	CreatedAt      string `db:"created_at"`
}

// GetAllGlobalTypes 获取所有全局类型
func GetAllGlobalTypes() ([]*GlobalTypeRow, error) {
	var rows []*GlobalTypeRow
	err := instance.Select(&rows, `SELECT id,type_name,sort,created_at,CASE WHEN EXISTS(
	 SELECT 1 FROM global_types denied WHERE denied.collect_enabled=0
	 AND cczj_type_key(denied.type_name)=cczj_type_key(global_types.type_name)) THEN 0 ELSE collect_enabled END AS collect_enabled
	 FROM global_types ORDER BY collect_enabled DESC, sort ASC, type_name COLLATE NOCASE ASC, id ASC`)
	if err != nil {
		applog.Error("[GlobalTypes] GetAllGlobalTypes failed: %v", err)
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
	_, err := instance.Exec(`UPDATE global_types SET collect_enabled = ? WHERE cczj_type_key(type_name) = ?`, val, normalizeTypeName(typeName))
	if err != nil {
		applog.Error("[GlobalTypes] SetGlobalTypeCollectEnabled failed for '%s': %v", typeName, err)
	}
	return err
}

// typeNameSynonyms 是同义类型名。键必须写成「剥完后缀之后」的样子，因为归一化
// 是先剥后缀再查表：「连续剧」剥成「连续」，所以表里存的是「连续」。
//
// 顺序很关键。原先的实现先剥后缀、再查一张键为「连续剧」「电视」「动画」「记录片」
// 的表，而那些键自己早就被同一次剥后缀啃掉了（「连续」「电」「动」「记录」），
// 于是映射永远命中不了，同义词表形同虚设，同一个类型在不同源站手里各建各的
// global_types 行。
var typeNameSynonyms = map[string]string{
	"连续": "电视",
	"动画": "动漫",
	"记录": "纪录",
}

// normalizeTypeName 类型名归一化：去空白 + 小写 + 去常见后缀 + 同义词映射。
// 归一化结果只用于「是不是同一个类型」的判定，写库的仍是用户看到的原名。
func normalizeTypeName(s string) string {
	s = strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(s)), ""))
	s = typeNameTraditional.Replace(s)
	s = stripTypeNameSuffixes(s)
	if canonical, ok := typeNameSynonyms[s]; ok {
		return canonical
	}
	return s
}

var typeNameTraditional = strings.NewReplacer("倫", "伦", "類", "类", "劇", "剧", "紀", "纪", "錄", "录", "動", "动", "畫", "画", "電", "电", "視", "视", "綜", "综", "藝", "艺")

var defaultBlockedTypeNames = []string{"福利", "伦理片", "倫理片"}

var defaultBlockedTypeKeys = func() []string {
	var keys []string
	seen := map[string]bool{}
	for _, name := range defaultBlockedTypeNames {
		key := normalizeTypeName(name)
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys
}()

// Keep all three built-in labels visible before any source has supplied types.
// Missing aliases inherit a saved choice; a fresh group starts disabled.
func seedDefaultBlockedTypes(tx *sqlx.Tx) error {
	for _, name := range defaultBlockedTypeNames {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO global_types(type_name,collect_enabled)
		 SELECT ?,COALESCE(MIN(collect_enabled),0) FROM global_types WHERE cczj_type_key(type_name)=?`, name, normalizeTypeName(name)); err != nil {
			return fmt.Errorf("seed default video type %q: %w", name, err)
		}
	}
	return nil
}

// Defaults apply to a new category; an explicit saved choice still takes precedence.
func initialTypeCollectEnabled(name string) int {
	key := normalizeTypeName(name)
	for _, blocked := range defaultBlockedTypeKeys {
		if key == blocked {
			return 0
		}
	}
	return 1
}

// stripTypeNameSuffixes 剥掉「类/片/剧」这一类纯修饰后缀，每个至多剥一次：
// 「电视剧类」剥成「电视」，「卡通片」剥成「卡通」。再往下就要把词根啃掉了，
// 所以「电」「动」这种不在这条路上。
func stripTypeNameSuffixes(s string) string {
	for _, suffix := range []string{"类", "片", "剧"} {
		s = strings.TrimSuffix(s, suffix)
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
	_, err := instance.Exec(`INSERT OR IGNORE INTO global_types (type_name,collect_enabled) VALUES (?,?)`, typeName, initialTypeCollectEnabled(typeName))
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
	res, err := instance.Exec(`INSERT OR IGNORE INTO global_types (type_name,collect_enabled) VALUES (?,?)`, typeName, initialTypeCollectEnabled(typeName))
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
		rows, qerr := instance.Queryx(`SELECT DISTINCT type_name FROM source_types WHERE source_key=?`, src.SourceKey)
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
// Runtime decisions read saved switches only. Defaults belong to row creation.
func IsTypeCollectEnabled(typeName string) bool {
	typeName = strings.TrimSpace(typeName)
	if typeName == "" {
		return true
	}
	var enabled bool
	if err := instance.Get(&enabled, `SELECT COALESCE(MIN(collect_enabled),1) FROM global_types WHERE cczj_type_key(type_name)=?`, normalizeTypeName(typeName)); err != nil {
		applog.Error("[GlobalTypes] cannot read type policy: %v", err)
		return false
	}
	return enabled
}

func isGlobalTypeCollectEnabled(globalTypeID int64, typeName string) bool {
	if !IsTypeCollectEnabled(typeName) {
		return false
	}
	if globalTypeID > 0 {
		var mapped string
		if err := instance.Get(&mapped, `SELECT type_name FROM global_types WHERE id = ?`, globalTypeID); err == nil {
			return IsTypeCollectEnabled(mapped)
		}
	}
	return true
}

// getAllSourceKeys 获取所有源的 source_key（内部使用）
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
