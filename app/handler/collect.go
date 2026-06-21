package handler

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/util"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

type CollectReq struct {
	SourceKey string `json:"source_key"`
	Mode      string `json:"mode"`  // "full" | "incremental" | "once"（空=full）
	Hours     int    `json:"hours"` // 增量模式的回溯小时数（空=使用源配置）
}

// SourceScheduleReq 设置单个源的调度配置请求
type SourceScheduleReq struct {
	SourceKey   string `json:"source_key"`
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`         // full | incremental
	IntervalMin int    `json:"interval_min"` // 定时间隔（分钟），最小 5
}

// CollectStatus 暴露给前端的采集状态
type CollectStatus struct {
	SourceKey string   `json:"source_key"`
	Running   bool     `json:"running"`
	Paused    bool     `json:"paused"`
	Current   int      `json:"current"`
	Total     int      `json:"total"`
	Page      int      `json:"page"`  // 当前正在采集的页码
	Names     []string `json:"names"` // 当前页的视频名称
	Log       string   `json:"log"`
	Mode      string   `json:"mode"`  // 当前采集模式
}

// CollectScheduleConfig 采集调度配置（持久化到 settings 表）
type CollectScheduleConfig struct {
	// 是否启用后台周期采集
	EnableBackground bool `json:"enable_background"`
	// 后台周期采集间隔（秒），默认 60
	BackgroundIntervalSeconds int `json:"background_interval_seconds"`
	// 向后兼容字段：旧版分钟（会在读取时转换为 seconds）
	BackgroundIntervalMinutes int `json:"background_interval_minutes,omitempty"`
	// 是否在软件启动时执行"从上次退出时间到现在"的补采
	EnableStartupCatchup bool `json:"enable_startup_catchup"`
	// 启动后首次全量采集是否启用（初始化数据库）
	EnableInitialFullCollect bool `json:"enable_initial_full_collect"`
	// 顺序采集两个 source 之间的最小间隔（秒）
	SourceGapSeconds int `json:"source_gap_seconds"`
	// 每页之间等待秒数（覆盖 engine 默认的 30s）
	PageGapSeconds int `json:"page_gap_seconds"`
}

// 默认配置
func defaultScheduleConfig() CollectScheduleConfig {
	return CollectScheduleConfig{
		EnableBackground:          false,
		BackgroundIntervalSeconds: 60,
		EnableStartupCatchup:      false,
		EnableInitialFullCollect:  false,
		SourceGapSeconds:          10,
		PageGapSeconds:            30,
	}
}

const scheduleConfigKey = "collect.schedule.config"
const scheduleLastExitKey = "collect.schedule.last_exit_unix_sec"
const scheduleLastRunKey = "collect.schedule.last_run_unix_sec"

func GetScheduleConfig() CollectScheduleConfig {
	raw, err := db.GetSetting(scheduleConfigKey)
	if err != nil || raw == "" {
		return defaultScheduleConfig()
	}
	var cfg CollectScheduleConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return defaultScheduleConfig()
	}
	// 保证合理默认值
	if cfg.BackgroundIntervalSeconds <= 0 {
		// 兼容旧版配置：若有 minutes 字段则转换
		if cfg.BackgroundIntervalMinutes > 0 {
			cfg.BackgroundIntervalSeconds = cfg.BackgroundIntervalMinutes * 60
		} else {
			cfg.BackgroundIntervalSeconds = 60
		}
	}
	// 最小间隔 30 秒（避免用户误输入过小值）
	if cfg.BackgroundIntervalSeconds < 30 {
		cfg.BackgroundIntervalSeconds = 30
	}
	if cfg.SourceGapSeconds <= 0 {
		cfg.SourceGapSeconds = 10
	}
	if cfg.PageGapSeconds <= 0 {
		cfg.PageGapSeconds = 30
	}
	return cfg
}

func SetScheduleConfig(cfg CollectScheduleConfig) error {
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return db.SetSetting(scheduleConfigKey, string(b))
}

// 记录最后一次退出时间（shutdown 时写入）
func TouchLastExit() {
	_ = db.SetSetting(scheduleLastExitKey, strconv.FormatInt(time.Now().Unix(), 10))
}

// 读取最后一次退出时间（返回秒时间戳，0 表示没有记录）
func GetLastExitUnix() int64 {
	raw, _ := db.GetSetting(scheduleLastExitKey)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// 记录最近一次周期采集的执行时间
func TouchLastRun() {
	_ = db.SetSetting(scheduleLastRunKey, strconv.FormatInt(time.Now().Unix(), 10))
}

func GetLastRunUnix() int64 {
	raw, _ := db.GetSetting(scheduleLastRunKey)
	if raw == "" {
		return 0
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// 全局采集引擎映射：source_key -> engine
type engineEntry struct {
	engine *collect.Engine
	status *CollectStatus
	mode   string
	mu     sync.Mutex
}

var (
	engineMap   = make(map[string]*engineEntry)
	engineMapMu sync.Mutex
)

// GetCollectStatus 返回指定 source 的采集状态
func GetCollectStatus(sourceKey string) *CollectStatus {
	engineMapMu.Lock()
	defer engineMapMu.Unlock()
	if e, ok := engineMap[sourceKey]; ok {
		e.mu.Lock()
		defer e.mu.Unlock()
		s := *e.status // 拷贝
		return &s
	}
	return &CollectStatus{SourceKey: sourceKey}
}

// GetOrCreateEngine 获取或创建一个新的引擎 entry（用于启动新采集）
func GetOrCreateEngine(sourceKey string) *engineEntry {
	engineMapMu.Lock()
	defer engineMapMu.Unlock()
	if e, ok := engineMap[sourceKey]; ok {
		return e
	}
	entry := &engineEntry{
		status: &CollectStatus{SourceKey: sourceKey},
	}
	engineMap[sourceKey] = entry
	return entry
}

func (e *engineEntry) BindEngine(engine *collect.Engine, mode string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.engine = engine
	e.status.Running = true
	e.status.Paused = false
	e.mode = mode
	e.status.Mode = mode
}

func (e *engineEntry) GetMode() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.mode
}

func (e *engineEntry) UpdateProgress(current, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.Current = current
	e.status.Total = total
}

func (e *engineEntry) UpdatePageNames(page int, names []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.Page = page
	e.status.Names = names
}

func (e *engineEntry) MarkDone(logMsg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.Running = false
	e.status.Paused = false
	e.status.Log = logMsg
}

func (e *engineEntry) SetPaused(paused bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.Paused = paused
}

func (e *engineEntry) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.status.Running
}

// PauseCollect 暂停指定 source 的采集
func PauseCollect(sourceKey string) bool {
	engineMapMu.Lock()
	entry, ok := engineMap[sourceKey]
	engineMapMu.Unlock()
	if !ok || entry == nil {
		return false
	}
	entry.mu.Lock()
	eng := entry.engine
	entry.mu.Unlock()
	if eng == nil {
		return false
	}
	eng.Pause()
	entry.SetPaused(true)
	return true
}

// ResumeCollect 恢复指定 source 的采集
func ResumeCollect(sourceKey string) bool {
	engineMapMu.Lock()
	entry, ok := engineMap[sourceKey]
	engineMapMu.Unlock()
	if !ok || entry == nil {
		return false
	}
	entry.mu.Lock()
	eng := entry.engine
	entry.mu.Unlock()
	if eng == nil {
		return false
	}
	eng.Resume()
	entry.SetPaused(false)
	return true
}

// StopCollect 停止指定 source 的采集
func StopCollect(sourceKey string) bool {
	engineMapMu.Lock()
	entry, ok := engineMap[sourceKey]
	engineMapMu.Unlock()
	if !ok || entry == nil {
		return false
	}
	entry.mu.Lock()
	eng := entry.engine
	entry.mu.Unlock()
	if eng == nil {
		return false
	}
	eng.Stop()
	return true
}

// ============================================================
// SearchSource: 用 wd=keyword 去源站模糊搜索，把结果入库，并返回
// ============================================================
type SearchSourceResult struct {
	Total   int           `json:"total"`
	Videos  []*model.Video `json:"videos"`
	From    string        `json:"from"`
	Keyword string        `json:"keyword"`
}

func SearchSource(sourceKey string, keyword string, limit int) (*SearchSourceResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}

	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, fmt.Errorf("获取源失败: %w", err)
	}

	if limit <= 0 {
		advCfg := src.GetAdvConfig()
		limit = advCfg.CollectLimit
	}
	if limit <= 0 {
		limit = 50
	}

	advCfg := src.GetAdvConfig()
	opts := collect.FetchOptions{
		Limit:        limit,
		Keyword:      keyword,
		FieldMapping: advCfg.FieldMapping,
	}

	page, err := collect.FetchPageWithOpts(src.ApiUrl, 1, opts)
	if err != nil {
		return nil, fmt.Errorf("源站搜索失败: %w", err)
	}
	if page == nil || len(page.List) == 0 {
		return &SearchSourceResult{Total: 0, Videos: nil, From: sourceKey, Keyword: keyword}, nil
	}

	applog.Info("[SearchSource] 源站搜索完成 - sourceKey: %s, keyword: %s, total: %d, listSize: %d",
		sourceKey, keyword, page.Total.Int(), len(page.List))

	// 调用详情接口获取完整字段（使用协程池并发，并发数 3）
	pool := collect.NewPool(3)
	var mu sync.Mutex
	for i, v := range page.List {
		if v == nil || v.VodId.String() == "" {
			continue
		}
		idx := i
		vid := v
		pool.Submit(func() {
			detail, err := collect.FetchVideoDetail(src.ApiUrl, vid.VodId.String())
			if err != nil || detail == nil {
				applog.Info("[SearchSource] 获取详情失败 - vod_id: %s, error: %v", vid.VodId.String(), err)
				return
			}
			applog.Info("[SearchSource] 获取详情成功 - vod_id: %s, vod_actor: %s, vod_director: %s, vod_content: %s",
				vid.VodId.String(), detail.VodActor, detail.VodDirector, truncate(detail.VodContent, 100))
			mu.Lock()
			if detail.VodActor != "" { page.List[idx].VodActor = detail.VodActor }
			if detail.VodDirector != "" { page.List[idx].VodDirector = detail.VodDirector }
			if detail.VodContent != "" { page.List[idx].VodContent = detail.VodContent }
			if detail.VodPic != "" { page.List[idx].VodPic = detail.VodPic }
			if detail.VodLang != "" { page.List[idx].VodLang = detail.VodLang }
			if detail.VodArea != "" { page.List[idx].VodArea = detail.VodArea }
			if detail.VodYear != "" { page.List[idx].VodYear = detail.VodYear }
			if detail.VodPlayUrl != "" { page.List[idx].VodPlayUrl = detail.VodPlayUrl }
			mu.Unlock()
		})
	}
	pool.Wait()
	pool.Stop()

	// 2) 入库（合并源站数据与数据库已有数据：源站非空字段覆盖，源站空字段保留数据库值）
	if err := db.EnsureVideoTable(sourceKey); err != nil {
		return nil, fmt.Errorf("确保表失败: %w", err)
	}
	for _, v := range page.List {
		if v == nil || v.VodName == "" {
			continue
		}
		applog.Info("[SearchSource] 处理前 - vod_id: %s, vod_name: %s, vod_actor: %s, vod_director: %s, vod_content: %s",
			v.VodId.String(), v.VodName, v.VodActor, v.VodDirector, truncate(v.VodContent, 100))

		v.VodContent = collect.CleanHTML(v.VodContent)
		v.VodContent = collect.CompressTextField(v.VodContent)
		v.VodActor = collect.CleanHTML(v.VodActor)
		v.VodActor = collect.CompressTextField(v.VodActor)
		v.VodDirector = collect.CleanHTML(v.VodDirector)
		v.VodDirector = collect.CompressTextField(v.VodDirector)
		v.VodPlayUrl = collect.CompressTextField(v.VodPlayUrl)
		v.VodDownUrl = collect.CompressTextField(v.VodDownUrl)

		applog.Info("[SearchSource] 处理后 - vod_id: %s, vod_name: %s, vod_actor: %s, vod_director: %s, vod_content: %s",
			v.VodId.String(), v.VodName, v.VodActor, v.VodDirector, truncate(v.VodContent, 100))
	}
	if err := db.MergeVideoDetails(sourceKey, page.List); err != nil {
		return nil, fmt.Errorf("合并视频数据失败: %w", err)
	}

	// 将源数据中携带的豆瓣信息存入全局 douban_info 表
	db.SaveDoubanInfoFromBatch(page.List)

	// 3) 从数据库读（带齐全字段）
	videos, _, err := db.GetVideos(sourceKey, db.FilterParams{Keyword: keyword, PageSize: limit})
	if err != nil {
		// 回退：直接把 API 结果返回
		return &SearchSourceResult{Total: len(page.List), Videos: page.List, From: sourceKey, Keyword: keyword}, nil
	}

	for _, v := range videos {
		v.VodActor = util.DecompressIfNeeded(v.VodActor)
		v.VodDirector = util.DecompressIfNeeded(v.VodDirector)
		v.VodContent = util.DecompressIfNeeded(v.VodContent)
		v.VodPlayUrl = util.DecompressIfNeeded(v.VodPlayUrl)
	}

	return &SearchSourceResult{Total: len(videos), Videos: videos, From: sourceKey, Keyword: keyword}, nil
}

// ============================================================
// GetSourceParams: 返回采集接口参数规范（给前端规则指南用）
// ============================================================
type SourceParamDoc struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Desc    string `json:"desc"`
	Example string `json:"example"`
}

type SourceParamsDoc struct {
	BaseUrl    string            `json:"base_url"`
	PathParams []SourceParamDoc  `json:"path_params"`
	QueryAc    []SourceParamDoc  `json:"query_ac"`
	QueryCommon []SourceParamDoc `json:"query_common"`
	QueryAdvanced []SourceParamDoc `json:"query_advanced"`
}

func GetSourceParamsDoc(sourceKey string) (*SourceParamsDoc, error) {
	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, err
	}
	return &SourceParamsDoc{
		BaseUrl: src.ApiUrl,
		PathParams: []SourceParamDoc{
			{
				Name:    "from",
				Type:    "路径参数",
				Desc:    "指定播放源 / 解析分组，过滤返回对应格式的播放地址",
				Example: "/from/mtm3u8 代表只返回 mtm3u8 格式播放链接",
			},
		},
		QueryAc: []SourceParamDoc{
			{Name: "ac=list", Type: "query", Desc: "获取视频列表（分页列表数据）", Example: "?ac=list"},
			{Name: "ac=videolist", Type: "query", Desc: "获取全字段视频列表（数据量更大）", Example: "?ac=videolist"},
			{Name: "ac=detail", Type: "query", Desc: "获取视频详情（单条/多条完整信息，当前使用模式）", Example: "?ac=detail"},
		},
		QueryCommon: []SourceParamDoc{
			{Name: "pg", Type: "int", Desc: "页码，用于分页", Example: "pg=2 获取第 2 页数据"},
			{Name: "limit", Type: "int", Desc: "单页返回数据条数（多数站点上限 100）", Example: "limit=50"},
			{Name: "t / type_id", Type: "int", Desc: "分类 ID，按影视分类筛选", Example: "t=47"},
			{Name: "ids", Type: "string", Desc: "视频 ID，ac=detail 专用，多 ID 用英文逗号分隔", Example: "ids=136279,136278"},
			{Name: "wd", Type: "string", Desc: "搜索关键词，按片名模糊检索", Example: "wd=修仙"},
			{Name: "h", Type: "int", Desc: "小时数，筛选 N 小时内更新的资源", Example: "h=24"},
		},
		QueryAdvanced: []SourceParamDoc{
			{Name: "isend", Type: "0/1", Desc: "是否完结，1=全集完结，0=连载中", Example: "isend=1"},
			{Name: "year", Type: "string", Desc: "上映年份，支持单年份/年份区间", Example: "year=2026 或 year=2020-2026"},
			{Name: "sort_direct", Type: "string", Desc: "排序方向，默认按更新时间倒序", Example: "sort_direct=asc"},
			{Name: "vod_letter", Type: "string", Desc: "首字母筛选（拼音首字母）", Example: "vod_letter=W"},
		},
	}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
