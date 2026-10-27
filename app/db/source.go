package db

import (
	"cczjVideo/app/model"
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
)

func GetAllSources() ([]*model.Source, error) {
	return ReadSources(instance)
}

// GetEnabledSources 返回**视频**采集池：启用中的、kind 为 video 的源。
//
// kind 过滤不是可选的。小说与漫画源现在和视频源同表（ADR 0016），而这个函数的
// 两个调用方——定时采集调度器（app/collection/scheduler.go）与豆瓣热榜匹配
// （app/douban/chart.go:455、chart_match.go:15）——都会拿返回的 api_url 当 MAC-CMS
// 接口去拉分页。一个启用的书源进了这个列表，就会被当成视频源发一轮
// ?ac=detail&pg=1 的请求，然后连败三次被自动停用。
func GetEnabledSources() ([]*model.Source, error) {
	return GetEnabledSourcesByKind(model.SourceKindVideo)
}

// GetEnabledSourcesByKind 返回某个模式下启用中的源。
func GetEnabledSourcesByKind(kind model.SourceKind) ([]*model.Source, error) {
	var sources []*model.Source
	err := instance.Select(&sources,
		`SELECT * FROM sources WHERE enabled = 1 AND kind = ? ORDER BY id`, string(kind.Normalize()))
	return sources, err
}

// GetSourcesByKind 返回某个模式下的全部源（含停用的），源管理页要它。
func GetSourcesByKind(kind model.SourceKind) ([]*model.Source, error) {
	return GetSourcesByKindContext(context.Background(), kind)
}

func GetSourcesByKindContext(ctx context.Context, kind model.SourceKind) ([]*model.Source, error) {
	var sources []*model.Source
	err := instance.SelectContext(ctx, &sources, `SELECT * FROM sources WHERE kind = ? ORDER BY id`, string(kind.Normalize()))
	return sources, err
}

func GetSourceByKey(key string) (*model.Source, error) {
	return GetSourceByKeyContext(context.Background(), key)
}

