package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
		instance, initErr = sqlx.Connect("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
		if initErr != nil {
			initErr = fmt.Errorf("connect sqlite: %w", initErr)
			return
		}
		// 连接池策略：8 条连接由读和写共用，并没有一套单独的「写连接上限 1」。
		// SQLite 的写锁本来就是库级的，WAL 下同一时刻仍只有一个写事务能推进，
		// 抢不到锁的连接靠 busy_timeout(5000) 排队而不是立刻返回 SQLITE_BUSY。
		// 早先的 SetMaxOpenConns(1) 会把读也一起串行化，采集写入时前端列表就卡住，
		// 所以才放开到 8；代价是写方必须自己把批量改动包进一个事务
		// （见 upsertCatalogItems / InheritDoubanFieldsFromSiblings），
		// 用「少而长的写事务」代替「几十次抢锁 + fsync 的短写」。
		instance.SetMaxOpenConns(8)
		instance.SetMaxIdleConns(4)
		instance.SetConnMaxLifetime(0) // 长连接，避免频繁重建
		if err := createTables(); err != nil {
			initErr = err
			return
		}
		initErr = runMigrations()
	})
	return initErr
}

// runMigrations 先备份再改 schema：拿不到备份就宁可启动失败，也不在无兜底的情况下
// 动用户的库。没有待执行迁移时直接返回，避免每次启动都复制一遍数据库。
func runMigrations() error {
	current, err := schemaVersion(instance)
	if err != nil {
		return err
	}
	pending := 0
	for _, m := range migrations {
		if m.version > current {
			pending++
		}
	}
	if pending == 0 {
		return nil
	}
	backup, err := backupBeforeMigrations(instance, dataDir, pending)
	if err != nil {
		return err
	}
	logInfo(fmt.Sprintf("迁移前已备份数据库: %s", backup))
	if _, err := migrateToLatest(instance); err != nil {
		return err
	}
	// 建表时写下的索引可能引用了迁移新增的列，迁移后重建一次才不会出现半套索引。
	return createIndexes()
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
			name_norm TEXT NOT NULL DEFAULT '',
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
			douban_last_attempt_at DATETIME DEFAULT NULL,
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

	// 旧版归一化索引由迁移 v3 负责删除并重建，这里不再每次启动都 drop 重建。

	return createIndexes()
}

// createIndexes 建出当前 schema 需要的全部索引，可重复执行。
func createIndexes() error {
	// 创建索引（IF NOT EXISTS 确保幂等）
	// global_video: 名称归一化唯一索引。name_norm 由 Go 侧那一份归一化实现写入，
	// 不再用 SQL 表达式建函数索引——那份表达式和 Go 的实现各自漂移过，
	// 同一部片因此能插进两条记录。允许同名但不同类型共存。
	// 部分索引：空标题的行没有可比的身份，让它们互相冲突只会挡住正常插入。
	//
	// 下面每条都对照过 EXPLAIN QUERY PLAN（见 indexes_test.go）：只建真被
	// 选中、并且确实消掉了扫表或临时排序的索引，多余的索引只会拖慢采集写入。
	indexes := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gv_name_type_norm
			ON global_video (name_norm, type_id) WHERE name_norm <> ''`,
		// global_video: 按豆瓣 ID 查。不能建成局部索引：查询是 douban_id = ?，
		// SQLite 证明不了占位符非空，加了 WHERE 反而全程扫表。
		`CREATE INDEX IF NOT EXISTS idx_gv_douban_id
			ON global_video (douban_id)`,
		// global_video: 按标题查（收藏、历史、详情补充都走 vod_name）
		`CREATE INDEX IF NOT EXISTS idx_gv_vod_name
			ON global_video (vod_name)`,
		// global_video: 首页「最近在看/最近入库」一类按 updated_at 倒序取 N 条
		`CREATE INDEX IF NOT EXISTS idx_gv_updated
			ON global_video (updated_at)`,
		// 豆瓣补全队列的 douban_last_attempt_at 刻意不建索引：EXPLAIN 显示两条队列的
		// 筛选各自走 NOT EXISTS 子查询和 MULTI-INDEX OR，都不选这条索引，临时排序照旧；
		// 队列每轮只取几行、命中集很小，为一列每轮都在变的时钟建索引只是多付一次写入。
		// source_videos: 目录浏览/搜索的主路径。前缀是 source_key + lifecycle_state，
		// 后缀按 vod_time、id 降序，翻页因此能顺着索引走并在 LIMIT 处停下，
		// 不再把整个源读进临时 B 树排序。
		`CREATE INDEX IF NOT EXISTS idx_sv_key_lifecycle_time
			ON source_videos (source_key, lifecycle_state, vod_time DESC, id DESC)`,
		// source_videos: 跨源合并视图没有时间前缀可以用，排序只能按
		// (lifecycle_state, vod_time DESC, id DESC) 顺着走并在 LIMIT 处停下；
		// 少了这条，合并列表每一页都要把整库读进临时 B 树排序。
		`CREATE INDEX IF NOT EXISTS idx_sv_lifecycle_time
			ON source_videos (lifecycle_state, vod_time DESC, id DESC)`,
		// source_videos: 按源内某个全局身份取最近一条
		`CREATE INDEX IF NOT EXISTS idx_sv_key_global_updated
			ON source_videos (source_key, global_id, updated_at DESC)`,
		// source_videos: 跨源同片列表（一部片在哪些源里有）
		`CREATE INDEX IF NOT EXISTS idx_sv_global_key
			ON source_videos (global_id, source_key)`,
		`CREATE INDEX IF NOT EXISTS idx_sv_key_year
			ON source_videos (source_key, vod_year)`,
		`CREATE INDEX IF NOT EXISTS idx_sv_key_area
			ON source_videos (source_key, vod_area)`,
		// watch_history: global_id + ep_num 复合索引（加速观看历史查询）
		`CREATE INDEX IF NOT EXISTS idx_wh_global_ep
			ON watch_history (global_id, ep_num)`,
		// watch_history: 首页"最近观看"按 updated_at 倒序取 N 条
		`CREATE INDEX IF NOT EXISTS idx_wh_updated
			ON watch_history (updated_at DESC)`,
		// watch_history: 按源内 vod_id 清理历史（DeleteHistoryByVideo 这条绑定接口）
		`CREATE INDEX IF NOT EXISTS idx_wh_source_vod
			ON watch_history (source_key, vod_id, ep_num)`,
		// favorites: 收藏列表按加入时间倒序分页
		`CREATE INDEX IF NOT EXISTS idx_fav_created
			ON favorites (created_at DESC)`,
		// source_health: 健康度取数永远是"某个源、按时间倒序、只要最近 N 条"
		// （汇总、历史列表、淘汰旧样本三处都是这个形状）。索引把这三条都变成
		// 顺着 B 树走并在 LIMIT 处停下，而不是每次打开诊断页就全表扫。
		`CREATE INDEX IF NOT EXISTS idx_sh_key_time
			ON source_health (source_key, ts_unix DESC, id DESC)`,
		// favorites 按 global_id 查走 UNIQUE(global_id, source_key, vod_id)
		// 自动索引的左前缀，不需要再建一条。
	}
	for _, idx := range indexes {
		if _, err := instance.Exec(idx); err != nil {
			logInfo(fmt.Sprintf("创建索引失败(已忽略): %v", err))
		}
	}

	return nil
}
