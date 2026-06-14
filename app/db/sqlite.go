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
		instance.SetMaxOpenConns(1)
		initErr = createTables()
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
		`CREATE TABLE IF NOT EXISTS sources (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source_key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			api_url TEXT NOT NULL,
			url_template TEXT DEFAULT '',
			url_prefix TEXT DEFAULT '',
			url_suffix TEXT DEFAULT '',
			enabled INTEGER DEFAULT 1,
			collect_limit INTEGER DEFAULT 0,
			collect_hours INTEGER DEFAULT 0,
			strategy_config TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// --- 全局视频去重表：按 vod_name 唯一标识同一部视频 ---
		`CREATE TABLE IF NOT EXISTS global_video (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			vod_name TEXT NOT NULL UNIQUE,
			title TEXT DEFAULT '',
			year TEXT DEFAULT '',
			area TEXT DEFAULT '',
			lang TEXT DEFAULT '',
			director TEXT DEFAULT '',
			actor TEXT DEFAULT '',
			tag TEXT DEFAULT '',
			content TEXT DEFAULT '',
			pic TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		// --- 全局豆瓣信息表：关联 global_video，subject_id 可为空（占位符） ---
		`CREATE TABLE IF NOT EXISTS douban_info (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			global_id INTEGER NOT NULL UNIQUE,
			subject_id TEXT DEFAULT '',
			rating TEXT DEFAULT '',
			votes TEXT DEFAULT '',
			director TEXT DEFAULT '',
			writer TEXT DEFAULT '',
			actor TEXT DEFAULT '',
			genre TEXT DEFAULT '',
			country TEXT DEFAULT '',
			language TEXT DEFAULT '',
			release_date TEXT DEFAULT '',
			season_count TEXT DEFAULT '',
			episode_count TEXT DEFAULT '',
			duration TEXT DEFAULT '',
			aka TEXT DEFAULT '',
			imdb TEXT DEFAULT '',
			poster_url TEXT DEFAULT '',
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (global_id) REFERENCES global_video(id) ON DELETE CASCADE
		)`,
		// --- 收藏（关联 global_video） ---
		`CREATE TABLE IF NOT EXISTS favorites (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			global_id INTEGER NOT NULL,
			source_key TEXT DEFAULT '',
			vod_id TEXT DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (global_id) REFERENCES global_video(id) ON DELETE CASCADE
		)`,
		// --- 观看历史（关联 global_video） ---
		`CREATE TABLE IF NOT EXISTS watch_history (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			global_id INTEGER NOT NULL,
			source_key TEXT DEFAULT '',
			vod_id TEXT DEFAULT '',
			ep_num INTEGER DEFAULT 1,
			position REAL DEFAULT 0,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (global_id) REFERENCES global_video(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}

	for _, t := range tables {
		if _, err := instance.Exec(t); err != nil {
			return fmt.Errorf("create table: %w\nsql: %s", err, t)
		}
	}

	// === Migration: 为旧数据库补齐 sources 表的新列 ===
	missingSourceCols := []string{
		`collect_limit INTEGER DEFAULT 0`,
		`collect_hours INTEGER DEFAULT 0`,
		`adv_config TEXT DEFAULT ''`,
		`schedule_config TEXT DEFAULT ''`,
		`strategy_config TEXT DEFAULT ''`,
	}
	for _, col := range missingSourceCols {
		_, _ = instance.Exec(fmt.Sprintf(`ALTER TABLE sources ADD COLUMN %s`, col))
	}

	// === Migration: 为旧 favorites 表添加 global_id / source_key / vod_id 列 ===
	_, _ = instance.Exec(`ALTER TABLE favorites ADD COLUMN global_id INTEGER DEFAULT 0`)
	_, _ = instance.Exec(`ALTER TABLE favorites ADD COLUMN source_key TEXT DEFAULT ''`)
	_, _ = instance.Exec(`ALTER TABLE favorites ADD COLUMN vod_id TEXT DEFAULT ''`)

	// === Migration: 为旧 watch_history 表添加 global_id 列 ===
	_, _ = instance.Exec(`ALTER TABLE watch_history ADD COLUMN global_id INTEGER DEFAULT 0`)

	// === Migration: 删除不再需要的 id_mappings 表 ===
	_, _ = instance.Exec(`DROP TABLE IF EXISTS id_mappings`)

	// === Migration: 为旧 douban_info 表添加 global_id 列 ===
	_, _ = instance.Exec(`ALTER TABLE douban_info ADD COLUMN IF NOT EXISTS global_id INTEGER DEFAULT 0`)
	_, _ = instance.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_douban_info_global_id ON douban_info(global_id)`)

	// === 创建索引（必须在 migration 添加列之后执行） ===
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_global_video_vod_name ON global_video(vod_name)`,
		`CREATE INDEX IF NOT EXISTS idx_douban_info_subject_id ON douban_info(subject_id)`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_global_id ON favorites(global_id)`,
		`CREATE INDEX IF NOT EXISTS idx_watch_history_global_id ON watch_history(global_id)`,
		`CREATE INDEX IF NOT EXISTS idx_watch_history_updated_at ON watch_history(updated_at DESC)`,
	}
	for _, idx := range indexes {
		if _, err := instance.Exec(idx); err != nil {
			return fmt.Errorf("create index: %w\nsql: %s", err, idx)
		}
	}

	return nil
}

