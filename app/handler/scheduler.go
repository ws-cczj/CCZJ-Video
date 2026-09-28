package handler

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Scheduler 后台采集调度器
// - 每个源可以独立配置后台周期采集（模式、间隔）
// - 启动时可选择性执行全量/补采
// - 后台周期采集根据每个源的模式决定是全量还是增量
// - 增量模式使用 h 参数只拉最新资源
type Scheduler struct {
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

var (
	globalScheduler   *Scheduler
	globalSchedulerMu sync.Mutex
)

// GetScheduler 返回全局调度器（懒初始化）
func GetScheduler(ctx context.Context) *Scheduler {
	globalSchedulerMu.Lock()
	defer globalSchedulerMu.Unlock()
	if globalScheduler == nil {
		cfg := GetScheduleConfig()
		globalScheduler = &Scheduler{
			ctx:          ctx,
			sourceGap:    time.Duration(cfg.SourceGapSeconds) * time.Second,
			pageGap:      time.Duration(cfg.PageGapSeconds) * time.Second,
			sourceTimers: make(map[string]*time.Timer),
		}
	}
	return globalScheduler
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

	go func() {
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
			s.stopAllSourceTimers()
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
		s.stopAllSourceTimers()
		finished()
	}()
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
		case <-s.ctx.Done():
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
	sources, err := db.GetAllSources()
	if err != nil {
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
	// 取消旧定时器
	if old, ok := s.sourceTimers[sourceKey]; ok {
		old.Stop()
	}
	s.sourceTimersMu.Unlock()

	mode := sc.Mode
	if mode == "" {
		mode = model.CollectModeIncremental // 默认增量（只拉最新）
	}

	s.logScheduler(fmt.Sprintf("[%s] 后台采集已就绪: 每 %d 分钟, 模式=%s", sourceKey, sc.IntervalMin, mode))

	timer := time.AfterFunc(interval, func() {
		s.runSourceCollect(sourceKey, mode, 0)

		// 重新调度下一次
		s.scheduleSource(sourceKey, sc)
	})

	s.sourceTimersMu.Lock()
	s.sourceTimers[sourceKey] = timer
	s.sourceTimersMu.Unlock()
}

// runSourceCollect 执行单个源的采集
func (s *Scheduler) runSourceCollect(sourceKey string, mode model.CollectMode, hours int) {
	entry := GetOrCreateEngine(sourceKey)

	s.mu.Lock()
	pageGap := s.pageGap
	s.mu.Unlock()

	var engineOpts []collect.EngineOption
	engineOpts = append(engineOpts, collect.WithCollectMode(mode))
	if mode == model.CollectModeIncremental && hours > 0 {
		engineOpts = append(engineOpts, collect.WithTimeHours(hours))
	}

	engine := collect.NewEngineV2(
		sourceKey,
		func(msg string) {
			application.Get().Event.Emit("collect:log", map[string]interface{}{
				"source_key": sourceKey,
				"message":    msg,
			})
		},
		func(current, total int) {
			entry.UpdateProgress(current, total)
			application.Get().Event.Emit("collect:progress", map[string]interface{}{
				"source_key": sourceKey,
				"current":    current,
				"total":      total,
			})
		},
		func(page int, names []string) {
			entry.UpdatePageNames(page, names)
			application.Get().Event.Emit("collect:page", map[string]interface{}{
				"source_key": sourceKey,
				"page":       page,
				"names":      names,
			})
		},
		engineOpts...,
	)
	engine.SetContext(s.ctx)
	engine.SetPageGap(pageGap)
	if !entry.TryBindEngine(engine, string(mode)) {
		s.logScheduler(fmt.Sprintf("[%s] 正在采集中，跳过", sourceKey))
		return
	}
	// 持久化"最近一次采集"，设置页重启后仍能看到；只存活在内存里会一重启就归零。
	TouchLastRun()

	modeLabel := string(mode)
	s.logScheduler(fmt.Sprintf("[%s] 开始采集 (模式=%s)", sourceKey, modeLabel))

	done := make(chan struct{})
	go func() {
		defer close(done)
		stats, err := engine.Run()
		outcome := OutcomeFromRun(stats, err)
		entry.FinishEngine(engine, outcome)
		RecordCollectHealth(sourceKey, outcome, err)
		application.Get().Event.Emit("collect:done", CollectDonePayload(sourceKey, modeLabel, outcome))
		// 定时采集跑完同样是"库里的数据变了"，缓存必须跟着作废；空跑不动，理由见 collection.Service。
		if outcome.Saved > 0 {
			cache.InvalidateSource(sourceKey, fmt.Sprintf("定时采集写入 %d 条", outcome.Saved))
		}
		if err != nil {
			s.logScheduler(fmt.Sprintf("[%s] 采集未完成: %v", sourceKey, err))
		} else {
			s.logScheduler(fmt.Sprintf("[%s] 采集完成 (入库 %d 条)", sourceKey, outcome.Saved))
		}
	}()

	select {
	case <-done:
		return
	case <-s.ctx.Done():
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

// TriggerNow 立即触发一次全量采集（不影响定时）
func (s *Scheduler) TriggerNow() {
	gen := s.currentGeneration()
	go s.runAllSourcesOnce(gen, model.CollectModeFull, 0)
}

// TriggerOne 立即触发单个源的采集
func (s *Scheduler) TriggerOne(sourceKey string, mode model.CollectMode, hours int) {
	if sourceKey == "" {
		s.TriggerNow()
		return
	}
	go s.runSourceCollect(sourceKey, mode, hours)
}

// UpdateSourceSchedule 更新某个源的后台采集配置
func (s *Scheduler) UpdateSourceSchedule(sourceKey string) {
	if !GetScheduleConfig().EnableBackground {
		s.sourceTimersMu.Lock()
		if old, ok := s.sourceTimers[sourceKey]; ok {
			old.Stop()
			delete(s.sourceTimers, sourceKey)
		}
		s.sourceTimersMu.Unlock()
		return
	}
	// 检查调度器是否在运行，如果没有则重启
	s.mu.Lock()
	running := s.running
	s.mu.Unlock()

	if !running {
		// 调度器已停止，重新启动
		go s.Start()
		// 等待调度器启动完成
		time.Sleep(100 * time.Millisecond)
	}

	s.sourceTimersMu.Lock()
	if old, ok := s.sourceTimers[sourceKey]; ok {
		old.Stop()
		delete(s.sourceTimers, sourceKey)
	}
	s.sourceTimersMu.Unlock()

	src, err := db.GetSourceByKey(sourceKey)
	if err != nil || src.Enabled != 1 {
		return
	}
	sc := src.GetScheduleConfig()
	if sc == nil || !sc.Enabled {
		return
	}
	s.scheduleSource(sourceKey, sc)
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
	sources, _ := db.GetAllSources()
	for _, src := range sources {
		sc := src.GetScheduleConfig()
		entry := GetCollectStatus(src.SourceKey)
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
		case <-s.ctx.Done():
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
	application.Get().Event.Emit("collect:log", map[string]interface{}{
		"source_key": "__scheduler__",
		"message":    msg,
	})
}
