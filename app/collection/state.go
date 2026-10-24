package collection

import (
	sourceservice "cczjVideo/app/source"
	"context"

	"cczjVideo/app/collect"
	"cczjVideo/app/db"

	"encoding/json"
	"fmt"
	"strconv"

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
func RecordCollectHealth(sourceKey string, outcome RunOutcome, err error) {
	if outcome.Stopped {
		return
	}
	ok, problem := classifyCollectRun(outcome, err)
	sourceservice.RecordSourceHealthSample(sourceKey, db.SourceHealthKindCollect, ok, outcome.ElapsedMs, outcome.Saved, problem)
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
	engine     *collect.Engine
	status     *CollectStatus
	mu         sync.Mutex
	done       chan struct{}
	changing   int
	forget     bool
	changeGate chan struct{}
}

// Registry is shared by the manual controller and its scheduler, owned by one
// application. Source mutations reject new admission until active work drains.
type Registry struct {
	engineMap   map[string]*engineEntry
	engineMapMu sync.Mutex
}

func newRegistry() *Registry { return &Registry{engineMap: map[string]*engineEntry{}} }

// GetCollectStatus 返回指定 source 的采集状态
func (r *Registry) GetCollectStatus(sourceKey string) *CollectStatus {
	r.engineMapMu.Lock()
	defer r.engineMapMu.Unlock()
	if e, ok := r.engineMap[sourceKey]; ok {
		s := e.snapshotStatus() // 拷贝
		return &s
	}
	return &CollectStatus{SourceKey: sourceKey}
}

// snapshotStatus 返回状态副本，调用方不会读到半更新的字段。
func (e *engineEntry) snapshotStatus() CollectStatus {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := *e.status
	out.Names = append([]string(nil), e.status.Names...)
	out.FetchFailures = append([]int(nil), e.status.FetchFailures...)
	out.SaveFailures = append([]int(nil), e.status.SaveFailures...)
	out.EmptyPages = append([]int(nil), e.status.EmptyPages...)
	return out
}

// GetOrCreateEngine 获取或创建一个新的引擎 entry（用于启动新采集）
func (r *Registry) GetOrCreateEngine(sourceKey string) *engineEntry {
	r.engineMapMu.Lock()
	defer r.engineMapMu.Unlock()
	if e, ok := r.engineMap[sourceKey]; ok {
		return e
	}
	entry := &engineEntry{
		status: &CollectStatus{SourceKey: sourceKey},
	}
	r.engineMap[sourceKey] = entry
	return entry
}

// TryBindEngine makes the idle-to-running transition atomic. Callers must not
// split the running check and the bind across goroutines, or a manual run and a
// scheduler tick can start the same source twice.
func (e *engineEntry) TryBindEngine(engine *collect.Engine, mode string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.status.Running || e.changing > 0 {
		return false
	}
	e.done = make(chan struct{})
	e.engine = engine
	e.status.Running = true
	e.status.Paused = false
	e.status.Mode = mode
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

// FinishEngine ignores a stale callback from a previous run.
func (e *engineEntry) FinishEngine(engine *collect.Engine, outcome RunOutcome) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.engine != engine || !e.status.Running {
		return
	}
	e.status.Running = false
	e.status.Paused = false
	e.status.applyOutcome(outcome)
	if e.done != nil {
		close(e.done)
	}
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
func (r *Registry) PauseCollect(sourceKey string) bool {
	r.engineMapMu.Lock()
	entry, ok := r.engineMap[sourceKey]
	r.engineMapMu.Unlock()
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
func (r *Registry) ResumeCollect(sourceKey string) bool {
	r.engineMapMu.Lock()
	entry, ok := r.engineMap[sourceKey]
	r.engineMapMu.Unlock()
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
func (r *Registry) StopCollect(sourceKey string) bool {
	r.engineMapMu.Lock()
	entry, ok := r.engineMap[sourceKey]
	r.engineMapMu.Unlock()
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

func (r *Registry) isRunning() bool {
	r.engineMapMu.Lock()
	defer r.engineMapMu.Unlock()
	for _, e := range r.engineMap {
		if e.IsRunning() {
			return true
		}
	}
	return false
}
func (r *Registry) quiesce(ctx context.Context, key string) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.engineMapMu.Lock()
	e := r.engineMap[key]
	if e == nil {
		e = &engineEntry{status: &CollectStatus{SourceKey: key}}
		r.engineMap[key] = e
	}
	e.mu.Lock()
	e.changing++
	if e.changeGate == nil {
		e.changeGate = make(chan struct{}, 1)
	}
	engine, done, running := e.engine, e.done, e.status.Running
	r.engineMapMu.Unlock()
	e.mu.Unlock()
	held := false
	release := func() {
		if held {
			<-e.changeGate
		}
		r.engineMapMu.Lock()
		defer r.engineMapMu.Unlock()
		e.mu.Lock()
		defer e.mu.Unlock()
		e.changing--
		if e.changing == 0 && !e.status.Running && (e.forget || e.engine == nil) {
			if r.engineMap[key] == e {
				delete(r.engineMap, key)
			}
		}
	}
	select {
	case e.changeGate <- struct{}{}:
		held = true
	case <-ctx.Done():
		release()
		return nil, ctx.Err()
	}
	if running && engine != nil {
		engine.Stop()
		select {
		case <-done:
		case <-ctx.Done():
			release()
			return nil, ctx.Err()
		}
	}
	return release, nil
}

func (r *Registry) bind(key string, engine *collect.Engine, mode string) (*engineEntry, bool) {
	return r.bindIf(key, engine, mode, nil)
}
func (r *Registry) bindIf(key string, engine *collect.Engine, mode string, admit func() bool) (*engineEntry, bool) {
	r.engineMapMu.Lock()
	defer r.engineMapMu.Unlock()
	if admit != nil && !admit() {
		return nil, false
	}
	e := r.engineMap[key]
	if e == nil {
		e = &engineEntry{status: &CollectStatus{SourceKey: key}}
		r.engineMap[key] = e
	}
	return e, e.TryBindEngine(engine, mode)
}
func (r *Registry) forget(key string) {
	r.engineMapMu.Lock()
	defer r.engineMapMu.Unlock()
	if e := r.engineMap[key]; e != nil {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.forget = true
		if !e.status.Running && e.changing == 0 {
			delete(r.engineMap, key)
		}
	}
}
