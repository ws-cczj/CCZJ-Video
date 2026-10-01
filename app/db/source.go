package db

import (
	"cczjVideo/app/model"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

func GetAllSources() ([]*model.Source, error) {
	return ReadSources(instance)
}

func GetEnabledSources() ([]*model.Source, error) {
	var sources []*model.Source
	err := instance.Select(&sources, `SELECT * FROM sources WHERE enabled = 1 ORDER BY id`)
	return sources, err
}

func GetSourceByKey(key string) (*model.Source, error) {
	if err := model.ValidateSourceKey(key); err != nil {
		return nil, err
	}
	var s model.Source
	err := instance.Get(&s, `SELECT * FROM sources WHERE source_key = ?`, key)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func AddSource(s *model.Source) error {
	if s == nil {
		return fmt.Errorf("source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return err
	}
	_, err := instance.NamedExec(`INSERT INTO sources (source_key, name, api_url, enabled, collect_limit, collect_hours, adv_config, schedule_config, strategy_config)
		VALUES (:source_key, :name, :api_url, :enabled, :collect_limit, :collect_hours, :adv_config, :schedule_config, :strategy_config)`, s)
	return err
}

func UpdateSource(s *model.Source) error {
	if s == nil {
		return fmt.Errorf("source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return err
	}
	// auto_disabled_at 一并清零：用户手动存过一次源，就等于重新宣告"这个源可用"，
	// 自动停用的时间戳留着会让调度器在冷却到期时误把它当自动停用去恢复。
	_, err := instance.NamedExec(`UPDATE sources SET name=:name, api_url=:api_url, enabled=:enabled,
		collect_limit=:collect_limit, collect_hours=:collect_hours,
		adv_config=:adv_config, schedule_config=:schedule_config, strategy_config=:strategy_config,
		auto_disabled_at=0
		WHERE source_key=:source_key`, s)
	return err
}

// AutoDisableSource 把连续失败的源停用，并记下停用时刻。
//
// 返回是否真的改动了行：用户早已手动关掉这个源时返回 false，调用方据此决定要不要
// 在日志里播报"已自动停用"，否则每次巡检都会重复喊一遍。
func AutoDisableSource(key string, at time.Time) (bool, error) {
	if err := model.ValidateSourceKey(key); err != nil {
		return false, err
	}
	res, err := instance.Exec(`UPDATE sources SET enabled=0, auto_disabled_at=? WHERE source_key=? AND enabled<>0`,
		at.Unix(), key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// RecoverAutoDisabledSources 把冷却到期的自动停用源放回采集池，返回这些源的 key。
//
// 只认 auto_disabled_at<>0 的行：用户自己关掉的源永远不该被这里打开。清掉时间戳
// 是为了让下一次连败从零计数，同时把健康度历史一起删掉——否则残留的连败样本会在
// 恢复后的第一次失败上立刻把它再停用，冷却就成了摆设。
func RecoverAutoDisabledSources(before time.Time) ([]string, error) {
	var keys []string
	if err := instance.Select(&keys,
		`SELECT source_key FROM sources WHERE auto_disabled_at<>0 AND auto_disabled_at<=? ORDER BY auto_disabled_at`,
		before.Unix()); err != nil {
		return nil, fmt.Errorf("read auto-disabled sources: %w", err)
	}
	if len(keys) == 0 {
		return nil, nil
	}
	tx, err := instance.Beginx()
	if err != nil {
		return nil, fmt.Errorf("begin auto-disable recovery: %w", err)
	}
	defer tx.Rollback()
	for _, key := range keys {
		if _, err := tx.Exec(`UPDATE sources SET enabled=1, auto_disabled_at=0 WHERE source_key=?`, key); err != nil {
			return nil, fmt.Errorf("recover source %s: %w", key, err)
		}
		if err := ResetSourceHealth(tx, key); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit auto-disable recovery: %w", err)
	}
	return keys, nil
}

// SetSourceStrategyConfig 只写 strategy_config 一列。
// 采集源的三个 JSON 配置列各有各的 owner：改名称/接口地址走 UpdateSource，
// 定时计划走调度入口，策略（含扩展包适配）必须只动这一列，否则一次改名就会抹掉配置。
// 传空串表示清除声明式策略，回到内置 standard_cms。
func SetSourceStrategyConfig(key, strategyConfig string) error {
	if err := model.ValidateSourceKey(key); err != nil {
		return err
	}
	res, err := instance.Exec(`UPDATE sources SET strategy_config=? WHERE source_key=?`, strategyConfig, key)
	if err != nil {
		return err
	}
	// UPDATE 命中 0 行不报错，所以这里自己判定：前端报的是"这个源已改用某适配"，
	// 源不存在时不能让它静默成功。
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return fmt.Errorf("来源 %s 不存在", key)
	}
	return nil
}

// migrateSourceAutoDisable 给 sources 补 auto_disabled_at 列。
//
// 自动停用需要一个能和「用户手动关闭」区分开的记号：只有机器做掉的停用才允许冷却
// 到期后自动放回采集池，手动关掉的源不该被程序擅自打开。列存 Unix 秒而不是
// DATETIME，是因为 modernc 把 DATETIME 读回成 RFC3339 字符串，Go 侧比时间还得再解析。
func migrateSourceAutoDisable(tx *sqlx.Tx) error {
	return addColumnIfMissing(tx, "sources", "auto_disabled_at", "INTEGER NOT NULL DEFAULT 0")
}

func DeleteSource(key string) error {
	if err := model.ValidateSourceKey(key); err != nil {
		return err
	}
	tx, err := instance.Beginx()
	if err != nil {
		return fmt.Errorf("begin source deletion: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE source_videos SET lifecycle_state='deleted', updated_at=CURRENT_TIMESTAMP WHERE source_key=?`, key); err != nil {
		return fmt.Errorf("retire source catalog: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM sources WHERE source_key = ?`, key); err != nil {
		return fmt.Errorf("delete source record: %w", err)
	}
	if err := ResetCollectCursor(tx, key); err != nil {
		return err
	}
	if err := ResetSourceHealth(tx, key); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit source deletion: %w", err)
	}
	return nil
}

func GetSourceStats() ([]model.SourceStat, error) {
	sources, err := GetAllSources()
	if err != nil {
		return nil, err
	}
	var stats []model.SourceStat
	for _, s := range sources {
		stats = append(stats, model.SourceStat{
			SourceKey:    s.SourceKey,
			Name:         s.Name,
			VideoCount:   catalogCount(s.SourceKey),
			EpisodeCount: 0,
		})
	}
	return stats, nil
}

func catalogCount(sourceKey string) int {
	var count int
	_ = instance.Get(&count, `SELECT COUNT(*) FROM source_videos WHERE source_key=? AND lifecycle_state='active'`, sourceKey)
	return count
}
