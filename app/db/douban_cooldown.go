package db

import (
	"cczjVideo/app/applog"
	"database/sql"
	"errors"
	"time"
)

// doubanAttemptColumn 是「这一行上次被豆瓣队列试过」的时刻，只由 MarkDoubanAttempt 写。
const doubanAttemptColumn = "douban_last_attempt_at"

// doubanCooldownFormat 是冷却/尝试时间落库的格式。SQLite 里按字符串比较，
// 读侧解析必须用同一个布局。
const doubanCooldownFormat = "2006-01-02 15:04:05"

// doubanCooldownHours 是一次「确定没查到」之后搁置多久。查无此片的记录不该每轮批量都
// 去撞一次豆瓣；但也不能永久否决——豆瓣随时可能收录。
const doubanCooldownHours = 24

// searchFailureCooldownThreshold 是累计多少次正常页无结果才进冷却（沿用旧策略）。
const searchFailureCooldownThreshold = 5

// SetDoubanCooldown 为指定 global_id 设置24小时冷静期（用于详情拿到但无评分）
func SetDoubanCooldown(globalID int) error {
	cooldownUntil := time.Now().Add(doubanCooldownHours * time.Hour).Format(doubanCooldownFormat)
	_, err := instance.Exec(`UPDATE global_video SET douban_cooldown_until = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		cooldownUntil, globalID)
	if err != nil {
		applog.Error("[Douban] SetDoubanCooldown failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// MarkDoubanAttempt 记下「这一行刚试过」。
//
// 空跑也必须打点：批量队列按最近尝试时间轮转，只在成功时打点等于让持续失败的行
// 永远霸占队头。刻意不动 updated_at——那一列是兄弟记录继承和列表排序的依据。
func MarkDoubanAttempt(globalID int) error {
	if globalID <= 0 {
		return nil
	}
	_, err := instance.Exec(`UPDATE global_video SET `+doubanAttemptColumn+` = ? WHERE id = ?`,
		time.Now().Format(doubanCooldownFormat), globalID)
	if err != nil {
		applog.Error("[Douban] MarkDoubanAttempt failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// doubanFailuresRow 只带失败计数：冷却时刻留在库里比，不读到 Go 再解析（见
// IsDoubanSearchOnCooldown）。
type doubanFailuresRow struct {
	Failures int `db:"douban_search_failures"`
}

// IsDoubanSearchOnCooldown 检查这一行是否还在搜索冷却期内。
// globalID <= 0（没有行身份，例如用户在界面上手搜）视为不在冷却期。
//
// 比较必须在 SQL 里做完，不能把 douban_cooldown_until 读进 Go 再 time.Parse：
// 那一列声明成 DATETIME，modernc 驱动按 decltype 把读回的值当日期处理，扫进
// *string 得到的是 "2026-09-29T01:50:25Z"，而不是写进去的 "2026-09-29 01:50:25"，
// 解析永远失败。以前这里解析失败就静默 return false，于是 24 小时冷却一次也没生效过，
// 查无此片的记录每轮批量都重新去撞豆瓣。库里存的就是文本，和写侧同一个布局，
// SQL 的字符串比较即时间先后。
func IsDoubanSearchOnCooldown(globalID int) bool {
	if globalID <= 0 {
		return false
	}
	var onCooldown int
	// NULL > ? 落到 CASE 的 ELSE，所以「没冷却」和「冷却已过」共用一条语句。
	err := instance.Get(&onCooldown, `SELECT CASE WHEN douban_cooldown_until > ? THEN 1 ELSE 0 END
		FROM global_video WHERE id = ?`, time.Now().Format(doubanCooldownFormat), globalID)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		applog.Error("[Douban] IsDoubanSearchOnCooldown global_id=%d 查询失败，按未冷却处理: %v", globalID, err)
		return false
	}
	return onCooldown == 1
}

// MarkDoubanSearchFailure 给这一行累计一次「正常页但没认出头」的失败，达到阈值进冷却。
//
// 调用方必须已经确认拿到过正常搜索页——网络错误或验证页不该走到这里。
// 这里也不再 GetOrCreateGlobalID：为一次失败的搜索去新建 global_video 行，
// 等于让采集结果凭空多出一条永远补不全的记录。
func MarkDoubanSearchFailure(globalID int) error {
	if globalID <= 0 {
		return nil
	}
	var row doubanFailuresRow
	if err := instance.Get(&row, `SELECT COALESCE(douban_search_failures, 0) AS douban_search_failures
		FROM global_video WHERE id = ?`, globalID); err != nil {
		return err
	}
	newCount := row.Failures + 1
	cooldownUntil := ""
	if newCount >= searchFailureCooldownThreshold {
		cooldownUntil = time.Now().Add(doubanCooldownHours * time.Hour).Format(doubanCooldownFormat)
	}
	_, err := instance.Exec(`UPDATE global_video SET douban_search_failures = ?, douban_cooldown_until = COALESCE(NULLIF(?, ''), douban_cooldown_until) WHERE id = ?`,
		newCount, cooldownUntil, globalID)
	if err != nil {
		applog.Error("[Douban] MarkDoubanSearchFailure failed for global_id=%d: %v", globalID, err)
		return err
	}
	if cooldownUntil != "" {
		applog.Info("[Douban] 搜索冷却生效 global_id=%d (failures=%d, until=%s)", globalID, newCount, cooldownUntil)
	}
	return nil
}

// ClearDoubanSearchFailure 搜索成功时清掉本行的失败计数与冷却。
// 只清这一行：同名的其它记录各有各的尝试结果，不该跟着一起放行。
func ClearDoubanSearchFailure(globalID int) error {
	if globalID <= 0 {
		return nil
	}
	_, err := instance.Exec(`UPDATE global_video SET douban_search_failures = 0, douban_cooldown_until = NULL WHERE id = ?`, globalID)
	if err != nil {
		applog.Error("[Douban] ClearDoubanSearchFailure failed for global_id=%d: %v", globalID, err)
	}
	return err
}

// ErrDoubanNotFound 错误
var ErrDoubanNotFound = errors.New("douban info not found")
