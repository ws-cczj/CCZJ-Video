package collection

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"

	"cczjVideo/app/model"
	"context"
	"fmt"
	"sync"
	"time"
)

// SchedulerService owns application access to collection and Douban schedulers.
type SchedulerService struct {
	runs      *Service
	mu        sync.Mutex
	active    context.Context
	scheduler *Scheduler
	douban    *douban.Scheduler
}

// NewSchedulerService constructs the scheduler facade.
func NewSchedulerService(interval time.Duration, runs *Service) *SchedulerService {
	return &SchedulerService{runs: runs, douban: douban.NewScheduler(interval)}
}

// Start starts collection and Douban schedulers using the application context.
func (s *SchedulerService) Start(ctx context.Context) {
	scheduler := s.collectionScheduler()
	// 应用上下文只在这里注入：调度器可能在更早的一次只读查询里就被建出来，
	// 那一刻它挂在的是"永不取消"。取消信号送不到采集引擎，退出时那一轮就会
	// 继续往已经关掉的库里写。
	s.mu.Lock()
	if ctx.Err() != nil {
		s.mu.Unlock()
		return
	}
	s.active = ctx
	scheduler.SetContext(ctx)
	s.scheduler = scheduler
	doubanScheduler := s.douban
	scheduler.Start()
	doubanScheduler.Updater().Enable(true)
	doubanScheduler.StartContext(ctx)
	s.mu.Unlock()

	// Keep the application task alive until shutdown so lifecycle.Group can
	// cancel and wait for both schedulers before the database is closed.
	<-ctx.Done()
	s.Stop()
	// Keep the mode owner registered until collection and Douban have drained.
	for s.IsRunning() {
		time.Sleep(50 * time.Millisecond)
	}
}

// Stop stops both schedulers and waits for their existing graceful-stop logic.
func (s *SchedulerService) Stop() {
	s.mu.Lock()
	deactivating := s.active == nil || s.active.Err() != nil
	if deactivating {
		s.active = nil
	}
	scheduler := s.scheduler
	doubanScheduler := s.douban
	s.mu.Unlock()
	if scheduler != nil {
		scheduler.Stop()
	}
	if deactivating {
		doubanScheduler.Updater().Enable(false)
	}
	doubanScheduler.Stop()
}

// Status returns collection scheduler state.
func (s *SchedulerService) Status() *SchedulerStatus {
	scheduler := s.collectionScheduler()
	status := scheduler.Status()
	return &status
}

// SetConfig persists and reloads the collection scheduler configuration.
func (s *SchedulerService) SetConfig(cfg CollectScheduleConfig) (CollectScheduleConfig, error) {
	if err := SetScheduleConfig(cfg); err != nil {
		return cfg, err
	}
	scheduler := s.collectionScheduler()
	s.mu.Lock()
	defer s.mu.Unlock()
	scheduler.ReloadConfig()
	if cfg.EnableBackground && s.active != nil && s.active.Err() == nil {
		scheduler.Stop()
		scheduler.Start()
	} else {
		scheduler.Stop()
	}
	return GetScheduleConfig(), nil
}

