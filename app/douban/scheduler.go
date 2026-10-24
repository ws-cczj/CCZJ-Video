package douban

import (
	"context"
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
	doneCh   chan struct{}
	cancel   context.CancelFunc
	// reconfigure 由 SetInterval 投递新间隔。ticker 归启动协程所有，
	// 包外只通过通道改它，避免两处同时换指针。
	reconfigure chan time.Duration
	// anchor 是当前这一轮计时的起点，lastTick 记录上一次真正触发的时刻，
	// 两者合起来才能算出「下次大约什么时候跑」给诊断页显示。
	anchor   time.Time
	lastTick time.Time
}

func NewScheduler(interval time.Duration) *Scheduler {
	if interval < time.Minute {
		interval = time.Minute
	}
	return &Scheduler{
		updater:  NewUpdater(),
		interval: interval,
	}
}

func (s *Scheduler) Start() {
	s.StartContext(context.Background())
}

func (s *Scheduler) StartContext(owner context.Context) {
	s.mu.Lock()
	if s.running || owner.Err() != nil {
		s.mu.Unlock()
		applog.Info("[Douban] Scheduler already running")
		return
	}

	s.running = true
	ctx, cancel := context.WithCancel(owner)
	s.cancel = cancel
	s.updater.SetContext(ctx)
	s.ticker = time.NewTicker(s.interval)
	s.doneCh = make(chan struct{})
	s.reconfigure = make(chan time.Duration, 1)
	s.anchor = time.Now()
	s.lastTick = time.Time{}
	ticker := s.ticker
	doneCh := s.doneCh
	reconfigure := s.reconfigure
	interval := s.interval
	s.mu.Unlock()

	applog.Info("[Douban] Scheduler started, interval: %s", interval)

	go func() {
		defer func() {
			ticker.Stop()
			cancel()
			s.mu.Lock()
			if s.doneCh == doneCh {
				s.running = false
				s.ticker, s.doneCh, s.reconfigure, s.cancel = nil, nil, nil, nil
			}
			close(doneCh)
			s.mu.Unlock()
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case next := <-reconfigure:
				ticker.Stop()
				ticker = time.NewTicker(next)
				s.mu.Lock()
				s.ticker = ticker
				s.anchor = time.Now()
				s.mu.Unlock()
				applog.Info("[Douban] Scheduler interval changed to %s", next)
				continue
			case <-ticker.C:
			}
			if ctx.Err() != nil {
				return
			}
			s.mu.Lock()
			s.lastTick = time.Now()
			s.anchor = s.lastTick
			s.mu.Unlock()
			applog.Info("[Douban] Scheduler tick triggered")
			count, err := s.updater.UpdateBatchContext(ctx)
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
		s.updater.RequestStop()
		applog.Info("[Douban] Scheduler already stopped")
		return
	}

	ticker := s.ticker
	doneCh := s.doneCh
	cancel := s.cancel
	if ticker != nil {
		ticker.Stop()
	}
	cancel()
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
	return s.running || s.updater.IsRunning()
}

func (s *Scheduler) Updater() *Updater {
	return s.updater
}

func (s *Scheduler) TriggerNow() (int, error) {
	return s.TriggerNowContext(context.Background())
}

func (s *Scheduler) TriggerNowContext(ctx context.Context) (int, error) {
	applog.Info("[Douban] Manual trigger requested")
	return s.updater.UpdateBatchContext(ctx)
}

// SetInterval 改轮询间隔。调度器在跑就立刻重建计时，不用重启应用。
func (s *Scheduler) SetInterval(d time.Duration) {
	if d < time.Minute {
		d = time.Minute
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.interval = d
	reconfigure := s.reconfigure
	if reconfigure == nil {
		return
	}
	select {
	case reconfigure <- d:
	default:
		// Preserve the newest setting while a batch keeps the loop busy.
		select {
		case <-reconfigure:
		default:
		}
		select {
		case reconfigure <- d:
		default:
		}
	}
}

// Schedule 返回间隔、上次触发时刻与下次预计触发时刻；未运行时后两者为零值。
func (s *Scheduler) Schedule() (time.Duration, time.Time, time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return s.interval, time.Time{}, time.Time{}
	}
	return s.interval, s.lastTick, s.anchor.Add(s.interval)
}
