package handler

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
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
	Mode      string   `json:"mode"` // 当前采集模式

	// 上一次运行的结果：进度字段只描述"正在跑的这一次"，运行结束后必须由
	// 这组字段说明这次到底完成了多少、哪些页没落地。
	Saved          int    `json:"saved"`
	FetchFailures  []int  `json:"fetch_failures"`
	SaveFailures   []int  `json:"save_failures"`
	EmptyPages     []int  `json:"empty_pages"`
	ErrorKind      string `json:"last_error_kind"`
	ElapsedMs      int64  `json:"elapsed_ms"`
	FinishedAtUnix int64  `json:"finished_at_unix"`
	Stopped        bool   `json:"stopped"`
}

// RunOutcome 是引擎一次运行结束后写回状态快照的报告。
type RunOutcome struct {
	Log           string
	Saved         int
	FetchFailures []int
	SaveFailures  []int
	EmptyPages    []int
	ErrorKind     string
	ElapsedMs     int64
	Stopped       bool
}

// OutcomeFromRun 把引擎的统计与错误压平成前端可直接消费的报告。
func OutcomeFromRun(stats *collect.RunStats, err error) RunOutcome {
	out := RunOutcome{
		ErrorKind: collect.ErrorKind(err),
	}
	if err != nil {
		out.Log = err.Error()
	}
	if stats != nil {
		out.Saved = stats.Saved
		out.FetchFailures = stats.FetchFailures
		out.SaveFailures = stats.SaveFailures
		out.EmptyPages = stats.EmptyPages
		out.Stopped = stats.Stopped
		out.ElapsedMs = int64(stats.ElapsedSeconds * 1000)
	}
	return out
}

// RecordCollectHealth 把一次采集运行记成一条源健康度样本，手动采集与定时采集共用。
//
// 用户手动停止直接跳过：按下停止不代表源有问题，记进去会把一个好源冤枉成坏的。
// 样本本身只在库写坏时才会失败，而那不该影响采集，所以只落日志。
func RecordCollectHealth(sourceKey string, outcome RunOutcome, err error) {
	if outcome.Stopped {
		return
	}
	ok, problem := classifyCollectRun(outcome, err)
	if dbErr := db.RecordSourceHealth(
		sourceKey, db.SourceHealthKindCollect, ok, outcome.ElapsedMs, outcome.Saved, problem, time.Now(),
	); dbErr != nil {
		applog.Warn("[Collect] 记录源健康度失败（不影响采集）: %v", dbErr)
	}
}

// classifyCollectRun 判定一次运行算不算健康，以及不健康时的说法。
//
// 有页没落地不能记成成功：那正是"源在慢慢不行"最早的信号，也是 A1 之前被吞掉的那类失败。
func classifyCollectRun(outcome RunOutcome, err error) (ok bool, problem string) {
	switch {
	case err != nil:
		return false, err.Error()
	case len(outcome.FetchFailures) > 0:
		return false, fmt.Sprintf("%d 页取页失败", len(outcome.FetchFailures))
	case len(outcome.SaveFailures) > 0:
		return false, fmt.Sprintf("%d 页入库失败", len(outcome.SaveFailures))
	}
	return true, ""
}

// CollectDonePayload 是 collect:done 事件的载荷。带上失败页码与耗时，
// 前端不必再轮询 GetCollectStatus 就能区分完成与部分失败。
func CollectDonePayload(sourceKey, mode string, outcome RunOutcome) map[string]any {
	return map[string]any{
		"source_key":     sourceKey,
		"mode":           mode,
		"error":          outcome.Log,
		"error_kind":     outcome.ErrorKind,
		"saved":          outcome.Saved,
		"fetch_failures": outcome.FetchFailures,
		"save_failures":  outcome.SaveFailures,
		"empty_pages":    outcome.EmptyPages,
		"elapsed_ms":     outcome.ElapsedMs,
		"stopped":        outcome.Stopped,
	}
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

// 记录最近一次采集的执行时间，周期调度和手动触发都写这里，
// 这样设置页重启后仍能看到"上次采集"，而不是随内存一起归零。
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
		s := e.snapshotStatus() // 拷贝
		return &s
	}
	return &CollectStatus{SourceKey: sourceKey}
}

