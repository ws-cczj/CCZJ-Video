package db

import (
	"fmt"
	"path/filepath"
	"sync"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

var (
	instance *sqlx.DB
	once     sync.Once
	dataDir  string

	logMu sync.Mutex
	logFn func(level, msg string)
)

// SetLogger 允许外部注入日志实现，避免 db 依赖 applog
func SetLogger(fn func(level, msg string)) {
	logMu.Lock()
	defer logMu.Unlock()
	logFn = fn
}

func InitDB(dir string) error {
	var initErr error
	once.Do(func() {
		dataDir = dir
		dbPath := filepath.Join(dir, "cczj_video.db")
		instance, initErr = sqlx.Connect("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
		if initErr != nil {
			initErr = fmt.Errorf("connect sqlite: %w", initErr)
			return
		}
		// 连接池策略：WAL 模式下允许并发读 + 串行写。
		// 之前 SetMaxOpenConns(1) 把所有读写串行化，导致采集（写）和前端列表查询（读）
		// 互相阻塞。WAL 允许多个读连接并发，写连接通过 busy_timeout 排队。
		// 写并发上限设 1（SQLite 写锁是库级的，多写连接无意义且易触发 SQLITE_BUSY），
		// 读连接放开到较小数值即可满足列表/详情并发。
		instance.SetMaxOpenConns(8)
			instance.SetMaxIdleConns(4)
			instance.SetConnMaxLifetime(0) // 长连接，避免频繁重建
			initErr = createTables()
			if initErr != nil {
				return
			}
			// 迁移：为旧版数据库补充缺失列
			migrateSourcesColumns()
			migrateGlobalTypesColumns()
			migrateGlobalVideoColumns()
			// 启动时修复数据库中格式异常的 douban_id（科学计数法、浮点格式等）
			RepairDoubanIDs()
	})
	return initErr
}

func DB() *sqlx.DB {
	return instance
}

func DataDir() string {
	return dataDir
}

func Close() {
	if instance != nil {
		instance.Close()
	}
}

func createTables() error {
	tables := []string{
		// 核心配置表
		`CREATE TABLE IF NOT EXISTS sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			api_url TEXT NOT NULL,
			url_template TEXT DEFAULT '',
			url_prefix TEXT DEFAULT '',
			url_suffix TEXT DEFAULT '',
			enabled INTEGER DEFAULT 0,
			collect_limit INTEGER DEFAULT 0,
			collect_hours INTEGER DEFAULT 0,
			adv_config TEXT DEFAULT '',
			schedule_config TEXT DEFAULT '',
			strategy_config TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
		// 全局视频元数据表（所有源站共享的字段）
		`CREATE TABLE IF NOT EXISTS global_video (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			vod_name TEXT NOT NULL DEFAULT '',
			year TEXT DEFAULT '',
			area TEXT DEFAULT '',
			lang TEXT DEFAULT '',
			director TEXT DEFAULT '',
			writer TEXT DEFAULT '',
			actor TEXT DEFAULT '',
			tag TEXT DEFAULT '',
			content TEXT DEFAULT '',
			pic TEXT DEFAULT '',
			douban_id TEXT DEFAULT '',
			douban_score TEXT DEFAULT '',
			douban_votes TEXT DEFAULT '',
			genre TEXT DEFAULT '',
			release_date TEXT DEFAULT '',
			duration TEXT DEFAULT '',
			aka TEXT DEFAULT '',
			imdb TEXT DEFAULT '',
			season_count TEXT DEFAULT '',
			episode_count TEXT DEFAULT '',
			douban_cooldown_until DATETIME DEFAULT NULL,
			douban_search_failures INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 全局视频类型表（统一管理所有类型的采集和磁力链接获取权限）
		`CREATE TABLE IF NOT EXISTS global_types (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			type_name TEXT NOT NULL UNIQUE,
			collect_enabled INTEGER DEFAULT 1,
			magnet_enabled INTEGER DEFAULT 0,
			sort INTEGER DEFAULT 0,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// 收藏表
		`CREATE TABLE IF NOT EXISTS favorites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			global_id INTEGER NOT NULL,
			source_key TEXT NOT NULL,
			vod_id TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(global_id, source_key, vod_id)
		)`,
		// 观看历史表
		`CREATE TABLE IF NOT EXISTS watch_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			global_id INTEGER NOT NULL,
			source_key TEXT NOT NULL,
			vod_id TEXT NOT NULL,
			ep_num INTEGER NOT NULL DEFAULT 0,
			position REAL NOT NULL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(global_id, source_key, ep_num)
		)`,
	}

	for _, t := range tables {
		if _, err := instance.Exec(t); err != nil {
			return fmt.Errorf("create table: %w\nsql: %s", err, t)
		}
	}

	// 旧版已合并到 global_video 的表，删除干净
	_, _ = instance.Exec(`DROP TABLE IF EXISTS douban_info`)
	_, _ = instance.Exec(`DROP TABLE IF EXISTS id_mappings`)

	// 先删除旧版归一化索引（索引表达式已更新，需重建）
	_, _ = instance.Exec(`DROP INDEX IF EXISTS idx_gv_name_norm`)

	// 创建索引（IF NOT EXISTS 确保幂等）
	// global_video: 名称归一化唯一索引，表达式由 sqlNormExpr() 统一管理
	indexes := []string{
		fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS idx_gv_name_norm
			ON global_video (%s)`, sqlNormExpr()),
		// global_video: douban_id 索引（加速豆瓣信息查询）
		`CREATE INDEX IF NOT EXISTS idx_gv_douban_id
			ON global_video (douban_id) WHERE douban_id != ''`,
		// watch_history: global_id + ep_num 复合索引（加速观看历史查询）
		`CREATE INDEX IF NOT EXISTS idx_wh_global_ep
			ON watch_history (global_id, ep_num)`,
		// favorites: global_id 索引
		`CREATE INDEX IF NOT EXISTS idx_fav_global
			ON favorites (global_id)`,
	}
	for _, idx := range indexes {
		if _, err := instance.Exec(idx); err != nil {
			logInfo(fmt.Sprintf("创建索引失败(已忽略): %v", err))
		}
	}

	return nil
}

