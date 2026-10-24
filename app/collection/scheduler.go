package collection

import (
	"cczjVideo/app/applog"

	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	sourceservice "cczjVideo/app/source"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Scheduler 后台采集调度器
// - 每个源可以独立配置后台周期采集（模式、间隔）
// - 启动时可选择性执行全量/补采
// - 后台周期采集根据每个源的模式决定是全量还是增量
// - 增量模式使用 h 参数只拉最新资源
type Scheduler struct {
	runs    *Service
	mu      sync.Mutex
	ctx     context.Context
	running bool
	stopCh  chan struct{}
	stopped chan struct{}
	// generation counts Start calls. Stop() waits only briefly for the running
	// loop, so a superseded generation must be able to notice that a new one has
	// taken over instead of reading the replacement channel as if it were its own.
	generation uint64

	// 全局默认配置
	sourceGap time.Duration
	pageGap   time.Duration

	// 每个源的定时器管理器
	sourceTimers   map[string]*time.Timer
	sourceTimersMu sync.Mutex
}

func NewScheduler(runs *Service) *Scheduler {
	cfg := GetScheduleConfig()
	return &Scheduler{runs: runs, ctx: context.Background(), sourceGap: time.Duration(cfg.SourceGapSeconds) * time.Second, pageGap: time.Duration(cfg.PageGapSeconds) * time.Second, sourceTimers: map[string]*time.Timer{}}
}
func (s *Scheduler) launch(work func()) bool {
	if s.runs == nil || s.runs.background == nil {
		return false
	}
	return s.runs.background("collection.scheduler", func(context.Context) { work() })
}

// SetContext 采用应用生命周期上下文。只在调度器还没跑起来时替换：已经在跑的循环
// 按旧上下文判断取消，中途换掉会让那一轮的"取消即停"失去依据。
func (s *Scheduler) SetContext(ctx context.Context) {
	if ctx == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return
	}
	s.ctx = ctx
}

// appContext 取当前应用上下文。读写都在 s.mu 下：SetContext 与采集协程可以并发。
func (s *Scheduler) appContext() context.Context {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx
}

// ReloadConfig 重新读取配置
func (s *Scheduler) ReloadConfig() {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := GetScheduleConfig()
	s.sourceGap = time.Duration(cfg.SourceGapSeconds) * time.Second
	s.pageGap = time.Duration(cfg.PageGapSeconds) * time.Second
}

