package db

import (
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// 源健康度样本。
//
// 采集源是外部站点，接口今天能答明天就可能永久下线。在这之前这类信息只存在于
// 内存的采集状态和滚动日志里：进程一重启就归零，用户看不到"这个源其实已经连续
// 失败两周了"，也没法区分偶发抖动和真的失效。这里把每次运行的结果按样本落库，
// 于是健康度有了历史，失效巡检也有了记账的地方。
//
// 粒度刻意是"一次运行一条"而不是"一页一条"：一次全量采集会上百页，逐页写等于
// 为了诊断把用户库撑大。运行级样本已经能回答唯一重要的问题——这个源还能不能用。
type SourceHealthSample struct {
	Id        int64  `db:"id" json:"id"`
	SourceKey string `db:"source_key" json:"source_key"`
	Kind      string `db:"kind" json:"kind"`
	OK        bool   `db:"ok" json:"ok"`
	LatencyMS int64  `db:"latency_ms" json:"latency_ms"`
	Saved     int    `db:"saved" json:"saved"`
	Err       string `db:"err" json:"err"`
	TsUnix    int64  `db:"ts_unix" json:"ts_unix"`
}

// 样本来源。collect 是一次采集运行收尾，patrol 是巡检主动探测接口地址。
const (
	SourceHealthKindCollect = "collect"
	SourceHealthKindPatrol  = "patrol"
)

// sourceHealthKeep 是每个源保留的样本条数（采集与巡检合起来算），超出即淘汰最旧的。
// 按巡检最快每小时一次算也够铺开一周多的历史，而十个源的总量仍只有几千行。
const sourceHealthKeep = 200

// sourceHealthErrBytes 是单条错误文本的字节上限。接口报错会把整个响应体带进来，
// 不设上限就是拿用户的库换一条日志。
const sourceHealthErrBytes = 300

// SourceHealth 是单个源的健康度汇总。RecentOnly 让汇总只看最近 N 条，
// 与历史列表共用同一份"新→旧"的取数口径，避免两处算出不同的成功率。
type SourceHealth struct {
	SourceKey      string `db:"source_key" json:"source_key"`
	Samples        int    `db:"samples" json:"samples"`
	OKs            int    `db:"oks" json:"oks"`
	SuccessRate    int    `db:"success_rate" json:"success_rate"`
	AvgLatencyMS   int64  `db:"avg_latency_ms" json:"avg_latency_ms"`
	FailStreak     int    `db:"fail_streak" json:"fail_streak"`
	LastError      string `db:"last_error" json:"last_error"`
	LastErrorUnix  int64  `db:"last_error_unix" json:"last_error_unix"`
	LastSampleUnix int64  `db:"last_sample_unix" json:"last_sample_unix"`
	LastOK         bool   `db:"last_ok" json:"last_ok"`
}

// migrateSourceHealth 建表。历史库升级后表是空的，空表等价于"没有观测过"，
// 界面据此显示"暂无记录"，而不是把从未采过的源误报成健康或故障。
func migrateSourceHealth(tx *sqlx.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS source_health (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		source_key TEXT NOT NULL,
		kind TEXT NOT NULL,
		ok INTEGER NOT NULL,
		latency_ms INTEGER NOT NULL DEFAULT 0,
		saved INTEGER NOT NULL DEFAULT 0,
		err TEXT NOT NULL DEFAULT '',
		ts_unix INTEGER NOT NULL
	)`)
	return err
}

// RecordSourceHealth 落一条样本，并顺带把该源超额的旧样本删掉。
//
// 失败只记日志、不向上抛：健康度是观测面，采集本身已经成功了的话，
// 不该因为记账失败而让调用方以为整轮采集出了问题。
func RecordSourceHealth(sourceKey, kind string, ok bool, latencyMS int64, saved int, errText string, at time.Time) error {
	if sourceKey == "" {
		return nil
	}
	// 错误文本来自外部接口，长起来没有边界。按字节限额截，但要把切点退回到
	// rune 边界：半个汉字会被诊断页原样渲染成乱码。
	if len(errText) > sourceHealthErrBytes {
		cut := sourceHealthErrBytes
		for cut > 0 && !utf8.RuneStart(errText[cut]) {
			cut--
		}
		errText = errText[:cut]
	}
	okInt := 0
	if ok {
		okInt = 1
	}
	if _, err := instance.Exec(`INSERT INTO source_health (source_key, kind, ok, latency_ms, saved, err, ts_unix)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, sourceKey, kind, okInt, latencyMS, saved, errText, at.Unix()); err != nil {
		return fmt.Errorf("record source health for %s: %w", sourceKey, err)
	}
	// 淘汰写在插入之后：删的是"含这条新样本"的超额部分，所以保留数始终成立。
	if _, err := instance.Exec(`DELETE FROM source_health
		WHERE source_key = ? AND id NOT IN (
			SELECT id FROM source_health WHERE source_key = ? ORDER BY ts_unix DESC, id DESC LIMIT ?
		)`, sourceKey, sourceKey, sourceHealthKeep); err != nil {
		return fmt.Errorf("prune source health for %s: %w", sourceKey, err)
	}
	return nil
}

