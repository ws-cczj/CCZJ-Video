package collect

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"cczjVideo/app/model"
)

// SourceStrategy 数据源策略接口
type SourceStrategy interface {
	// BuildListUrl 构建列表接口URL
	BuildListUrl(page int, opts FetchOptions) string

	// BuildDetailUrl 构建详情接口URL
	BuildDetailUrl(vodId string) string

	// BuildSearchUrl 构建搜索接口URL
	BuildSearchUrl(keyword string, page int) string

	// GetFieldMapping 获取字段映射
	GetFieldMapping() map[string]string

	// GetStrategyName 获取策略名称
	GetStrategyName() string
}

// StrategyConfig 策略配置，定义接口参数组合
type StrategyConfig struct {
	ListParams struct {
		PageParam    string            // 页码参数名，如 "pg", "page", "p"
		LimitParam   string            // 条数参数名，如 "limit", "size"
		TypeParam    string            // 分类参数名，如 "t", "type", "category"
		KeywordParam string            // 搜索参数名，如 "wd", "keyword", "q"
		HoursParam   string            // 时间筛选参数名，如 "h", "hours"
		FixedParams  map[string]string // 固定参数，如 {"ac": "list"}
	} `json:"list_params"`

	DetailParams struct {
		FixedParams map[string]string // 固定参数，如 {"ac": "videolist"}
		IDParam     string            // ID参数名，如 "ids", "id", "vod_id"
	} `json:"detail_params"`

	SearchParams struct {
		FixedParams map[string]string // 固定参数
	} `json:"search_params"`

	FieldMapping map[string]string `json:"field_mapping"` // 字段映射
	ApiUrl       string            `json:"api_url"`       // 基础API URL
}

// DefaultStrategy 默认策略，兼容大部分源站
type DefaultStrategy struct {
	config *StrategyConfig
}

func NewDefaultStrategy(apiUrl string) *DefaultStrategy {
	return &DefaultStrategy{
		config: &StrategyConfig{
			ApiUrl: apiUrl,
			ListParams: struct {
				PageParam    string
				LimitParam   string
				TypeParam    string
				KeywordParam string
				HoursParam   string
				FixedParams  map[string]string
			}{
				PageParam:    "pg",
				LimitParam:   "limit",
				TypeParam:    "t",
				KeywordParam: "wd",
				HoursParam:   "h",
				FixedParams:  map[string]string{"ac": "list"},
			},
			DetailParams: struct {
				FixedParams map[string]string
				IDParam     string
			}{
				FixedParams: map[string]string{"ac": "videolist"},
				IDParam:     "ids",
			},
			SearchParams: struct {
				FixedParams map[string]string
			}{
				FixedParams: map[string]string{"ac": "list"},
			},
			FieldMapping: nil,
		},
	}
}

func NewDefaultStrategyWithConfig(config *StrategyConfig) *DefaultStrategy {
	if config.ListParams.PageParam == "" {
		config.ListParams.PageParam = "pg"
	}
	if config.ListParams.LimitParam == "" {
		config.ListParams.LimitParam = "limit"
	}
	if config.ListParams.TypeParam == "" {
		config.ListParams.TypeParam = "t"
	}
	if config.ListParams.KeywordParam == "" {
		config.ListParams.KeywordParam = "wd"
	}
	if config.ListParams.HoursParam == "" {
		config.ListParams.HoursParam = "h"
	}
	if config.DetailParams.IDParam == "" {
		config.DetailParams.IDParam = "ids"
	}
	return &DefaultStrategy{config: config}
}

func (s *DefaultStrategy) BuildListUrl(page int, opts FetchOptions) string {
	return buildUrlWithParams(s.config.ApiUrl, map[string]string{
		s.config.ListParams.PageParam: strconv.Itoa(page),
	}, s.config.ListParams.FixedParams, map[string]string{
		s.config.ListParams.LimitParam:   strconv.Itoa(opts.Limit),
		s.config.ListParams.HoursParam:   strconv.Itoa(opts.Hours),
		s.config.ListParams.KeywordParam: opts.Keyword,
		s.config.ListParams.TypeParam:    opts.TypeID,
	})
}

func (s *DefaultStrategy) BuildDetailUrl(vodId string) string {
	return buildUrlWithParams(s.config.ApiUrl, map[string]string{
		s.config.DetailParams.IDParam: vodId,
	}, s.config.DetailParams.FixedParams, nil)
}

func (s *DefaultStrategy) BuildSearchUrl(keyword string, page int) string {
	return buildUrlWithParams(s.config.ApiUrl, map[string]string{
		s.config.ListParams.PageParam:    strconv.Itoa(page),
		s.config.ListParams.KeywordParam: keyword,
	}, s.config.SearchParams.FixedParams, nil)
}

func (s *DefaultStrategy) GetFieldMapping() map[string]string {
	return s.config.FieldMapping
}

func (s *DefaultStrategy) GetStrategyName() string {
	return "default"
}

// CustomStrategy 自定义策略，完全由配置驱动
type CustomStrategy struct {
	config *StrategyConfig
}