func EnsureVideoTable(sourceKey string) error {
	tn := "v_" + esc(sourceKey)
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		vod_id TEXT NOT NULL,
		type_id TEXT,
		type_name TEXT,
		vod_name TEXT,
		global_id INTEGER DEFAULT 0,
		vod_class TEXT DEFAULT '',
		vod_remarks TEXT DEFAULT '',
		vod_pic TEXT DEFAULT '',
		vod_play_url TEXT DEFAULT '',
		vod_down_url TEXT DEFAULT '',
		vod_time TEXT DEFAULT '',
		vod_douban_id TEXT DEFAULT '',
		vod_douban_score TEXT DEFAULT '',
		vod_hits TEXT DEFAULT '',
		vod_hits_day TEXT DEFAULT '',
		vod_hits_week TEXT DEFAULT '',
		vod_hits_month TEXT DEFAULT '',
		vod_pubdate TEXT DEFAULT '',
		vod_version TEXT DEFAULT '',
		vod_state TEXT DEFAULT '',
		vod_score TEXT DEFAULT '',
		vod_score_all TEXT DEFAULT '',
		vod_score_num TEXT DEFAULT '',
		vod_isend TEXT DEFAULT '',
		vod_play_from TEXT DEFAULT '',
		vod_play_note TEXT DEFAULT '',
		vod_letter TEXT DEFAULT '',
		vod_sub TEXT DEFAULT '',
		vod_en TEXT DEFAULT '',
		UNIQUE(vod_id)
	)`, tn)
	if _, err := instance.Exec(q); err != nil {
		return err
	}

	var existing []TableColumn
	err := instance.Select(&existing, fmt.Sprintf("PRAGMA table_info(%s)", tn))
	if err != nil {
		logWarn(fmt.Sprintf("获取表 %s 列信息失败: %v", tn, err))
	}
	has := make(map[string]bool, len(existing))
	for _, c := range existing {
		has[c.Name] = true
	}

	// === Migration: 删除与 global_video 重复的字段（释放存储空间） ===
	// global_video: pic, year, area, lang, director, actor, tag, content
	// v_*:          vod_pic, vod_year, vod_area, vod_lang, vod_director, vod_actor, vod_tag, vod_content
	duplicateCols := []string{
		"vod_pic", "vod_year", "vod_area", "vod_lang",
		"vod_director", "vod_actor", "vod_tag", "vod_content",
	}
	for _, col := range duplicateCols {
		if has[col] {
			if _, err := instance.Exec(fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", tn, col)); err != nil {
				logWarn(fmt.Sprintf("删除表 %s 的重复字段 %s 失败: %v", tn, col, err))
			} else {
				logInfo(fmt.Sprintf("删除表 %s 的重复字段 %s", tn, col))
			}
		}
	}

	// === Migration: 添加缺失的新字段 ===
	newCols := []struct {
		name    string
		def     string
	}{
		{"global_id", "INTEGER DEFAULT 0"},
		{"vod_douban_id", "TEXT DEFAULT ''"},
		{"vod_douban_score", "TEXT DEFAULT ''"},
		{"vod_hits", "TEXT DEFAULT ''"},
		{"vod_hits_day", "TEXT DEFAULT ''"},
		{"vod_hits_week", "TEXT DEFAULT ''"},
		{"vod_hits_month", "TEXT DEFAULT ''"},
		{"vod_pubdate", "TEXT DEFAULT ''"},
		{"vod_version", "TEXT DEFAULT ''"},
		{"vod_state", "TEXT DEFAULT ''"},
		{"vod_score", "TEXT DEFAULT ''"},
		{"vod_score_all", "TEXT DEFAULT ''"},
		{"vod_score_num", "TEXT DEFAULT ''"},
		{"vod_isend", "TEXT DEFAULT ''"},
		{"vod_play_from", "TEXT DEFAULT ''"},
		{"vod_play_note", "TEXT DEFAULT ''"},
		{"vod_pic", "TEXT DEFAULT ''"},
		{"vod_letter", "TEXT DEFAULT ''"},
		{"vod_sub", "TEXT DEFAULT ''"},
		{"vod_en", "TEXT DEFAULT ''"},
	}
	for _, col := range newCols {
		if !has[col.name] {
			alterSQL := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", tn, col.name, col.def)
			if _, err := instance.Exec(alterSQL); err != nil {
				logWarn(fmt.Sprintf("为表 %s 添加列 %s 失败: %v", tn, col.name, err))
			} else {
				logInfo(fmt.Sprintf("为表 %s 添加列 %s", tn, col.name))
			}
		}
	}

	indexes := []string{
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_vod_time ON %s(vod_time DESC)`, tn, tn),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_type_id ON %s(type_id)`, tn, tn),
		fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%s_global_id ON %s(global_id)`, tn, tn),
	}
	for _, idx := range indexes {
		if _, err := instance.Exec(idx); err != nil {
			logWarn(fmt.Sprintf("创建索引失败: %v", err))
		}
	}

	// === Migration: 为 global_id=0 的记录重新关联 global_video ===
	type pendingRow struct {
		VodId   string `db:"vod_id"`
		VodName string `db:"vod_name"`
	}
	var pending []pendingRow
	err = instance.Select(&pending, fmt.Sprintf(`SELECT vod_id, vod_name FROM %s WHERE global_id = 0 AND vod_name != '' LIMIT 1000`, tn))
	if err != nil {
		logWarn(fmt.Sprintf("查询待迁移记录失败: %v", err))
	} else if len(pending) > 0 {
		logInfo(fmt.Sprintf("表 %s 有 %d 条记录需要关联 global_video", tn, len(pending)))
		for _, row := range pending {
			globalID, err := GetOrCreateGlobalID(row.VodName)
			if err == nil && globalID > 0 {
				_, _ = instance.Exec(fmt.Sprintf(`UPDATE %s SET global_id = ? WHERE vod_id = ?`, tn), globalID, row.VodId)
			}
		}
	}

	return nil
}

func EnsureTypeTable(sourceKey string) error {
	q := fmt.Sprintf(`CREATE TABLE IF NOT EXISTS t_%s (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		type_id INTEGER NOT NULL,
		type_name TEXT NOT NULL,
		UNIQUE(type_id)
	)`, esc(sourceKey))
	_, err := instance.Exec(q)
	return err
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
func TypeTableName(sourceKey string) string    { return "t_" + sourceKey }
func EpisodeTableName(sourceKey string) string { return "e_" + sourceKey }
