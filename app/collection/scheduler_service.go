package collection

import (
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"context"
	"fmt"
	"sync"
	"time"
)

// SchedulerService owns application access to collection and Douban schedulers.
type SchedulerService struct {
	mu        sync.Mutex
	scheduler *handler.Scheduler
	douban    *douban.Scheduler
}

// NewSchedulerService constructs the scheduler facade.
func NewSchedulerService(interval time.Duration) *SchedulerService {
	return &SchedulerService{douban: douban.NewScheduler(interval)}
}

// Start starts collection and Douban schedulers using the application context.
func (s *SchedulerService) Start(ctx context.Context) {
	s.mu.Lock()
	s.scheduler = handler.GetScheduler(ctx)
	scheduler := s.scheduler
	doubanScheduler := s.douban
	s.mu.Unlock()
	scheduler.Start()
	doubanScheduler.Start()

	// Keep the application task alive until shutdown so lifecycle.Group can
	// cancel and wait for both schedulers before the database is closed.
	<-ctx.Done()
	s.Stop()
}

// Stop stops both schedulers and waits for their existing graceful-stop logic.
func (s *SchedulerService) Stop() {
	s.mu.Lock()
	scheduler := s.scheduler
	doubanScheduler := s.douban
	s.mu.Unlock()
	if scheduler != nil {
		scheduler.Stop()
	}
	doubanScheduler.Stop()
}

// Status returns collection scheduler state.
func (s *SchedulerService) Status() *handler.SchedulerStatus {
	scheduler := s.collectionScheduler(context.Background())
	status := scheduler.Status()
	return &status
}

// SetConfig persists and reloads the collection scheduler configuration.
func (s *SchedulerService) SetConfig(cfg handler.CollectScheduleConfig) (handler.CollectScheduleConfig, error) {
	if err := handler.SetScheduleConfig(cfg); err != nil {
		return cfg, err
	}
	scheduler := s.collectionScheduler(context.Background())
	scheduler.ReloadConfig()
	if cfg.EnableBackground {
		scheduler.Stop()
		scheduler.Start()
	} else {
		scheduler.Stop()
	}
	return handler.GetScheduleConfig(), nil
}

// Trigger starts collection work immediately without changing schedules.
func (s *SchedulerService) Trigger(sourceKey, mode string, hours int) {
	scheduler := s.collectionScheduler(context.Background())
	collectMode := model.CollectMode(mode)
	if collectMode == "" {
		collectMode = model.CollectModeFull
	}
	if sourceKey != "" {
		scheduler.TriggerOne(sourceKey, collectMode, hours)
		return
	}
	scheduler.TriggerNow()
}

// SetSourceSchedule updates one source's schedule and its active timer.
func (s *SchedulerService) SetSourceSchedule(req handler.SourceScheduleReq) error {
	source, err := db.GetSourceByKey(req.SourceKey)
	if err != nil {
		return fmt.Errorf("source does not exist: %s", req.SourceKey)
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
	if err := db.UpdateSource(source); err != nil {
		return err
	}
	s.collectionScheduler(context.Background()).UpdateSourceSchedule(req.SourceKey)
	return nil
}

// IsRunning reports whether collection, active collection engines, or Douban work is active.
func (s *SchedulerService) IsRunning() bool {
	if s.collectionScheduler(context.Background()).IsRunning() {
		return true
	}
	sources, err := handler.GetAllSources()
	if err == nil {
		for _, source := range sources {
			if status := handler.GetCollectStatus(source.SourceKey); status != nil && status.Running {
				return true
			}
		}
	}
	return s.douban.IsRunning()
}

// UpdateDoubanByKeyword performs a manual Douban update.
func (s *SchedulerService) UpdateDoubanByKeyword(keyword string) (*douban.DoubanInfo, error) {
	return s.douban.Updater().UpdateSingleByKeyword(keyword)
}

// TriggerDouban starts one Douban batch update.
func (s *SchedulerService) TriggerDouban() (int, error) {
	return s.douban.TriggerNow()
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

func (s *SchedulerService) collectionScheduler(ctx context.Context) *handler.Scheduler {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.scheduler == nil {
		s.scheduler = handler.GetScheduler(ctx)
	}
	return s.scheduler
}
