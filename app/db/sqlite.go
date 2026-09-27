package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"
	_ "modernc.org/sqlite"
)

// databaseResetVersion identifies the schema generation that can be retained.
// Older databases are archived then replaced with a fresh catalog-only store.
const databaseResetVersion = "5"

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

func logInfo(msg string)  { safeLog("INFO", msg) }
func logWarn(msg string)  { safeLog("WARN", msg) }
func logError(msg string) { safeLog("ERROR", msg) }

func safeLog(level, msg string) {
	defer func() { _ = recover() }()
	logMu.Lock()
	fn := logFn
	logMu.Unlock()
	if fn != nil {
		fn(level, msg)
	}
}

func InitDB(dir string) error {
	var initErr error
	once.Do(func() {
		dataDir = dir
		if err := os.MkdirAll(dir, 0755); err != nil {
			initErr = fmt.Errorf("create data directory: %w", err)
			return
		}
		dbPath := filepath.Join(dir, "cczj_video.db")
		if err := ResetDatabaseForUpgrade(dir, dbPath); err != nil {
			initErr = err
			return
		}
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
		if err := createTables(); err != nil {
			initErr = err
			return
		}
		if err := SetSetting("database_reset_version", databaseResetVersion); err != nil {
			initErr = fmt.Errorf("record database reset version: %w", err)
			return
		}
		if err := SetSetting("database_reset_generation", databaseResetVersion); err != nil {
			initErr = fmt.Errorf("record browser reset generation: %w", err)
			return
		}
	})
	return initErr
}

// ResetDatabaseForUpgrade archives an unsupported database through SQLite,
// removes its files and cache, then lets normal startup create a clean store.
// It deliberately never executes schema migrations.
func ResetDatabaseForUpgrade(dir, dbPath string) error {
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("stat database: %w", err)
	}
	probe, err := sqlx.Open("sqlite", "file:"+filepath.ToSlash(dbPath)+"?mode=ro")
	if err != nil {
		return fmt.Errorf("open database reset probe: %w", err)
	}
	var version string
	err = probe.Get(&version, `SELECT value FROM settings WHERE key='database_reset_version'`)
	_ = probe.Close()
	if err == nil && version == databaseResetVersion {
		return nil
	}
	if err != nil && err != sql.ErrNoRows && !strings.Contains(err.Error(), "no such table") {
		return fmt.Errorf("read database reset marker: %w", err)
	}
	archiveDir := filepath.Join(dir, "reset-archives")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return fmt.Errorf("create reset archive: %w", err)
	}
	archive := filepath.Join(archiveDir, "cczj_video_pre_reset_"+time.Now().Format("20060102_150405.000000000")+".db")
	writer, err := sqlx.Connect("sqlite", dbPath+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return fmt.Errorf("open database for consistent archive: %w", err)
	}
	var integrity string
	if err = writer.Get(&integrity, "PRAGMA integrity_check"); err == nil && integrity != "ok" {
		err = fmt.Errorf("integrity check: %s", integrity)
	}
	if err == nil {
		err = vacuumInto(writer, archive)
	}
	_ = writer.Close()
	if err != nil {
		return fmt.Errorf("archive pre-reset database: %w", err)
	}
	for _, path := range []string{dbPath, dbPath + "-wal", dbPath + "-shm"} {
		if err := removeIfExists(path); err != nil {
			return err
		}
	}
	if err := os.RemoveAll(filepath.Join(dir, "ts_cache")); err != nil {
		return fmt.Errorf("clear disk cache: %w", err)
	}
	return nil
}

func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove %s: %w", path, err)
	}
	return nil
}

func vacuumInto(database *sqlx.DB, destination string) error {
	_, err := database.Exec("VACUUM INTO '" + strings.ReplaceAll(filepath.ToSlash(destination), "'", "''") + "'")
	return err
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
			type_id INTEGER DEFAULT 0,
			year TEXT DEFAULT '',
			area TEXT DEFAULT '',
			lang TEXT DEFAULT '',
			writer TEXT DEFAULT '',
			tag TEXT DEFAULT '',
			pic TEXT DEFAULT '',
			douban_id TEXT DEFAULT '',
			douban_score TEXT DEFAULT '',
			douban_votes TEXT DEFAULT '',
			douban_hotness TEXT DEFAULT '',
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
		`CREATE TABLE IF NOT EXISTS source_types (
			source_key TEXT NOT NULL, source_type_id TEXT NOT NULL, global_type_id INTEGER,
			type_name TEXT NOT NULL DEFAULT '', updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY(source_key, source_type_id)
		)`,
		`CREATE TABLE IF NOT EXISTS source_videos (
			id INTEGER PRIMARY KEY AUTOINCREMENT, source_key TEXT NOT NULL, source_vod_id TEXT NOT NULL,
			global_id INTEGER NOT NULL, source_type_id TEXT NOT NULL DEFAULT '', global_type_id INTEGER,
			type_name TEXT NOT NULL DEFAULT '', vod_name TEXT NOT NULL DEFAULT '', vod_pic TEXT NOT NULL DEFAULT '',
			vod_remarks TEXT NOT NULL DEFAULT '', vod_year TEXT NOT NULL DEFAULT '', vod_area TEXT NOT NULL DEFAULT '',
			vod_time TEXT NOT NULL DEFAULT '', lifecycle_state TEXT NOT NULL DEFAULT 'active',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP, updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(source_key, source_vod_id)
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
	_, _ = instance.Exec(`DROP INDEX IF EXISTS idx_gv_name_type_norm`)

	// 创建索引（IF NOT EXISTS 确保幂等）
	// global_video: 名称+类型归一化唯一索引，允许同名但不同类型共存
	indexes := []string{
		fmt.Sprintf(`CREATE UNIQUE INDEX IF NOT EXISTS idx_gv_name_type_norm
			ON global_video (%s, type_id)`, sqlNormExpr()),
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
