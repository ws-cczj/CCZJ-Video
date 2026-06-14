package model

import (
	"encoding/json"
)

// CollectMode 采集模式
type CollectMode string

const (
	CollectModeFull        CollectMode = "full"        // 全量采集：拉取所有分页
	CollectModeIncremental CollectMode = "incremental" // 增量/补采：按时间窗采集（h参数）
	CollectModeOnce        CollectMode = "once"        // 单次采集：采集一次后停止
)

// AdvConfig 源的高级配置（JSON 存储在 sources.adv_config 列）
type AdvConfig struct {
	UrlTemplate  string            `json:"url_template,omitempty"`  // URL 模板（用于压缩 m3u8 地址）
	UrlPrefix    string            `json:"url_prefix,omitempty"`    // URL 前缀
	UrlSuffix    string            `json:"url_suffix,omitempty"`    // URL 后缀
	CollectLimit int               `json:"collect_limit,omitempty"` // 单页条数（0=使用接口默认）
	CollectHours int               `json:"collect_hours,omitempty"` // 时间窗小时数（增量采集用，0=不限制）
	FieldMapping map[string]string `json:"field_mapping,omitempty"` // 字段映射：源字段名 → 目标字段名（如 {"title":"vod_name"}）
}

// ScheduleConfig 单个源的调度配置（存储在 sources.schedule_config 列）
type ScheduleConfig struct {
	Enabled  bool        `json:"enabled"`   // 是否启用后台周期采集
	Mode     CollectMode `json:"mode"`      // 后台采集模式
	IntervalMin int      `json:"interval_min"` // 采集间隔（分钟），最小 5 分钟
}

// Source 采集源定义
type Source struct {
	Id             int    `json:"id" db:"id"`
	SourceKey      string `json:"source_key" db:"source_key"`
	Name           string `json:"name" db:"name"`
	ApiUrl         string `json:"api_url" db:"api_url"`
	Enabled        int    `json:"enabled" db:"enabled"`
	CreatedAt      string `json:"created_at" db:"created_at"`

	// 高级配置（JSON 字符串，DB 列）
	AdvConfigRaw   string `json:"-" db:"adv_config"`
	// 调度配置（JSON 字符串，DB 列，null 表示使用全局默认）
	ScheduleCfgRaw string `json:"-" db:"schedule_config"`
	// 策略配置（JSON 字符串，DB 列，用于自定义数据源参数组合）
	StrategyConfig string `json:"strategy_config,omitempty" db:"strategy_config"`

	// === 以下为兼容旧字段（逐步废弃），读 DB 后自动展开到 AdvConfig ===
	UrlTemplate  string `json:"url_template,omitempty" db:"url_template"`
	UrlPrefix    string `json:"url_prefix,omitempty" db:"url_prefix"`
	UrlSuffix    string `json:"url_suffix,omitempty" db:"url_suffix"`
	CollectLimit int    `json:"collect_limit,omitempty" db:"collect_limit"`
	CollectHours int    `json:"collect_hours,omitempty" db:"collect_hours"`
}

// GetAdvConfig 解析高级配置
func (s *Source) GetAdvConfig() AdvConfig {
	cfg := AdvConfig{}
	if s.AdvConfigRaw != "" {
		json.Unmarshal([]byte(s.AdvConfigRaw), &cfg)
	}
	// 兼容旧字段：如果 AdvConfig 为空但旧字段有值，则用旧字段值
	if cfg.UrlTemplate == "" && s.UrlTemplate != "" {
		cfg.UrlTemplate = s.UrlTemplate
	}
	if cfg.UrlPrefix == "" && s.UrlPrefix != "" {
		cfg.UrlPrefix = s.UrlPrefix
	}
	if cfg.UrlSuffix == "" && s.UrlSuffix != "" {
		cfg.UrlSuffix = s.UrlSuffix
	}
	if cfg.CollectLimit == 0 && s.CollectLimit > 0 {
		cfg.CollectLimit = s.CollectLimit
	}
	if cfg.CollectHours == 0 && s.CollectHours > 0 {
		cfg.CollectHours = s.CollectHours
	}
	return cfg
}

// SetAdvConfig 序列化高级配置到 AdvConfigRaw
func (s *Source) SetAdvConfig(cfg AdvConfig) {
	b, _ := json.Marshal(cfg)
	s.AdvConfigRaw = string(b)
	// 同步到旧字段（兼容读取）
	s.UrlTemplate = cfg.UrlTemplate
	s.UrlPrefix = cfg.UrlPrefix
	s.UrlSuffix = cfg.UrlSuffix
	s.CollectLimit = cfg.CollectLimit
	s.CollectHours = cfg.CollectHours
}

// GetScheduleConfig 解析调度配置
func (s *Source) GetScheduleConfig() *ScheduleConfig {
	if s.ScheduleCfgRaw == "" {
		return nil // 返回 nil 表示使用全局默认
	}
	var cfg ScheduleConfig
	if err := json.Unmarshal([]byte(s.ScheduleCfgRaw), &cfg); err != nil {
		return nil
	}
	return &cfg
}

// SetScheduleConfig 序列化调度配置
func (s *Source) SetScheduleConfig(cfg *ScheduleConfig) {
	if cfg == nil {
		s.ScheduleCfgRaw = ""
		return
	}
	b, _ := json.Marshal(cfg)
	s.ScheduleCfgRaw = string(b)
}

type SourceStat struct {
	SourceKey    string `json:"source_key" db:"source_key"`
	Name         string `json:"name" db:"name"`
	VideoCount   int    `json:"video_count" db:"video_count"`
	EpisodeCount int    `json:"episode_count" db:"episode_count"`
}