// ResetSourceHealth 在源被删除时连带清掉它的健康度样本：一个已经不存在的源
// 继续留在健康度列表里，只会让人以为还有东西在跑。执行器由调用方传入，
// 与删除源共用同一个事务。
func ResetSourceHealth(exec sqlx.Ext, sourceKey string) error {
	if _, err := exec.Exec(`DELETE FROM source_health WHERE source_key=?`, sourceKey); err != nil {
		return fmt.Errorf("reset source health for %s: %w", sourceKey, err)
	}
	return nil
}

// ListSourceHealth 返回某源最近的样本，新→旧。kind 为空表示不限来源，
// 诊断页的"健康度历史"就靠这个把采集与巡检掺在一起按时间铺开。
func ListSourceHealth(sourceKey, kind string, limit int) ([]SourceHealthSample, error) {
	if limit <= 0 || limit > sourceHealthKeep {
		limit = sourceHealthKeep
	}
	query := `SELECT id, source_key, kind, ok, latency_ms, saved, err, ts_unix
		FROM source_health WHERE source_key = ?`
	args := []any{sourceKey}
	if kind != "" {
		query += ` AND kind = ?`
		args = append(args, kind)
	}
	var rows []SourceHealthSample
	err := instance.Select(&rows, query+` ORDER BY ts_unix DESC, id DESC LIMIT ?`, append(args, limit)...)
	return rows, err
}

// GetSourceHealth 汇总单个源某类最近 window 条样本。没有样本时返回零值，
// 调用方用 Samples==0 区分"没观测过"和"观测过但全失败"。
//
// 汇总必须按 kind 分开算：采集样本的 latency 是整轮运行的秒级耗时，巡检样本
// 是单次请求的毫秒级耗时，掺在一起取平均得到的数两种含义都不对。
func GetSourceHealth(sourceKey, kind string, window int) (SourceHealth, error) {
	if window <= 0 {
		window = sourceHealthKeep
	}
	var h SourceHealth
	// 聚合只覆盖子查询里那 window 条：成功率必须是"最近这段时间的成功率"，
	// 全历史的成功率会被很久以前的正常运行稀释掉，看不出源正在恶化。
	err := instance.Get(&h, `SELECT COUNT(*) AS samples,
			COALESCE(SUM(ok), 0) AS oks,
			CASE WHEN COUNT(*) > 0 THEN CAST(ROUND(100.0 * COALESCE(SUM(ok), 0) / COUNT(*)) AS INTEGER) ELSE 0 END AS success_rate,
			COALESCE(CAST(AVG(CASE WHEN ok = 1 AND latency_ms > 0 THEN latency_ms END) AS INTEGER), 0) AS avg_latency_ms,
			COALESCE(MAX(CASE WHEN ok = 0 THEN ts_unix END), 0) AS last_error_unix,
			COALESCE(MAX(ts_unix), 0) AS last_sample_unix
		FROM (SELECT ok, latency_ms, ts_unix FROM source_health
			WHERE source_key = ? AND kind = ? ORDER BY ts_unix DESC, id DESC LIMIT ?)`, sourceKey, kind, window)
	if err != nil {
		return SourceHealth{}, err
	}
	h.SourceKey = sourceKey
	if h.Samples == 0 {
		return h, nil
	}
	// 最后一条样本的成败和错误文本各自单取一条：聚合查询里没有"取这一行的某列"
	// 的稳定写法，而这两列都要按 ts 排序取最新，索引正好覆盖。
	var last struct {
		OK bool `db:"ok"`
	}
	if err := instance.Get(&last, `SELECT ok FROM source_health
		WHERE source_key = ? AND kind = ? ORDER BY ts_unix DESC, id DESC LIMIT 1`, sourceKey, kind); err == nil {
		h.LastOK = last.OK
	}
	var lastErr struct {
		Err string `db:"err"`
	}
	if err := instance.Get(&lastErr, `SELECT err FROM source_health
		WHERE source_key = ? AND kind = ? AND ok = 0 ORDER BY ts_unix DESC, id DESC LIMIT 1`, sourceKey, kind); err == nil {
		h.LastError = lastErr.Err
	}
	// 连败次数是"失效"最直接的读数：从最后一次成功之后累计的失败。
	// 用 ts 比较而不是行号，因为同一毫秒内的样本顺序由 id 决定，这里宁可
	// 多算一条也不能漏——巡检判定只看量级，不区分并列的那一次。
	var lastOK struct {
		Ts int64 `db:"ts"`
	}
	if err := instance.Get(&lastOK, `SELECT COALESCE(MAX(ts_unix), 0) AS ts FROM source_health
		WHERE source_key = ? AND kind = ? AND ok = 1`, sourceKey, kind); err == nil {
		_ = instance.Get(&h.FailStreak, `SELECT COUNT(*) FROM source_health
			WHERE source_key = ? AND kind = ? AND ok = 0 AND ts_unix >= ?`, sourceKey, kind, lastOK.Ts)
	}
	return h, nil
}
