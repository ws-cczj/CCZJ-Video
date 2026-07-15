package douban

import (
	"sync"
	"time"

	"cczjVideo/app/applog"
)

type Scheduler struct {
	mu       sync.Mutex
	updater  *Updater
	interval time.Duration
	ticker   *time.Ticker
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}
}

func NewScheduler(interval time.Duration) *Scheduler {
	return &Scheduler{
		updater:  NewUpdater(),
		interval: interval,
	}
}

func (s *Scheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		applog.Info("[Douban] Scheduler already running")
		return
	}

	s.running = true
	s.ticker = time.NewTicker(s.interval)
	s.stopCh = make(chan struct{})
	s.doneCh = make(chan struct{})
	ticker := s.ticker
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.mu.Unlock()

	applog.Info("[Douban] Scheduler started, interval: %s", s.interval)

	go func() {
		defer close(doneCh)
		for {
			select {
			case <-stopCh:
				applog.Info("[Douban] Scheduler ticker stopped")
				return
			case <-ticker.C:
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
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		applog.Info("[Douban] Scheduler already stopped")
		return
	}

	s.running = false
	ticker := s.ticker
	stopCh := s.stopCh
	doneCh := s.doneCh
	s.ticker = nil
	s.stopCh = nil
	s.doneCh = nil
	if ticker != nil {
		ticker.Stop()
	}
	close(stopCh)
	s.mu.Unlock()

	applog.Info("[Douban] Stopping scheduler (graceful)...")
	s.updater.RequestStop()

	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
	}

	applog.Info("[Douban] Scheduler stopped")
}

func (s *Scheduler) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

func (s *Scheduler) Updater() *Updater {
	return s.updater
}

func (s *Scheduler) TriggerNow() (int, error) {
	applog.Info("[Douban] Manual trigger requested")
	return s.updater.UpdateBatch()
}