// migrateGlobalTypesColumns 为 global_types 表补充采集启用和磁力启用字段
func migrateGlobalTypesColumns() {
	migrations := []struct {
		col string
		def string
	}{
		{"collect_enabled", "INTEGER DEFAULT 1"},
		{"magnet_enabled", "INTEGER DEFAULT 0"},
	}
	for _, m := range migrations {
		q := fmt.Sprintf("ALTER TABLE global_types ADD COLUMN %s %s", m.col, m.def)
		if _, err := instance.Exec(q); err != nil {
			logInfo(fmt.Sprintf("迁移 global_types 列 %s: %v (已忽略)", m.col, err))
		} else {
			logInfo(fmt.Sprintf("迁移 global_types 列 %s 成功", m.col))
		}
	}
}

// migrateGlobalVideoColumns 为 global_video 表补充豆瓣冷静期相关列及磁力链接列
func migrateGlobalVideoColumns() {
	migrations := []struct {
		col string
		def string
	}{
		{"douban_cooldown_until", "DATETIME DEFAULT NULL"},
		{"douban_search_failures", "INTEGER DEFAULT 0"},
		{"magnet_link", "TEXT DEFAULT ''"},
		{"magnet_cooldown_until", "DATETIME DEFAULT NULL"},
		{"magnet_search_failures", "INTEGER DEFAULT 0"},
	}
	for _, m := range migrations {
		q := fmt.Sprintf("ALTER TABLE global_video ADD COLUMN %s %s", m.col, m.def)
		if _, err := instance.Exec(q); err != nil {
			logInfo(fmt.Sprintf("迁移 global_video 列 %s: %v (已忽略)", m.col, err))
		} else {
			logInfo(fmt.Sprintf("迁移 global_video 列 %s 成功", m.col))
		}
	}
}

// migrateSourcesColumns 为旧版数据库补充缺失的列
func migrateSourcesColumns() {
	migrations := []struct {
		col string
		def string
	}{
		{"adv_config", "TEXT DEFAULT ''"},
		{"schedule_config", "TEXT DEFAULT ''"},
	}
	for _, m := range migrations {
		q := fmt.Sprintf("ALTER TABLE sources ADD COLUMN %s %s", m.col, m.def)
		if _, err := instance.Exec(q); err != nil {
			// 列已存在时会报错，忽略即可
			logInfo(fmt.Sprintf("迁移列 %s: %v (已忽略)", m.col, err))
		} else {
			logInfo(fmt.Sprintf("迁移列 %s 成功", m.col))
		}
	}
}

func EnsureVideoTable(sourceKey string) error {
	tn := "v_" + esc(sourceKey)
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vod_id TEXT NOT NULL,
		type_id TEXT,
		type_name TEXT,
		vod_name TEXT,
		global_id INTEGER NOT NULL,
		vod_class TEXT DEFAULT '',
		vod_remarks TEXT DEFAULT '',
		vod_play_url TEXT DEFAULT '',
		vod_down_url TEXT DEFAULT '',
		vod_time TEXT DEFAULT '',
		vod_play_from TEXT DEFAULT '',
		vod_letter TEXT DEFAULT '',
		vod_sub TEXT DEFAULT '',
		vod_en TEXT DEFAULT '',
		UNIQUE(vod_id)
	)`, tn)
	if _, err := instance.Exec(q); err != nil {
		return err
	}

	indexes := []string{
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_vod_time ON %s(vod_time DESC)`, tn, tn),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_type_id ON %s(type_id)`, tn, tn),
	}
	for _, idx := range indexes {
		if _, err := instance.Exec(idx); err != nil {
			logWarn(fmt.Sprintf("创建索引失败: %v", err))
		}
	}

	return nil
}



func EnsureEpisodeTable(sourceKey string) error {
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS e_%s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vod_id INTEGER NOT NULL,
		ep_num INTEGER NOT NULL,
		ep_name TEXT DEFAULT '',
		ep_url TEXT NOT NULL,
		UNIQUE(vod_id, ep_num)
	)`, esc(sourceKey))
	_, err := instance.Exec(q)
	return err
}

func esc(s string) string {
	return s
}

func VideoTableName(sourceKey string) string  { return "v_" + sourceKey }
func EpisodeTableName(sourceKey string) string { return "e_" + sourceKey }