func NewCustomStrategy(config *StrategyConfig) *CustomStrategy {
	return &CustomStrategy{config: config}
}

func (s *CustomStrategy) BuildListUrl(page int, opts FetchOptions) string {
	return buildUrlWithParams(s.config.ApiUrl, map[string]string{
		s.config.ListParams.PageParam: strconv.Itoa(page),
	}, s.config.ListParams.FixedParams, map[string]string{
		s.config.ListParams.LimitParam:   strconv.Itoa(opts.Limit),
		s.config.ListParams.HoursParam:   strconv.Itoa(opts.Hours),
		s.config.ListParams.KeywordParam: opts.Keyword,
		s.config.ListParams.TypeParam:    opts.TypeID,
	})
}

func (s *CustomStrategy) BuildDetailUrl(vodId string) string {
	return buildUrlWithParams(s.config.ApiUrl, map[string]string{
		s.config.DetailParams.IDParam: vodId,
	}, s.config.DetailParams.FixedParams, nil)
}

func (s *CustomStrategy) BuildSearchUrl(keyword string, page int) string {
	return buildUrlWithParams(s.config.ApiUrl, map[string]string{
		s.config.ListParams.PageParam:    strconv.Itoa(page),
		s.config.ListParams.KeywordParam: keyword,
	}, s.config.SearchParams.FixedParams, nil)
}

func (s *CustomStrategy) GetFieldMapping() map[string]string {
	return s.config.FieldMapping
}

func (s *CustomStrategy) GetStrategyName() string {
	return "custom"
}

// buildUrlWithParams 通用URL构建函数
func buildUrlWithParams(baseUrl string, requiredParams, fixedParams, optionalParams map[string]string) string {
	if baseUrl == "" {
		return ""
	}

	u, err := url.Parse(baseUrl)
	if err != nil {
		return fmt.Sprintf("%s?%s", baseUrl, encodeParams(requiredParams, fixedParams, optionalParams))
	}

	q := u.Query()

	for k, v := range fixedParams {
		q.Set(k, v)
	}

	for k, v := range requiredParams {
		if v != "" {
			q.Set(k, v)
		}
	}

	for k, v := range optionalParams {
		if v != "" && v != "0" {
			q.Set(k, v)
		}
	}

	u.RawQuery = q.Encode()
	return u.String()
}

func encodeParams(paramsList ...map[string]string) string {
	var parts []string
	for _, params := range paramsList {
		for k, v := range params {
			if v != "" && v != "0" {
				parts = append(parts, fmt.Sprintf("%s=%s", url.QueryEscape(k), url.QueryEscape(v)))
			}
		}
	}
	return strings.Join(parts, "&")
}

// StrategyFactory 策略工厂
type StrategyFactory struct{}

func (f *StrategyFactory) CreateStrategy(apiUrl string, config *StrategyConfig) SourceStrategy {
	if config != nil && isCustomConfig(config) {
		return NewCustomStrategy(config)
	}
	return NewDefaultStrategy(apiUrl)
}

func (f *StrategyFactory) CreateStrategyFromSource(source *model.Source) SourceStrategy {
	if source == nil || source.ApiUrl == "" {
		return nil
	}

	if source.StrategyConfig != "" {
		var config StrategyConfig
		if err := parseStrategyConfig(source.StrategyConfig, &config); err == nil {
			config.ApiUrl = source.ApiUrl
			return NewCustomStrategy(&config)
		}
	}

	return NewDefaultStrategy(source.ApiUrl)
}

func isCustomConfig(config *StrategyConfig) bool {
	if config == nil {
		return false
	}
	if len(config.FieldMapping) > 0 {
		return true
	}
	if len(config.ListParams.FixedParams) > 0 && config.ListParams.FixedParams["ac"] != "list" {
		return true
	}
	if len(config.DetailParams.FixedParams) > 0 && config.DetailParams.FixedParams["ac"] != "videolist" {
		return true
	}
	if config.ListParams.PageParam != "" && config.ListParams.PageParam != "pg" {
		return true
	}
	if config.DetailParams.IDParam != "" && config.DetailParams.IDParam != "ids" {
		return true
	}
	return false
}

func parseStrategyConfig(configStr string, config *StrategyConfig) error {
	if configStr == "" {
		return fmt.Errorf("empty config")
	}

	u, err := url.ParseQuery(configStr)
	if err != nil {
		return err
	}

	config.FieldMapping = make(map[string]string)
	for k, v := range u {
		if len(v) > 0 {
			parts := strings.SplitN(k, ":", 2)
			if len(parts) == 2 {
				config.FieldMapping[parts[0]] = parts[1]
			}
		}
	}

	return nil
}

var DefaultFactory = &StrategyFactory{}

func CreateStrategy(apiUrl string) SourceStrategy {
	return DefaultFactory.CreateStrategy(apiUrl, nil)
}

func CreateStrategyFromSource(source *model.Source) SourceStrategy {
	return DefaultFactory.CreateStrategyFromSource(source)
}