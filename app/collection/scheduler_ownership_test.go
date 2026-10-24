package collection

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/model"
	sourceservice "cczjVideo/app/source"
)

func TestDisablingScheduleDuringRunCannotBeUndoneByOldTimer(t *testing.T) {
	prior := GetScheduleConfig()
	if err := SetScheduleConfig(CollectScheduleConfig{EnableBackground: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetScheduleConfig(prior) })
	started, release := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		fmt.Fprint(w, `{"code":1,"page":1,"pagecount":1,"total":0,"list":[]}`)
	}))
	defer server.Close()
	key := "timer_disabled_during_run"
	source := &model.Source{SourceKey: key, Name: key, ApiUrl: server.URL, Enabled: 1}
	source.SetScheduleConfig(&model.ScheduleConfig{Enabled: true, IntervalMin: 10, Mode: model.CollectModeOnce})
	if err := db.AddSource(source); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteSource(key) })
	owner := lifecycle.NewGroup()
	runs := NewService(nil, owner.Go, owner.Context())
	s := NewScheduler(runs)
	s.SetContext(owner.Context())
	s.running = true
	defer func() { s.stopAllSourceTimers(); owner.Stop(time.Second) }()
	s.scheduleSource(key, source.GetScheduleConfig())
	s.sourceTimersMu.Lock()
	s.sourceTimers[key].Reset(0)
	s.sourceTimersMu.Unlock()
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("timer did not start upstream work")
	}
	service := NewSchedulerService(time.Minute, runs)
	service.active = owner.Context()
	service.scheduler = s
	err := service.SetSourceSchedule(context.Background(), SourceScheduleReq{SourceKey: key, Enabled: false, IntervalMin: 10})
	close(release)
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for len(owner.ActiveTasks()) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(owner.ActiveTasks()) > 0 {
		t.Fatal("timer work did not finish")
	}
	s.sourceTimersMu.Lock()
	timer := s.sourceTimers[key]
	s.sourceTimersMu.Unlock()
	if timer != nil {
		t.Fatal("completed old timer restored the disabled schedule")
	}
}

func TestCommittedSourceChangesRefreshTimersWithoutRestartingScheduler(t *testing.T) {
	prior := GetScheduleConfig()
	if err := SetScheduleConfig(CollectScheduleConfig{EnableBackground: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = SetScheduleConfig(prior) })
	for _, action := range []string{"disable", "delete", "import_schedule"} {
		t.Run(action, func(t *testing.T) {
			owner := lifecycle.NewGroup()
			defer owner.Stop(time.Second)
			runs := NewService(nil, owner.Go, owner.Context())
			s := NewScheduler(runs)
			s.SetContext(owner.Context())
			s.running = true
			defer s.stopAllSourceTimers()
			key := "timer_source_" + action
			row := &model.Source{SourceKey: key, Name: key, ApiUrl: "https://example.invalid", Enabled: 1}
			row.SetScheduleConfig(&model.ScheduleConfig{Enabled: true, IntervalMin: 10})
			if err := db.AddSource(row); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = db.DeleteSource(key) })
			s.scheduleSource(key, row.GetScheduleConfig())
			service := NewSchedulerService(time.Minute, runs)
			service.active = owner.Context()
			service.scheduler = s
			sources := sourceservice.NewServiceWithRuntime(runs, nil)
			sources.SetChangeObserver(service.SourceChanged)
			var err error
			switch action {
			case "disable":
				row.Enabled = 0
				err = sources.Update(context.Background(), row)
			case "delete":
				err = sources.Delete(context.Background(), key)
			case "import_schedule":
				row.SetScheduleConfig(&model.ScheduleConfig{Enabled: false, IntervalMin: 10})
				_, err = sources.ImportContext(context.Background(), sourceservice.Payload{Version: 2, Source: row}, "test")
			}
			if err != nil {
				t.Fatal(err)
			}
			s.sourceTimersMu.Lock()
			timer := s.sourceTimers[key]
			s.sourceTimersMu.Unlock()
			if timer != nil {
				t.Fatal("source mutation retained its old timer")
			}
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			service.SourceChanged(key)
			if s.IsRunning() {
				t.Fatal("source notification restarted a stopped scheduler")
			}
		})
	}
}

func TestSupersededSchedulerLoopCannotRemoveCurrentTimers(t *testing.T) {
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	s := &Scheduler{generation: 2, sourceTimers: map[string]*time.Timer{"source": timer}}
	s.stopSourceTimersForGeneration(1)
	if s.sourceTimers["source"] != timer {
		t.Fatal("old loop removed the current generation's timer")
	}
	s.stopSourceTimersForGeneration(2)
	if len(s.sourceTimers) != 0 {
		t.Fatal("current generation did not release its timers")
	}
}
