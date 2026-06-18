package handler

import (
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
//   - 根据全局配置决定是否执行启动阶段采集（全量/补采）
//   - 然后为每个启用后台采集的源启动独立定时器
func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.stopCh = make(chan struct{})
	s.stopped = make(chan struct{})
	cfg := GetScheduleConfig()
	s.mu.Unlock()

	go func() {
		defer close(s.stopped)

		// === 启动阶段 ===
		if cfg.EnableInitialFullCollect {
			s.logScheduler("启动阶段全量采集开始")
			s.runAllSourcesOnce(model.CollectModeFull, 0)
		} else if cfg.EnableStartupCatchup {
			s.logScheduler("启动阶段补采开始")
			// 补采：用上次退出到现在的时长作为时间窗
			lastExit := GetLastExitUnix()
			hours := 0
			if lastExit > 0 {
				hours = int(time.Since(time.Unix(lastExit, 0)).Hours())
				if hours < 1 {
					hours = 1
				}
			}
			s.logScheduler(fmt.Sprintf("补采时间窗: %d 小时", hours))
			s.runAllSourcesOnce(model.CollectModeIncremental, hours)
		}

		// === 后台周期循环 ===
		// 为每个源启动独立定时器
		s.startSourceTimers()

		// 主循环：等待停止信号
		<-s.stopCh
		s.stopAllSourceTimers()
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
}

// runAllSourcesOnce 采集所有源一次（启动阶段用）
func (s *Scheduler) runAllSourcesOnce(mode model.CollectMode, hours int) {
	sources, err := db.GetEnabledSources()
	if err != nil {
		s.logScheduler("读取采集源列表失败: " + err.Error())
		return
	}
	if len(sources) == 0 {
		s.logScheduler("没有可用的采集源，跳过")
		return
	}

	setSchedulerLastRun(time.Now().Unix())

	for i, src := range sources {
		select {
		case <-s.stopCh:
			s.logScheduler("后台采集被停止")
			return
		case <-s.ctx.Done():
			return
		default:
		}

		if i > 0 {
			s.mu.Lock()
			gap := s.sourceGap
			s.mu.Unlock()
			if !s.sleepInterruptible(gap) {
				return
			}
		}

		s.runSourceCollect(src.SourceKey, mode, hours)
	}
	s.logScheduler("启动阶段采集结束")
}

// startSourceTimers 为每个启用后台采集的源启动独立定时器
func (s *Scheduler) startSourceTimers() {
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
	if sc.IntervalMin < 5 {
		sc.IntervalMin = 5 // 最小 5 分钟
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
	if entry.IsRunning() {
		s.logScheduler(fmt.Sprintf("[%s] 正在采集中，跳过", sourceKey))
		return
	}

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
	entry.BindEngine(engine, string(mode))

	modeLabel := string(mode)
	s.logScheduler(fmt.Sprintf("[%s] 开始采集 (模式=%s)", sourceKey, modeLabel))

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := engine.Run()
		entry.MarkDone(errStrSchedule(err))
		application.Get().Event.Emit("collect:done", map[string]interface{}{
			"source_key": sourceKey,
			"error":      errStrSchedule(err),
			"mode":       modeLabel,
		})
		if err != nil {
			s.logScheduler(fmt.Sprintf("[%s] 采集失败: %v", sourceKey, err))
		} else {
			s.logScheduler(fmt.Sprintf("[%s] 采集完成", sourceKey))
		}
	}()

	select {
	case <-done:
		return
	case <-s.ctx.Done():
		engine.Stop()
		<-done
		return
	case <-s.stopCh:
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
	go s.runAllSourcesOnce(model.CollectModeFull, 0)
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
	Running                bool                    `json:"running"`
	Background             bool                    `json:"background"`
	BackgroundEveryMinutes int                     `json:"background_every_minutes"`
	BackgroundEverySeconds int                     `json:"background_every_seconds"`
	SourceGapSeconds       int                     `json:"source_gap_seconds"`
	PageGapSeconds         int                     `json:"page_gap_seconds"`
	LastExitUnix           int64                   `json:"last_exit_unix"`
	LastRunUnix            int64                   `json:"last_run_unix"`
	NowUnix                int64                   `json:"now_unix"`
	Note                   string                  `json:"note"`
	SourceSchedules        []SourceScheduleItem    `json:"source_schedules"`
}

// SourceScheduleItem 单个源的调度信息
type SourceScheduleItem struct {
	SourceKey   string `json:"source_key"`
	Name        string `json:"name"`
	Enabled     bool   `json:"enabled"`
	Mode        string `json:"mode"`         // full | incremental
	IntervalMin int    `json:"interval_min"` // 定时间隔（分钟）
	Running     bool   `json:"running"`      // 是否正在采集
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

	// 收集每个源的调度信息
	var srcItems []SourceScheduleItem
	sources, _ := db.GetAllSources()
	for _, src := range sources {
		if src.Enabled != 1 {
			continue
		}
		sc := src.GetScheduleConfig()
		entry := GetCollectStatus(src.SourceKey)
		item := SourceScheduleItem{
			SourceKey: src.SourceKey,
			Name:      src.Name,
			Running:   entry.Running,
		}
		if sc != nil && sc.Enabled {
			item.Enabled = true
			item.Mode = string(sc.Mode)
			if item.Mode == "" {
				item.Mode = "incremental"
			}
			item.IntervalMin = sc.IntervalMin
			if item.IntervalMin < 5 {
				item.IntervalMin = 5
			}
		}
		srcItems = append(srcItems, item)
	}

	note := fmt.Sprintf("独立定时器模式: 每个源按各自配置间隔采集")
	if !bgEnabled {
		note = "后台周期采集已禁用"
	}

	return SchedulerStatus{
		Running:                running,
		Background:             bgEnabled,
		BackgroundEveryMinutes: everyMin,
		BackgroundEverySeconds: everySec,
		SourceGapSeconds:       sourceGap,
		PageGapSeconds:         pageGap,
		LastExitUnix:           GetLastExitUnix(),
		LastRunUnix:            schedulerLastRun,
		NowUnix:                time.Now().Unix(),
		Note:                   note,
		SourceSchedules:        srcItems,
	}
}

// sleepInterruptible 睡眠期间可被停止
func (s *Scheduler) sleepInterruptible(d time.Duration) bool {
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
		case <-s.stopCh:
			return false
		case <-s.ctx.Done():
			return false
		}
	}
}

func (s *Scheduler) logScheduler(msg string) {
	application.Get().Event.Emit("collect:log", map[string]interface{}{
		"source_key": "__scheduler__",
		"message":    msg,
	})
}

var (
	schedulerLastRun    int64
	schedulerLastRunMu sync.Mutex
)

func setSchedulerLastRun(t int64) {
	schedulerLastRunMu.Lock()
	defer schedulerLastRunMu.Unlock()
	schedulerLastRun = t
}

func errStrSchedule(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}