// snapshotStatus 返回状态副本，调用方不会读到半更新的字段。
func (e *engineEntry) snapshotStatus() CollectStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	return *e.status
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
	e.status.Mode = mode
	e.mode = mode
	e.status.clearLastRunResult()
}

// TryBindEngine makes the idle-to-running transition atomic. Callers must not
// split IsRunning and BindEngine across goroutines, or a manual run and a
// scheduler tick can start the same source twice.
func (e *engineEntry) TryBindEngine(engine *collect.Engine, mode string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.status.Running {
		return false
	}
	e.engine = engine
	e.status.Running = true
	e.status.Paused = false
	e.status.Mode = mode
	e.mode = mode
	e.status.clearLastRunResult()
	return true
}

// clearLastRunResult 在新一轮开始时抹掉上一轮的结果，否则旧的失败页码会被
// 当成这一轮的状态显示。
func (s *CollectStatus) clearLastRunResult() {
	s.Saved = 0
	s.FetchFailures = nil
	s.SaveFailures = nil
	s.EmptyPages = nil
	s.ErrorKind = ""
	s.ElapsedMs = 0
	s.FinishedAtUnix = 0
	s.Stopped = false
}

// applyOutcome 把引擎报告写入状态快照，前端据此区分"全部落地"与"部分失败"。
func (s *CollectStatus) applyOutcome(outcome RunOutcome) {
	s.Log = outcome.Log
	s.Saved = outcome.Saved
	s.FetchFailures = outcome.FetchFailures
	s.SaveFailures = outcome.SaveFailures
	s.EmptyPages = outcome.EmptyPages
	s.ErrorKind = outcome.ErrorKind
	s.ElapsedMs = outcome.ElapsedMs
	s.Stopped = outcome.Stopped
	s.FinishedAtUnix = time.Now().Unix()
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

func (e *engineEntry) MarkDone(outcome RunOutcome) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.status.Running = false
	e.status.Paused = false
	e.status.applyOutcome(outcome)
}

// FinishEngine ignores a stale callback from a previous run.
func (e *engineEntry) FinishEngine(engine *collect.Engine, outcome RunOutcome) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.engine != engine {
		return
	}
	e.status.Running = false
	e.status.Paused = false
	e.status.applyOutcome(outcome)
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
// SearchSource: 用 wd=keyword 去源站搜索指定页，返回富字段结果（不入库）
// 入库请调用 ImportSourceVideos
// ============================================================
type SearchSourceResult struct {
	Total     int            `json:"total"`
	Page      int            `json:"page"`
	PageCount int            `json:"page_count"`
	PageSize  int            `json:"page_size"`
	Videos    []*model.Video `json:"videos"`
	From      string         `json:"from"`
	Keyword   string         `json:"keyword"`
}