func GetSourceByKeyContext(ctx context.Context, key string) (*model.Source, error) {
	if err := model.ValidateSourceKey(key); err != nil {
		return nil, err
	}
	var s model.Source
	err := instance.GetContext(ctx, &s, `SELECT * FROM sources WHERE source_key = ?`, key)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func AddSource(s *model.Source) error { return AddSourceContext(context.Background(), s) }
func AddSourceContext(ctx context.Context, s *model.Source) error {
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureUniqueVideoEndpoint(ctx, tx, s, false); err != nil {
		return err
	}
	if err := insertSourceContext(ctx, tx, s); err != nil {
		return err
	}
	return tx.Commit()
}
func CreateSourceWithDefault(ctx context.Context, s *model.Source) error {
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureUniqueVideoEndpoint(ctx, tx, s, false); err != nil {
		return err
	}
	if err := insertSourceContext(ctx, tx, s); err != nil {
		return err
	}
	if s.GetKind() == model.SourceKindVideo {
		if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key,value) VALUES('default_source_key',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value WHERE COALESCE(settings.value,'')=''`, s.SourceKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func insertSourceContext(ctx context.Context, executor sqlx.ExtContext, s *model.Source) error {
	if s == nil {
		return fmt.Errorf("source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return err
	}
	// kind 在这里归一并校验：一个拼错的 kind 会让这行既进不了视频采集池、
	// 也进不了阅读模式的源列表，界面上就是一个谁也解释不了的幽灵源。
	s.Kind = string(s.GetKind())
	if err := model.ValidateSourceKind(model.SourceKind(s.Kind)); err != nil {
		return err
	}
	_, err := sqlx.NamedExecContext(ctx, executor, `INSERT INTO sources (source_key, name, api_url, enabled, collect_limit, collect_hours, adv_config, schedule_config, strategy_config, kind)
		VALUES (:source_key, :name, :api_url, :enabled, :collect_limit, :collect_hours, :adv_config, :schedule_config, :strategy_config, :kind)`, s)
	return err
}

func UpdateSource(s *model.Source) error { return UpdateSourceContext(context.Background(), s) }
func UpdateSourceContext(ctx context.Context, s *model.Source) error {
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureUniqueVideoEndpoint(ctx, tx, s, true); err != nil {
		return err
	}
	if err := updateSourceContext(ctx, tx, s, false); err != nil {
		return err
	}
	return tx.Commit()
}
func UpdateSourceMetadataContext(ctx context.Context, s *model.Source) error {
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := ensureUniqueVideoEndpoint(ctx, tx, s, true); err != nil {
		return err
	}
	var enabled int
	if err := tx.GetContext(ctx, &enabled, `SELECT enabled FROM sources WHERE source_key=?`, s.SourceKey); err != nil {
		return err
	}
	if err := updateSourceContext(ctx, tx, s, true); err != nil {
		return err
	}
	if enabled != 1 && s.Enabled == 1 {
		if err := ResetSourceHealth(tx, s.SourceKey); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func updateSourceContext(ctx context.Context, executor sqlx.ExtContext, s *model.Source, preserve bool) error {
	if s == nil {
		return fmt.Errorf("source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return err
	}
	s.Kind = string(s.GetKind())
	if err := model.ValidateSourceKind(model.SourceKind(s.Kind)); err != nil {
		return err
	}
	// auto_disabled_at 一并清零：用户手动存过一次源，就等于重新宣告"这个源可用"，
	// 自动停用的时间戳留着会让调度器在冷却到期时误把它当自动停用去恢复。
	//
	// kind 刻意**不在** SET 里：一个源属于哪个模式是它的身份，改名或改接口地址
	// 都不该顺带把它从视频源变成书源——那会让已经采集的目录与已经加入书架的
	// 条目全部失去归属。要换模式就删掉重建。
	result, err := sqlx.NamedExecContext(ctx, executor, `UPDATE sources SET name=:name, api_url=:api_url, enabled=:enabled,
		collect_limit=:collect_limit, collect_hours=:collect_hours,
		adv_config=CASE WHEN :preserve AND :adv_config='' THEN adv_config ELSE :adv_config END, schedule_config=CASE WHEN :preserve AND :schedule_config='' THEN schedule_config ELSE :schedule_config END, strategy_config=CASE WHEN :preserve AND :strategy_config='' THEN strategy_config ELSE :strategy_config END,
		auto_disabled_at=0
		WHERE source_key=:source_key`, struct {
		*model.Source
		Preserve bool `db:"preserve"`
	}{s, preserve})
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
	return SetSourceStrategyConfigContext(context.Background(), key, strategyConfig)
}

// SetSourceScheduleConfigContext updates only the scheduler-owned column.
func SetSourceScheduleConfigContext(ctx context.Context, key, config string) error {
	result, err := instance.ExecContext(ctx, `UPDATE sources SET schedule_config=? WHERE source_key=? AND kind='video'`, config, key)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func SetSourceStrategyConfigContext(ctx context.Context, key, strategyConfig string) error {
	if err := model.ValidateSourceKey(key); err != nil {
		return err
	}
	res, err := instance.ExecContext(ctx, `UPDATE sources SET strategy_config=? WHERE source_key=?`, strategyConfig, key)
	if err != nil {
		return err
	}
	// UPDATE 命中 0 行不报错，所以这里自己判定：前端报的是"这个源已改用某适配"，
	// 源不存在时不能让它静默成功。
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
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

func DeleteSource(key string) error { return DeleteSourceContext(context.Background(), key) }
func DeleteSourceContext(ctx context.Context, key string) error {
	if err := model.ValidateSourceKey(key); err != nil {
		return err
	}
	tx, err := instance.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin source deletion: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE source_videos SET lifecycle_state='deleted', updated_at=CURRENT_TIMESTAMP WHERE source_key=?`, key); err != nil {
		return fmt.Errorf("retire source catalog: %w", err)
	}
	// 小说/漫画的作品与章节同样只退役不删行：书架与阅读进度还指着它们，
	// 删了会让"这个源没了"变成"这本书从来没存在过"。
	if err := RetireLibraryItemsOfSource(tx, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sources WHERE source_key = ?`, key); err != nil {
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
