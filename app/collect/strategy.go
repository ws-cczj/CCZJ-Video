package collect

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// SourceStrategy builds one URL for one operation. Callers must never append
// query parameters after this boundary.
//
// The interface is the seam between the collector and every source shape, so it
// stays frozen: strategies that can read a non-MAC-CMS envelope advertise it
// through the optional ResponseShaper capability below, which the fetch path
// type-asserts. No implementation is forced to change to support one.
type SourceStrategy interface {
	BuildListUrl(page int, opts FetchOptions) string
	BuildDetailUrl(vodID string) string
	BuildSearchUrl(keyword string, page int) string
	GetFieldMapping() map[string]string
	GetStrategyName() string
}

// ResponseShaper is the narrow optional capability a SourceStrategy implements
// when the source's envelope differs from MAC CMS. ok is false for strategies
// that did not declare a "response" block, which keeps the historical decoder
// path in charge.
type ResponseShaper interface {
	ResponseShape() (*ResponseShape, bool)
}

type ParamConfig struct {
	Action       string `json:"action,omitempty"`
	ActionParam  string `json:"action_param,omitempty"`
	PageParam    string `json:"page_param,omitempty"`
	LimitParam   string `json:"limit_param,omitempty"`
	TypeParam    string `json:"type_param,omitempty"`
	KeywordParam string `json:"keyword_param,omitempty"`
	HoursParam   string `json:"hours_param,omitempty"`
	IDParam      string `json:"id_param,omitempty"`
	// Extra holds fixed parameters that belong to this operation only.
	Extra map[string]string `json:"extra,omitempty"`
}

// StrategyConfig is persisted JSON v2. Extras are deliberately separated by
// operation, so a detail-only option cannot leak into list/search requests.
type StrategyConfig struct {
	Version      int               `json:"version"`
	Strategy     string            `json:"strategy"`
	List         ParamConfig       `json:"list"`
	Search       ParamConfig       `json:"search"`
	Detail       ParamConfig       `json:"detail"`
	Response     *ResponseShape    `json:"response,omitempty"`
	FieldMapping map[string]string `json:"field_mapping,omitempty"`
	ApiUrl       string            `json:"-"`
}

type configuredStrategy struct{ cfg StrategyConfig }

func defaultConfig(api string) StrategyConfig {
	return StrategyConfig{Version: 2, Strategy: "standard_cms", ApiUrl: api,
		List:   ParamConfig{Action: "detail", PageParam: "pg", LimitParam: "limit", TypeParam: "t", HoursParam: "h"},
		Search: ParamConfig{Action: "detail", PageParam: "pg", LimitParam: "limit", KeywordParam: "wd"},
		Detail: ParamConfig{Action: "detail", IDParam: "ids"}}
}

// defaultActionParam is the MAC-CMS query key that carries an operation's action
// value. Declarative sources that spell it differently set "action_param".
const defaultActionParam = "ac"

func normalizeConfig(c *StrategyConfig) {
	d := defaultConfig(c.ApiUrl)
	if c.Version == 0 {
		c.Version = 2
	}
	if c.Strategy == "" {
		c.Strategy = "standard_cms"
	}
	// cms_videolist has the same CMS response envelope but uses the legacy
	// videolist action unless an operation explicitly overrides it.
	if c.Strategy == "cms_videolist" {
		if c.List.Action == "" {
			c.List.Action = "videolist"
		}
		if c.Search.Action == "" {
			c.Search.Action = "videolist"
		}
		if c.Detail.Action == "" {
			c.Detail.Action = "videolist"
		}
	}
	// declarative is the pack-authored driver: it emits what it declares. It
	// still inherits the MAC-CMS *parameter names* below (pg/limit/t/h/wd/ids,
	// action key ac) so a pack that only describes an envelope keeps working,
	// but it gets no action value for free: an unset "action" must produce no
	// action parameter at all, otherwise a non-MAC endpoint would be sent a
	// bogus ac=detail that no pack can switch off.
	if c.Strategy != "declarative" {
		if c.List.Action == "" {
			c.List.Action = d.List.Action
		}
		if c.Search.Action == "" {
			c.Search.Action = d.Search.Action
		}
		if c.Detail.Action == "" {
			c.Detail.Action = d.Detail.Action
		}
	}
	if c.List.PageParam == "" {
		c.List.PageParam = d.List.PageParam
	}
	if c.List.LimitParam == "" {
		c.List.LimitParam = d.List.LimitParam
	}
	if c.List.TypeParam == "" {
		c.List.TypeParam = d.List.TypeParam
	}
	if c.List.HoursParam == "" {
		c.List.HoursParam = d.List.HoursParam
	}
	if c.Search.PageParam == "" {
		c.Search.PageParam = d.Search.PageParam
	}
	if c.Search.LimitParam == "" {
		c.Search.LimitParam = d.Search.LimitParam
	}
	if c.Search.KeywordParam == "" {
		c.Search.KeywordParam = d.Search.KeywordParam
	}
	if c.Detail.IDParam == "" {
		c.Detail.IDParam = d.Detail.IDParam
	}
	// A missing response block stays nil: the envelope then keeps being decoded
	// the way it always has been.
	if c.Response != nil {
		c.Response = c.Response.withDefaults()
	}
	if c.Strategy == "declarative" {
		c.FieldMapping = canonicalizeFieldMapping(c.FieldMapping)
	}
}
func (s *configuredStrategy) BuildListUrl(page int, opts FetchOptions) string {
	p := map[string]string{s.cfg.List.PageParam: strconv.Itoa(page)}
	if opts.Limit > 0 {
		p[s.cfg.List.LimitParam] = strconv.Itoa(opts.Limit)
	}
	if strings.TrimSpace(opts.TypeID) != "" {
		p[s.cfg.List.TypeParam] = strings.TrimSpace(opts.TypeID)
	}
	if opts.Hours > 0 {
		p[s.cfg.List.HoursParam] = strconv.Itoa(opts.Hours)
	}
	return buildOperationURL(s.cfg.ApiUrl, s.cfg.List, p)
}
func (s *configuredStrategy) BuildSearchUrl(keyword string, page int) string {
	if strings.TrimSpace(keyword) == "" {
		return ""
	}
	p := map[string]string{s.cfg.Search.PageParam: strconv.Itoa(page), s.cfg.Search.KeywordParam: strings.TrimSpace(keyword)}
	return buildOperationURL(s.cfg.ApiUrl, s.cfg.Search, p)
}
func (s *configuredStrategy) BuildDetailUrl(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	return buildOperationURL(s.cfg.ApiUrl, s.cfg.Detail, map[string]string{s.cfg.Detail.IDParam: strings.TrimSpace(id)})
}
func (s *configuredStrategy) GetFieldMapping() map[string]string { return s.cfg.FieldMapping }
func (s *configuredStrategy) GetStrategyName() string            { return s.cfg.Strategy }

