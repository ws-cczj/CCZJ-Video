package handler

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

func GetAllSources() ([]*model.Source, error) {
	return db.GetAllSources()
}

func GetEnabledSources() ([]*model.Source, error) {
	return db.GetEnabledSources()
}

var nonAlpha = regexp.MustCompile(`[^a-z0-9]`)

func deriveKey(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	host = strings.TrimPrefix(host, "api.")
	parts := strings.Split(host, ".")
	if len(parts) >= 2 && (parts[len(parts)-1] == "com" || parts[len(parts)-1] == "cn" || parts[len(parts)-1] == "me" || parts[len(parts)-1] == "cc") {
		return nonAlpha.ReplaceAllString(parts[len(parts)-2], "_")
	}
	return nonAlpha.ReplaceAllString(parts[0], "_")
}

func AddSource(s *model.Source) error {
	if s == nil {
		return apperror.New(apperror.Validation, "source is required")
	}
	if s.ApiUrl == "" {
		return apperror.New(apperror.Validation, "api_url is required")
	}
	if _, err := url.Parse(s.ApiUrl); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid api_url")
	}
	if s.SourceKey == "" {
		s.SourceKey = deriveKey(s.ApiUrl)
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid source_key")
	}
	if s.Name == "" {
		s.Name = s.SourceKey
	}

	existing, _ := db.GetSourceByKey(s.SourceKey)
	if existing != nil {
		return apperror.Newf(apperror.Conflict, "来源 %s 已存在", s.SourceKey)
	}
	if err := db.AddSource(s); err != nil {
		return err
	}
	// 当用户当前没有默认源时，自动把这个新源设为默认源
	// 这样首次添加源后前端可以立即拿到一个可用的默认源
	if cur, _ := db.GetSetting("default_source_key"); cur == "" {
		_ = db.SetSetting("default_source_key", s.SourceKey)
	}
	return nil
}

func UpdateSource(s *model.Source) error {
	if s == nil {
		return apperror.New(apperror.Validation, "source is required")
	}
	if err := model.ValidateSourceKey(s.SourceKey); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid source_key")
	}
	// 三个 JSON 列不归这个入口管：adv_config 与 schedule_config 在 model.Source 上是
	// `json:"-"`，前端根本拿不到，strategy_config 虽然能拿到但只在选扩展包适配时才有值，
	// 而 db.UpdateSource 是无条件整列覆盖 —— 只要用户改一次源名，定时采集计划、高级配置、
	// 采集策略就会被静默清空。所以入参为空时一律沿用库里的原值，
	// 要改这些配置得走 SetSourceSchedule / SetSourceStrategy 这类专门入口。
	existing, existingErr := db.GetSourceByKey(s.SourceKey)
	if existingErr == nil && existing != nil {
		if s.AdvConfigRaw == "" {
			s.AdvConfigRaw = existing.AdvConfigRaw
		}
		if s.ScheduleCfgRaw == "" {
			s.ScheduleCfgRaw = existing.ScheduleCfgRaw
		}
		if s.StrategyConfig == "" {
			s.StrategyConfig = existing.StrategyConfig
		}
	}
	// 从「停用」被手动改回「启用」：用户宣布这个源恢复了，旧的失败样本必须一起作废。
	// 不作废的话连败计数还停在自动停用的阈值上，下一次探测刚回来就会把它再次停用，
	// 用户在界面上看到的就是「我明明开了，它自己又关了」。
	reopened := existingErr == nil && existing != nil && existing.Enabled != 1 && s.Enabled == 1
	if err := db.UpdateSource(s); err != nil {
		return err
	}
	if reopened {
		if err := db.ResetSourceHealth(db.DB(), s.SourceKey); err != nil {
			applog.Warn("[Source] 重置 %s 的健康度历史失败: %v", s.SourceKey, err)
		}
	}
	cache.InvalidateSource(s.SourceKey, "采集源配置已修改")
	return nil
}

// SetSourceStrategy 只改某个源的 strategy_config 一列：扩展包适配文档由前端原样透传，
// 传空串表示退回内置 standard_cms。它和 UpdateSource 分开，是为了让"改个源名"不会抹掉采集策略。
func SetSourceStrategy(key, strategyConfig string) error {
	if err := db.SetSourceStrategyConfig(key, strategyConfig); err != nil {
		return err
	}
	cache.InvalidateSource(key, "采集源策略已修改")
	return nil
}

func DeleteSource(key string) error {
	if err := model.ValidateSourceKey(key); err != nil {
		return apperror.Wrap(apperror.Validation, err, "invalid source_key")
	}
	if err := db.DeleteSource(key); err != nil {
		return err
	}
	cache.InvalidateSource(key, "采集源已删除")
	return nil
}

func GetSourceStats() ([]model.SourceStat, error) {
	return db.GetSourceStats()
}

// ======================== 数据源详情 / 操作 ========================

// SourceTableSummary 描述该源下某一张表的信息
type SourceTableSummary struct {
	TableName string           `json:"table_name"`
	Role      string           `json:"role"` // video / episode / type
	RowCount  int              `json:"row_count"`
	Columns   []db.TableColumn `json:"columns"`
}

// SourceDetail 返回该源的"字段 + 示例"，用于设置页面展示
type SourceDetail struct {
	SourceKey string               `json:"source_key"`
	Name      string               `json:"name"`
	ApiUrl    string               `json:"api_url"`
	Tables    []SourceTableSummary `json:"tables"`
	Samples   []*model.Video       `json:"sample_videos"`
	Episodes  []*model.Episode     `json:"sample_episodes"`
}

func GetSourceDetail(sourceKey string) (*SourceDetail, error) {
	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, apperror.Newf(apperror.NotFound, "source not found: %s", sourceKey)
	}

	tables := []SourceTableSummary{
		{TableName: "source_videos", Role: "catalog"},
		{TableName: "global_types", Role: "type"},
	}
	for i := range tables {
		t := &tables[i]
		cols, cErr := db.GetTableColumns(t.TableName)
		if cErr == nil {
			t.Columns = cols
		}
		if db.TableExists(t.TableName) {
			var cnt int
			_ = db.DB().Get(&cnt, fmt.Sprintf(`SELECT COUNT(1) FROM %s`, t.TableName))
			t.RowCount = cnt
		}
	}

	samples, _, _ := db.GetCatalogVideos(sourceKey, db.FilterParams{Page: 1, PageSize: 5})
	episodes := []*model.Episode{}

	return &SourceDetail{
		SourceKey: src.SourceKey,
		Name:      src.Name,
		ApiUrl:    src.ApiUrl,
		Tables:    tables,
		Samples:   samples,
		Episodes:  episodes,
	}, nil
}

// TruncateSourceData 仅清空该源的视频/剧集/分类数据，保留 source 元信息
func TruncateSourceData(sourceKey string) (bool, error) {
	if err := db.TruncateSource(sourceKey); err != nil {
		return false, err
	}
	cache.InvalidateSource(sourceKey, "源数据已清空")
	return true, nil
}

// RecreateSourceTables 删除并重建该源的两张表（数据全部丢失）
func RecreateSourceTables(sourceKey string) (bool, error) {
	if err := db.TruncateSource(sourceKey); err != nil {
		return false, err
	}
	cache.InvalidateSource(sourceKey, "源表已重建")
	return true, nil
}

// DeleteSourceVideo 精确删除该源下的某一条 vod_id
func DeleteSourceVideo(sourceKey string, vodId string) (bool, error) {
	if err := cache.DeleteCatalogVideo(sourceKey, vodId, "源内删除单条"); err != nil {
		return false, err
	}
	return true, nil
}