// SearchSource is the 源搜索 path: it streams remote hits straight to the UI
// and never writes the catalog. Nothing here resurrects soft-deleted rows,
// because the user is only previewing a source. Persisting these hits is an
// explicit action — see ImportSourceVideos, which always resurrects.
//
// This is the intentional counterpart to SearchVideos (app/handler/video.go),
// which caches each hit as it searches.
func SearchSource(sourceKey string, keyword string, page int, pageSize int) (*SearchSourceResult, error) {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}

	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, fmt.Errorf("获取源失败: %w", err)
	}

	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		advCfg := src.GetAdvConfig()
		pageSize = advCfg.CollectLimit
	}
	if pageSize <= 0 {
		pageSize = 50
	}

	// Emit: 开始搜索
	application.Get().Event.Emit("search:progress", map[string]interface{}{
		"stage":   "fetching_list",
		"message": "正在从源站获取搜索结果...",
		"current": 0,
		"total":   0,
	})

	strategy := collect.CreateStrategyFromSource(src)
	if strategy == nil {
		return nil, fmt.Errorf("源站策略不可用")
	}
	p, err := collect.FetchSearchPage(strategy, keyword, page)
	if err != nil {
		return nil, fmt.Errorf("源站搜索失败: %w", err)
	}
	if p == nil || len(p.List) == 0 {
		return &SearchSourceResult{
			Total: 0, Page: page, PageCount: 0, PageSize: pageSize,
			Videos: nil, From: sourceKey, Keyword: keyword,
		}, nil
	}

	applog.Info("[SearchSource] 源站搜索完成 - sourceKey: %s, keyword: %s, page: %d, total: %d, listSize: %d",
		sourceKey, keyword, page, p.Total.Int(), len(p.List))

	p.List = db.FilterEnabledCollectVideos(p.List)
	// Search results are transient. Only ImportSourceVideos may persist the
	// user-selected items; otherwise every search hit would appear on Home.
	// Membership is probed against the catalog so the UI can mark hits the user
	// has already imported, including imports from earlier sessions.
	ids := make([]string, 0, len(p.List))
	for _, v := range p.List {
		if v != nil {
			ids = append(ids, v.VodId.String())
		}
	}
	cached, err := db.ExistingCatalogVodIDs(sourceKey, ids)
	if err != nil {
		applog.Warn("[SearchSource] 查询入库状态失败: %v", err)
	}
	for _, v := range p.List {
		if v == nil {
			continue
		}
		v.InCatalog = cached[strings.TrimSpace(v.VodId.String())]
		v.VodContent, v.VodActor, v.VodDirector = "", "", ""
		v.VodPlayUrl, v.VodDownUrl, v.VodPlayFrom = "", "", ""
	}
	application.Get().Event.Emit("search:progress", map[string]interface{}{
		"stage":   "done",
		"message": "搜索完成",
		"current": len(p.List),
		"total":   len(p.List),
	})

	// 返回结果（不入库），分页元数据来自源站
	pageCount := p.Pagecount.Int()
	if pageCount <= 0 && p.Total.Int() > 0 {
		pageCount = (p.Total.Int() + pageSize - 1) / pageSize
	}
	total := p.Total.Int()
	if total < len(p.List) {
		total = len(p.List)
	}

	return &SearchSourceResult{
		Total:     total,
		Page:      page,
		PageCount: pageCount,
		PageSize:  pageSize,
		Videos:    p.List,
		From:      sourceKey,
		Keyword:   keyword,
	}, nil
}

// ImportSourceVideos stores only catalog fields from user-selected search hits.
// It is the 入库 button, i.e. the explicit counterpart to the automatic
// collection/search caching paths, so it bypasses catalog_revive_deleted.
// 返回成功入库的条数
func ImportSourceVideos(sourceKey string, videos []*model.Video) (int, error) {
	if sourceKey == "" {
		return 0, fmt.Errorf("source_key 不能为空")
	}
	if len(videos) == 0 {
		return 0, nil
	}
	videos = db.FilterEnabledCollectVideos(videos)
	if len(videos) == 0 {
		return 0, nil
	}

	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return 0, fmt.Errorf("获取源失败: %w", err)
	}
	_ = src

	toSave := make([]*model.Video, 0, len(videos))
	for _, v := range videos {
		if v == nil || v.VodName == "" {
			continue
		}
		v.VodContent, v.VodActor, v.VodDirector = "", "", ""
		v.VodPlayUrl, v.VodDownUrl, v.VodPlayFrom = "", "", ""
		toSave = append(toSave, v)
	}
	if len(toSave) == 0 {
		return 0, nil
	}
	// Explicit "入库": always restore soft-deleted entries, ignoring the
	// catalog_revive_deleted setting.
	if err := db.UpsertCatalogItemsWithRevival(sourceKey, toSave); err != nil {
		return 0, fmt.Errorf("写入视频目录失败: %w", err)
	}

	applog.Info("[ImportSourceVideos] 入库完成 - sourceKey: %s, count: %d", sourceKey, len(toSave))
	return len(toSave), nil
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
	BaseUrl       string           `json:"base_url"`
	PathParams    []SourceParamDoc `json:"path_params"`
	QueryAc       []SourceParamDoc `json:"query_ac"`
	QueryCommon   []SourceParamDoc `json:"query_common"`
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