// ResponseShape satisfies the optional ResponseShaper capability. The shape is
// copied so no fetch can write back into the strategy's own config.
func (s *configuredStrategy) ResponseShape() (*ResponseShape, bool) {
	if s.cfg.Response == nil {
		return nil, false
	}
	return s.cfg.Response.withDefaults(), true
}

// actionParamName is the query key carrying this operation's action value.
// Unset means the MAC-CMS "ac", which keeps every pre-declarative config
// producing the byte-for-byte query string it produced before.
func (op ParamConfig) actionParamName() string {
	if name := strings.TrimSpace(op.ActionParam); name != "" {
		return name
	}
	return defaultActionParam
}

func buildOperationURL(base string, op ParamConfig, values map[string]string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	q := u.Query()
	// Remove all controlled names inherited from the base URL. This prevents a
	// previous operation's ids/wd/h from surviving into this request. The action
	// key is only scrubbed when it is actually this operation's action key, so a
	// declarative source whose base URL carries an unrelated ac= stays intact.
	for _, k := range []string{"pg", "limit", "t", "wd", "h", "ids", op.actionParamName(), op.PageParam, op.LimitParam, op.TypeParam, op.KeywordParam, op.HoursParam, op.IDParam} {
		if k != "" {
			q.Del(k)
		}
	}
	if op.Action != "" {
		q.Set(op.actionParamName(), op.Action)
	}
	for k, v := range op.Extra {
		q.Set(k, v)
	}
	for k, v := range values {
		if k != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

type StrategyFactory struct{}

func (f *StrategyFactory) CreateStrategy(api string, config *StrategyConfig) SourceStrategy {
	c := defaultConfig(api)
	if config != nil {
		c = *config
		c.ApiUrl = api
	}
	normalizeConfig(&c)
	return &configuredStrategy{c}
}
func (f *StrategyFactory) CreateStrategyFromSource(source *model.Source) SourceStrategy {
	if source == nil || strings.TrimSpace(source.ApiUrl) == "" {
		return nil
	}
	c := StrategyConfig{ApiUrl: source.ApiUrl}
	if raw := strings.TrimSpace(source.StrategyConfig); raw != "" {
		parsed := StrategyConfig{}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			// A hand-edited or half-written config must not brick the source for
			// good: keep the API URL, drop the unusable params, and collect with
			// standard CMS defaults instead.
			applog.Warn("[Strategy] 源 %s 的 strategy_config 解析失败，已回退默认策略: %v", source.SourceKey, err)
		} else {
			c = parsed
			c.ApiUrl = source.ApiUrl
		}
	}
	normalizeConfig(&c)
	return &configuredStrategy{c}
}

var DefaultFactory = &StrategyFactory{}

func CreateStrategy(api string) SourceStrategy { return DefaultFactory.CreateStrategy(api, nil) }
func CreateStrategyFromSource(source *model.Source) SourceStrategy {
	return DefaultFactory.CreateStrategyFromSource(source)
}
