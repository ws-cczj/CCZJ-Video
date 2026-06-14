package douban

import (
	"time"

	"cczjVideo/app/applog"
)

type Scheduler struct {
	updater  *Updater
	interval time.Duration
	ticker   *time.Ticker
	running  bool
}

func NewScheduler(interval time.Duration) *Scheduler {
	return &Scheduler{
		updater:  NewUpdater(),
		interval: interval,
	}
}

func (s *Scheduler) Start() {
	if s.running {
		applog.Info("[Douban] Scheduler already running")
		return
	}

	s.running = true
	s.ticker = time.NewTicker(s.interval)

	applog.Info("[Douban] Scheduler started, interval: %s", s.interval)

	go func() {
		for range s.ticker.C {
			if !s.running {
				applog.Info("[Douban] Scheduler ticker stopped")
				break
			}

			applog.Info("[Douban] Scheduler tick triggered")
			count, err := s.updater.UpdateBatch()
			if err != nil {
				applog.Error("[Douban] Scheduler batch update ERROR: %v", err)
			} else if count > 0 {
				applog.Info("[Douban] Scheduler batch update SUCCESS: %d videos updated", count)
			} else {
				applog.Info("[Douban] Scheduler batch update: no videos updated")
			}
		}
	}()
}

func (s *Scheduler) Stop() {
	if !s.running {
		applog.Info("[Douban] Scheduler already stopped")
		return
	}

	applog.Info("[Douban] Stopping scheduler...")
	s.running = false
	if s.ticker != nil {
		s.ticker.Stop()
		s.ticker = nil
	}

	applog.Info("[Douban] Scheduler stopped")
}

func (s *Scheduler) IsRunning() bool {
	return s.running
}

func (s *Scheduler) Updater() *Updater {
	return s.updater
}

func (s *Scheduler) TriggerNow() (int, error) {
	applog.Info("[Douban] Manual trigger requested")
	return s.updater.UpdateBatch()
}