// Start 启动调度器
//   - 启动阶段（全量 / 按水位线补采）只看自己的开关，不被后台总开关连带短路
//   - 后台总开关打开时，再为每个启用后台采集的源启动独立定时器
func (s *Scheduler) Start() {
	cfg := GetScheduleConfig()
	if !cfg.EnableBackground && !cfg.EnableInitialFullCollect && !cfg.EnableStartupCatchup {
		s.stopAllSourceTimers()
		return
	}
	// 冷却到期的自动停用源先放回采集池，再决定这轮采谁、给谁配定时器。
	if n := sourceservice.RecoverAutoDisabledSources(); n > 0 {
		s.logScheduler(fmt.Sprintf("%d 个自动停用的采集源冷却到期，已重新放回采集池", n))
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.generation++
	gen := s.generation
	stopCh := make(chan struct{})
	stopped := make(chan struct{})
	s.stopCh = stopCh
	s.stopped = stopped
	s.mu.Unlock()

	if !s.launch(func() {
		finished := func() {
			s.mu.Lock()
			if s.generation == gen {
				s.running = false
			}
			s.mu.Unlock()
		}
		defer func() {
			finished()
			close(stopped)
		}()

		// === 启动阶段 ===
		if cfg.EnableInitialFullCollect {
			s.logScheduler("启动阶段全量采集开始")
			s.runAllSourcesOnce(gen, model.CollectModeFull, 0)
		} else if cfg.EnableStartupCatchup {
			// 补采窗口由每个源自己的水位线算出：停机多久就补多久，
			// 不再依赖一个全局退出时刻把所有源压成同一个小时数。
			s.logScheduler("启动阶段按各源水位线补采开始")
			s.runAllSourcesOnce(gen, model.CollectModeIncremental, 0)
		}

		if !cfg.EnableBackground {
			// 补采跑完就收工：后台总开关关着，不该留下任何定时器。
			s.stopSourceTimersForGeneration(gen)
			return
		}

		// A generation that was superseded while Stop()'s wait timed out must not
		// tear down the timers its replacement just registered.
		if !s.isCurrentGeneration(gen) {
			s.logScheduler("旧一代后台调度已让位，退出")
			return
		}

		// === 后台周期循环 ===
		// 为每个源启动独立定时器
		s.startSourceTimers()

		// 主循环：等待停止信号
		<-stopCh
		s.stopSourceTimersForGeneration(gen)
		finished()
	}) {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
		close(stopped)
	}
}

// runAllSourcesOnce 采集所有源一次（启动阶段用）
func (s *Scheduler) runAllSourcesOnce(gen uint64, mode model.CollectMode, hours int) {
	sources, err := db.GetEnabledSources()
	if err != nil {
		s.logScheduler("读取采集源列表失败: " + err.Error())
		return
	}
	if len(sources) == 0 {
		s.logScheduler("没有可用的采集源，跳过")
		return
	}

	for i, src := range sources {
		select {
		case <-s.appContext().Done():
			return
		default:
		}
		if s.stoppedFor(gen) {
			s.logScheduler("后台采集被停止")
			return
		}

		if i > 0 {
			s.mu.Lock()
			gap := s.sourceGap
			s.mu.Unlock()
			if !s.sleepInterruptible(gen, gap) {
				return
			}
		}

		s.runSourceCollect(src.SourceKey, mode, hours)
	}
	s.logScheduler("启动阶段采集结束")
}

// startSourceTimers 为每个启用后台采集的源启动独立定时器
func (s *Scheduler) startSourceTimers() {
	if !GetScheduleConfig().EnableBackground {
		return
	}
	sources, err := db.GetSourcesByKind(model.SourceKindVideo)
	if err != nil {
		// 这里必须出声：读不到源列表等于一个定时采集都没安排上，而界面只会显示
		// "调度器在跑"。静默 return 的话，用户以为后台在补数据，其实一天都没采。
		s.logScheduler("安排定时采集时读取采集源列表失败: " + err.Error())
		return
	}

	for _, src := range sources {
		if src.Enabled != 1 {
			continue
		}
		sc := src.GetScheduleConfig()
		if sc == nil || !sc.Enabled {
			continue
		}
		s.scheduleSource(src.SourceKey, sc)
	}
}

// scheduleSource 为一个源安排定时采集
func (s *Scheduler) scheduleSource(sourceKey string, sc *model.ScheduleConfig) {
	s.scheduleSourceReplacing(sourceKey, sc, nil)
}
func (s *Scheduler) scheduleSourceReplacing(sourceKey string, sc *model.ScheduleConfig, previous *time.Timer) {
	if !GetScheduleConfig().EnableBackground {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	// A timer callback can race Stop. Keep this check and timer registration
	// serialized with Stop so a stopped scheduler cannot recreate a timer.
	if !s.running {
		return
	}

	if sc.IntervalMin < 1 {
		sc.IntervalMin = 1 // 最小 1 分钟
	}
	interval := time.Duration(sc.IntervalMin) * time.Minute

	s.sourceTimersMu.Lock()
	defer s.sourceTimersMu.Unlock()
	if previous != nil && s.sourceTimers[sourceKey] != previous {
		return
	}
	// 取消旧定时器
	if old, ok := s.sourceTimers[sourceKey]; ok {
		old.Stop()
	}

	mode := sc.Mode
	if mode == "" {
		mode = model.CollectModeIncremental // 默认增量（只拉最新）
	}

	s.logScheduler(fmt.Sprintf("[%s] 后台采集已就绪: 每 %d 分钟, 模式=%s", sourceKey, sc.IntervalMin, mode))

	var timer *time.Timer
	timer = time.AfterFunc(interval, func() {
		s.launch(func() {
			s.runSourceCollectAdmitted(sourceKey, mode, 0, func() bool { return s.ownsSourceTimer(sourceKey, timer) })
			s.refreshSourceTimer(sourceKey, timer)
		})
	})
	s.sourceTimers[sourceKey] = timer
}

func (s *Scheduler) ownsSourceTimer(key string, timer *time.Timer) bool {
	s.sourceTimersMu.Lock()
	defer s.sourceTimersMu.Unlock()
	return s.sourceTimers[key] == timer
}
func (s *Scheduler) removeSourceTimer(key string, previous *time.Timer) {
	s.sourceTimersMu.Lock()
	defer s.sourceTimersMu.Unlock()
	if timer := s.sourceTimers[key]; timer != nil && (previous == nil || timer == previous) {
		timer.Stop()
		delete(s.sourceTimers, key)
	}
}

// Rearming reads committed configuration, and only the current timer may replace itself.
func (s *Scheduler) refreshSourceTimer(key string, previous *time.Timer) {
	if previous != nil && !s.ownsSourceTimer(key, previous) {
		return
	}
	row, err := db.GetSourceByKeyContext(s.appContext(), key)
	if err != nil || row.GetKind() != model.SourceKindVideo || row.Enabled != 1 {
		s.removeSourceTimer(key, previous)
		if err != nil && !errors.Is(err, sql.ErrNoRows) && s.appContext().Err() == nil {
			s.logScheduler(fmt.Sprintf("[%s] 读取调度配置失败: %v", key, err))
		}
		return
	}
	config := row.GetScheduleConfig()
	if config == nil || !config.Enabled {
		s.removeSourceTimer(key, previous)
		return
	}
	s.scheduleSourceReplacing(key, config, previous)
}

// runSourceCollect 执行单个源的采集
func (s *Scheduler) runSourceCollect(sourceKey string, mode model.CollectMode, hours int) {
	s.runSourceCollectAdmitted(sourceKey, mode, hours, nil)
}
func (s *Scheduler) runSourceCollectAdmitted(sourceKey string, mode model.CollectMode, hours int, admit func() bool) {
	var entry *engineEntry

	s.mu.Lock()
	pageGap := s.pageGap
	s.mu.Unlock()
	appCtx := s.appContext()
	appCtx, operationID := runContext(appCtx, appCtx, sourceKey)

	var engineOpts []collect.EngineOption
	engineOpts = append(engineOpts, collect.WithCollectMode(mode))
	if mode == model.CollectModeIncremental && hours > 0 {
		engineOpts = append(engineOpts, collect.WithTimeHours(hours))
	}

	engine := collect.NewEngineV2(
		sourceKey,
		func(msg string) {
			s.runs.emit("collect:log", map[string]interface{}{
				"source_key":   sourceKey,
				"operation_id": operationID,
				"message":      msg,
			})
		},
		func(current, total int) {
			entry.UpdateProgress(current, total)
			s.runs.emit("collect:progress", map[string]interface{}{
				"source_key":   sourceKey,
				"operation_id": operationID,
				"current":      current,
				"total":        total,
			})
		},
		func(page int, names []string) {
			entry.UpdatePageNames(page, names)
			s.runs.emit("collect:page", map[string]interface{}{
				"source_key":   sourceKey,
				"operation_id": operationID,
				"page":         page,
				"names":        names,
			})
		},
		engineOpts...,
	)
	engine.SetContext(appCtx)
	engine.SetPageGap(pageGap)
	var accepted bool
	entry, accepted = s.runs.registry.bindIf(sourceKey, engine, string(mode), admit)
	if !accepted {
		engine.Stop()
		s.logScheduler(fmt.Sprintf("[%s] 正在采集中，跳过", sourceKey))
		return
	}
	// 持久化"最近一次采集"，设置页重启后仍能看到；只存活在内存里会一重启就归零。
	TouchLastRun()

	s.logScheduler(fmt.Sprintf("[%s] 开始采集 (模式=%s)", sourceKey, string(mode)))

	done := make(chan struct{})
	if !s.launch(func() {
		defer close(done)
		s.runs.execute(engine, entry, sourceKey, string(mode), operationID)
	}) {
		engine.Stop()
		entry.FinishEngine(engine, RunOutcome{Stopped: true})
		close(done)
	}

	select {
	case <-done:
		return
	case <-appCtx.Done():
		engine.Stop()
		<-done
		return
	case <-s.stopChannel():
		// 优雅停止：通知引擎完成当前页后停止，并等待引擎结束
		engine.Stop()
		<-done
		return
	}
}

// Stop 停止调度器的后台循环，并等待所有正在运行的采集引擎优雅结束
func (s *Scheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.stopAllSourceTimers()

	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}

	stoppedCh := s.stopped
	s.mu.Unlock()

	select {
	case <-stoppedCh:
	case <-time.After(5 * time.Second):
	}
}

// stopAllSourceTimers 停止所有源的定时器
func (s *Scheduler) stopAllSourceTimers() {
	s.sourceTimersMu.Lock()
	defer s.sourceTimersMu.Unlock()
	for k, t := range s.sourceTimers {
		t.Stop()
		delete(s.sourceTimers, k)
	}
}

func (s *Scheduler) stopSourceTimersForGeneration(gen uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.generation == gen {
		s.stopAllSourceTimers()
	}
}

// TriggerNow 立即触发一次全量采集（不影响定时）
func (s *Scheduler) TriggerNow() {
	gen := s.currentGeneration()
	s.launch(func() { s.runAllSourcesOnce(gen, model.CollectModeFull, 0) })
}

// TriggerOne 立即触发单个源的采集
func (s *Scheduler) TriggerOne(sourceKey string, mode model.CollectMode, hours int) {
	if sourceKey == "" {
		s.TriggerNow()
		return
	}
	s.launch(func() { s.runSourceCollect(sourceKey, mode, hours) })
}

// UpdateSourceSchedule 更新某个源的后台采集配置
func (s *Scheduler) UpdateSourceSchedule(sourceKey string) {
	s.removeSourceTimer(sourceKey, nil)
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()
	if !running || !GetScheduleConfig().EnableBackground {
		return
	}
	s.refreshSourceTimer(sourceKey, nil)
}

// Status 调度器状态（供前端展示）
type SchedulerStatus struct {
	Running                bool                 `json:"running"`
	Background             bool                 `json:"background"`
	BackgroundEveryMinutes int                  `json:"background_every_minutes"`
	BackgroundEverySeconds int                  `json:"background_every_seconds"`
	SourceGapSeconds       int                  `json:"source_gap_seconds"`
	PageGapSeconds         int                  `json:"page_gap_seconds"`
	StartupCatchup         bool                 `json:"startup_catchup"`
	InitialFullCollect     bool                 `json:"initial_full_collect"`
	LastExitUnix           int64                `json:"last_exit_unix"`
	LastRunUnix            int64                `json:"last_run_unix"`
	NowUnix                int64                `json:"now_unix"`
	Note                   string               `json:"note"`
	SourceSchedules        []SourceScheduleItem `json:"source_schedules"`
}

// SourceScheduleItem 单个源的调度信息
type SourceScheduleItem struct {
	SourceKey   string `json:"source_key"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`         // full | incremental
	IntervalMin int    `json:"interval_min"` // 定时间隔（分钟）
	Running     bool   `json:"running"`      // 是否正在采集
	// 增量水位线：已覆盖到 / 最近一次尝试采集的时刻（unix 秒，0 表示没有记录）
	CoveredUntilUnix int64 `json:"covered_until_unix"`
	LastAttemptUnix  int64 `json:"last_attempt_unix"`
}

// Status 返回当前调度器状态
// IsRunning 返回调度器是否正在运行
func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) Status() SchedulerStatus {
	s.mu.Lock()
	cfg := GetScheduleConfig()
	running := s.running
	bgEnabled := cfg.EnableBackground
	everySec := cfg.BackgroundIntervalSeconds
	if everySec <= 0 {
		everySec = 60
	}
	everyMin := everySec / 60
	sourceGap := cfg.SourceGapSeconds
	pageGap := cfg.PageGapSeconds
	s.mu.Unlock()

	// 收集每个源的调度信息（包含所有源，不按 enabled 过滤，前端需要看到全部源的调度配置）
	var srcItems []SourceScheduleItem
	sources, _ := db.GetSourcesByKind(model.SourceKindVideo)
	for _, src := range sources {
		sc := src.GetScheduleConfig()
		entry := s.runs.registry.GetCollectStatus(src.SourceKey)
		cursor, _ := db.GetCollectCursor(src.SourceKey)
		item := SourceScheduleItem{
			SourceKey:        src.SourceKey,
			Name:             src.Name,
			Running:          entry.Running,
			CoveredUntilUnix: cursor.CoveredUntilUnix,
			LastAttemptUnix:  cursor.LastAttemptUnix,
		}
		if sc != nil && sc.Enabled {
			item.Enabled = true
			item.Mode = string(sc.Mode)
			if item.Mode == "" {
				item.Mode = "incremental"
			}
			item.IntervalMin = sc.IntervalMin
			if item.IntervalMin < 1 {
				item.IntervalMin = 1
			}
		}
		srcItems = append(srcItems, item)
	}

	note := fmt.Sprintf("独立定时器模式: 每个源按各自配置间隔采集")
	if !bgEnabled {
		// ⭐ 统计实际启用定时采集的源数量
		enabledCount := 0
		for _, item := range srcItems {
			if item.Enabled {
				enabledCount++
			}
		}
		if enabledCount > 0 {
			note = fmt.Sprintf("独立定时器模式: %d 个源已配置定时采集（全局开关未启用）", enabledCount)
		} else {
			note = "后台周期采集已禁用"
		}
	}

	return SchedulerStatus{
		Running:                running,
		Background:             bgEnabled,
		BackgroundEveryMinutes: everyMin,
		BackgroundEverySeconds: everySec,
		SourceGapSeconds:       sourceGap,
		PageGapSeconds:         pageGap,
		StartupCatchup:         cfg.EnableStartupCatchup,
		InitialFullCollect:     cfg.EnableInitialFullCollect,
		LastExitUnix:           GetLastExitUnix(),
		LastRunUnix:            GetLastRunUnix(),
		NowUnix:                time.Now().Unix(),
		Note:                   note,
		SourceSchedules:        srcItems,
	}
}