// Trigger starts collection work immediately without changing schedules.
func (s *SchedulerService) Trigger(ctx context.Context, sourceKey, mode string, hours int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if sourceKey != "" {
		row, err := db.GetSourceByKeyContext(ctx, sourceKey)
		if err != nil {
			return false, err
		}
		if row.GetKind() != model.SourceKindVideo {
			return false, apperror.New(apperror.Unsupported, "采集只支持视频源")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active == nil || s.active.Err() != nil {
		return false, apperror.New(apperror.Conflict, "视频采集调度尚未启动")
	}
	scheduler := s.scheduler
	collectMode := model.CollectMode(mode)
	if collectMode == "" {
		collectMode = model.CollectModeFull
	}
	if collectMode != model.CollectModeFull && collectMode != model.CollectModeIncremental && collectMode != model.CollectModeOnce {
		return false, apperror.New(apperror.Validation, "采集模式无效")
	}
	if sourceKey != "" {
		scheduler.TriggerOne(sourceKey, collectMode, hours)
		return true, nil
	}
	scheduler.TriggerNow()
	return true, nil
}

// SetSourceSchedule updates one source's schedule and its active timer.
func (s *SchedulerService) SetSourceSchedule(ctx context.Context, req SourceScheduleReq) error {
	source, err := db.GetSourceByKeyContext(ctx, req.SourceKey)
	if err != nil {
		return apperror.Wrap(apperror.NotFound, err, fmt.Sprintf("采集源 %s 不存在", req.SourceKey))
	}
	if source.GetKind() != model.SourceKindVideo {
		return apperror.New(apperror.Unsupported, "采集调度只支持视频源")
	}

	interval := req.IntervalMin
	if interval < 1 {
		interval = 1
	}
	source.SetScheduleConfig(&model.ScheduleConfig{
		Enabled:     req.Enabled,
		Mode:        model.CollectMode(req.Mode),
		IntervalMin: interval,
	})
	if err := db.SetSourceScheduleConfigContext(ctx, req.SourceKey, source.ScheduleCfgRaw); err != nil {
		return err
	}
	scheduler := s.collectionScheduler()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.Err() == nil {
		scheduler.UpdateSourceSchedule(req.SourceKey)
	}
	return nil
}

// IsRunning reports whether collection, active collection engines, or Douban work is active.
func (s *SchedulerService) IsRunning() bool {
	if s.collectionScheduler().IsRunning() {
		return true
	}
	if s.runs.registry.isRunning() {
		return true
	}

	return s.douban.IsRunning()
}

// UpdateDoubanByKeyword performs a manual Douban update.
func (s *SchedulerService) UpdateDoubanByKeyword(ctx context.Context, keyword string) (*douban.DoubanInfo, error) {
	s.mu.Lock()
	active := s.active != nil && s.active.Err() == nil
	s.mu.Unlock()
	if !active {
		return nil, apperror.New(apperror.Conflict, "请在视频模式使用豆瓣补全")
	}
	return s.douban.Updater().UpdateSingleByKeywordContext(ctx, keyword)
}

// TriggerDouban starts one Douban batch update.
func (s *SchedulerService) TriggerDouban(ctx context.Context) (int, error) {
	s.mu.Lock()
	active := s.active != nil && s.active.Err() == nil
	s.mu.Unlock()
	if !active {
		return 0, apperror.New(apperror.Conflict, "请在视频模式使用豆瓣补全")
	}
	return s.douban.TriggerNowContext(ctx)
}

// DoubanStatus returns scheduler and update state.
func (s *SchedulerService) DoubanStatus() (running, updating bool) {
	return s.douban.IsRunning(), s.douban.Updater().IsRunning()
}

// DoubanSchedule reports the completion polling cadence: interval, whether the
// scheduler is running, and the last/next tick. The UI needs the next tick to
// show how long the Douban queue will stay idle.
func (s *SchedulerService) DoubanSchedule() (interval time.Duration, running bool, lastTick, nextTick time.Time) {
	interval, lastTick, nextTick = s.douban.Schedule()
	return interval, s.douban.IsRunning(), lastTick, nextTick
}

// SetDoubanInterval changes the completion polling cadence at runtime and
// returns the value actually applied (the scheduler clamps very short intervals).
func (s *SchedulerService) SetDoubanInterval(d time.Duration) time.Duration {
	s.douban.SetInterval(d)
	interval, _, _ := s.douban.Schedule()
	return interval
}

func (s *SchedulerService) collectionScheduler() *Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduler == nil {
		s.scheduler = NewScheduler(s.runs)
	}
	return s.scheduler
}

func (s *SchedulerService) StopCollection() { s.collectionScheduler().Stop() }

// SourceChanged observes committed source changes without restarting a stopped scheduler.
func (s *SchedulerService) SourceChanged(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.active != nil && s.active.Err() == nil && s.scheduler != nil {
		s.scheduler.UpdateSourceSchedule(key)
	}
}
