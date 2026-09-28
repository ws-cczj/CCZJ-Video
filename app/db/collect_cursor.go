package db

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

// CollectCursor 是一个源的增量水位线。
//
// CoveredUntilUnix 只被"完整成功"的运行推进：取页/入库有缺口的运行不推进，
// 下一次就会把同一段时间重扫一遍，缺的数据才有机会补回来。它存在库里，
// 所以重启、停止后台采集、换设备都不会把进度丢掉。
type CollectCursor struct {
	SourceKey        string `db:"source_key" json:"source_key"`
	CoveredUntilUnix int64  `db:"covered_until_unix" json:"covered_until_unix"`
	LastAttemptUnix  int64  `db:"last_attempt_unix" json:"last_attempt_unix"`
}

// migrateCollectCursor 建出游标表。历史库没有游标，游标为空等价于
// "从未成功采集过"，第一次增量会退化成全量窗口——这与旧行为一致，不会漏数据。
func migrateCollectCursor(tx *sqlx.Tx) error {
	_, err := tx.Exec(`CREATE TABLE IF NOT EXISTS collect_cursor (
		source_key TEXT PRIMARY KEY,
		covered_until_unix INTEGER NOT NULL DEFAULT 0,
		last_attempt_unix INTEGER NOT NULL DEFAULT 0,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	return err
}

// GetCollectCursor 返回源的游标；没有记录时返回零值（首次采集）。
func GetCollectCursor(sourceKey string) (CollectCursor, error) {
	var c CollectCursor
	err := instance.Get(&c, `SELECT source_key, covered_until_unix, last_attempt_unix
		FROM collect_cursor WHERE source_key=?`, sourceKey)
	if errors.Is(err, sql.ErrNoRows) {
		return CollectCursor{SourceKey: sourceKey}, nil
	}
	if err != nil {
		return CollectCursor{}, fmt.Errorf("read collect cursor for %s: %w", sourceKey, err)
	}
	return c, nil
}

// MarkCollectAttempt 记下这次采集的开始时刻。它无论成败都写，用于诊断
// "上次是什么时候试过"，与只在成功时推进的 covered_until 区分开。
func MarkCollectAttempt(sourceKey string, at time.Time) error {
	_, err := instance.Exec(`INSERT INTO collect_cursor (source_key, last_attempt_unix, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_key) DO UPDATE SET last_attempt_unix=excluded.last_attempt_unix,
			updated_at=CURRENT_TIMESTAMP`, sourceKey, at.Unix())
	if err != nil {
		return fmt.Errorf("mark collect attempt for %s: %w", sourceKey, err)
	}
	return nil
}

// AdvanceCollectCursor 把水位线推到 windowEnd，只前进不后退：并发或重放时
// 旧窗口不能把新水位拉回去。
func AdvanceCollectCursor(sourceKey string, windowEnd time.Time) error {
	_, err := instance.Exec(`INSERT INTO collect_cursor (source_key, covered_until_unix, last_attempt_unix, updated_at)
		VALUES (?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_key) DO UPDATE SET
			covered_until_unix=MAX(collect_cursor.covered_until_unix, excluded.covered_until_unix),
			last_attempt_unix=MAX(collect_cursor.last_attempt_unix, excluded.last_attempt_unix),
			updated_at=CURRENT_TIMESTAMP`, sourceKey, windowEnd.Unix(), windowEnd.Unix())
	if err != nil {
		return fmt.Errorf("advance collect cursor for %s: %w", sourceKey, err)
	}
	return nil
}

// ResetCollectCursor 在源被删除或数据被清空后作废水位线，否则下一轮增量会
// 跳过实际并不存在的数据。执行器由调用方传入：删除源要游标和源记录同生共死，
// 必须在同一个事务里落刀。
func ResetCollectCursor(exec sqlx.Ext, sourceKey string) error {
	if _, err := exec.Exec(`DELETE FROM collect_cursor WHERE source_key=?`, sourceKey); err != nil {
		return fmt.Errorf("reset collect cursor for %s: %w", sourceKey, err)
	}
	return nil
}

// IncrementalWindow 把"水位线到此刻"换算成接口能接受的回溯小时数。
//
// 采集接口只有 h=N（最近 N 小时）这一种窗口表达，所以小时数向上取整，并额外
// 重叠 cursorWindowOverlap 覆盖源站时间戳与本地时钟的偏差；h 参数按"最近 N
// 小时"取整会截断边界，只向上取整才不会漏掉刚好落在窗口左端的那批数据。
func IncrementalWindow(cursor CollectCursor, now time.Time) (hours int, full bool) {
	if cursor.CoveredUntilUnix <= 0 {
		return 0, true // 从未成功过：没有可信窗口，按全量爬
	}
	elapsed := now.Unix() - cursor.CoveredUntilUnix
	if elapsed < 0 {
		elapsed = 0 // 源站/系统时钟回拨，按最小窗口处理
	}
	h := int(elapsed/3600) + 1 + cursorWindowOverlap
	if h >= cursorMaxHours {
		return 0, true // 缺口大到窗口不再有意义，全量重建更可靠
	}
	return h, false
}

const (
	// cursorWindowOverlap 是每次增量固定多回溯的小时数，用来吃掉边界误差。
	cursorWindowOverlap = 1
	// cursorMaxHours 是增量窗口的上限（小时）。再大就等同全量，不如直接全量。
	cursorMaxHours = 24 * 30
)