// sleepInterruptible 睡眠期间可被停止
func (s *Scheduler) sleepInterruptible(gen uint64, d time.Duration) bool {
	appCtx := s.appContext()
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		chunk := 500 * time.Millisecond
		if remaining < chunk {
			chunk = remaining
		}
		select {
		case <-time.After(chunk):
		case <-appCtx.Done():
			return false
		}
		if s.stoppedFor(gen) {
			return false
		}
	}
}

// isCurrentGeneration reports whether gen still owns the background loop.
func (s *Scheduler) isCurrentGeneration(gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return gen == s.generation
}

// currentGeneration snapshots the generation counter so a manual trigger stops
// alongside whichever background loop is live right now.
func (s *Scheduler) currentGeneration() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.generation
}

// stopChannel snapshots the current stop signal so callers never race with the
// channel Start() installs. Returns nil when the scheduler was never started,
// which blocks forever in a select and is therefore ignored.
func (s *Scheduler) stopChannel() chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopCh
}

// stoppedFor reports whether generation gen was asked to stop, either by Stop()
// closing its own channel or by a newer Start() taking over. Blocking on
// s.stopCh directly cannot work: after a timed-out Stop the field already holds
// the replacement channel, so the old generation would wait on the wrong signal
// and keep collecting behind the new one.
func (s *Scheduler) stoppedFor(gen uint64) bool {
	s.mu.Lock()
	stale := gen != s.generation
	stopCh := s.stopCh
	s.mu.Unlock()
	if stale {
		return true
	}
	select {
	case <-stopCh:
		return true
	default:
		return false
	}
}

func (s *Scheduler) logScheduler(msg string) {
	// 调度日志同时进入 applog，设置页的统一时间线只消费这一条流；
	// extra=1 跳过本包装函数，caller 指回真正触发调度的那一行
	applog.InfoAt(1, "%s", "[scheduler] "+msg)
	s.runs.emit("collect:log", map[string]interface{}{
		"source_key": "__scheduler__",
		"message":    msg,
	})
}
