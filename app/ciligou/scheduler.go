package ciligou

import (
	"time"

	"cczjVideo/app/applog"
)

// Scheduler 磁力链接爬取调度器
type Scheduler struct {
	updater  *Updater
	interval time.Duration
	ticker   *time.Ticker
	running  bool
}

// NewScheduler 创建调度器
func NewScheduler(interval time.Duration) *Scheduler {
	return &Scheduler{
		updater:  NewUpdater(),
		interval: interval,
	}
}

// Start 启动调度器
func (s *Scheduler) Start() {
	if s.running {
		applog.Info("[Ciligou] Scheduler already running")
		return
	}

	s.running = true
	s.ticker = time.NewTicker(s.interval)

	applog.Info("[Ciligou] Scheduler started, interval: %s", s.interval)

	go func() {
		for range s.ticker.C {
			if !s.running {
				applog.Info("[Ciligou] Scheduler ticker stopped")
				break
			}

			applog.Info("[Ciligou] Scheduler tick triggered")
			count, err := s.updater.UpdateBatch()
			if err != nil {
				applog.Error("[Ciligou] Scheduler batch update ERROR: %v", err)
			} else if count > 0 {
				applog.Info("[Ciligou] Scheduler batch update SUCCESS: %d magnet links updated", count)
			} else {
				applog.Info("[Ciligou] Scheduler batch update: no magnet links updated")
			}
		}
	}()
}

// Stop 停止调度器
func (s *Scheduler) Stop() {
	if !s.running {
		applog.Info("[Ciligou] Scheduler already stopped")
		return
	}

	applog.Info("[Ciligou] Stopping scheduler (graceful)...")
	s.running = false
	s.updater.RequestStop()
	if s.ticker != nil {
		s.ticker.Stop()
		s.ticker = nil
	}

	done := make(chan struct{})
	go func() {
		for s.updater.IsRunning() {
			time.Sleep(100 * time.Millisecond)
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}

	applog.Info("[Ciligou] Scheduler stopped")
}

// IsRunning 是否正在运行
func (s *Scheduler) IsRunning() bool {
	return s.running
}

// Updater 获取更新器
func (s *Scheduler) Updater() *Updater {
	return s.updater
}

// TriggerNow 立即触发一次磁力链接获取
func (s *Scheduler) TriggerNow() (int, error) {
	applog.Info("[Ciligou] Manual trigger requested")
	return s.updater.UpdateBatch()
}