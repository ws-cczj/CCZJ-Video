package douban

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"cczjVideo/app/db"
)

func TestStoppingUpdaterCancelsActiveHTTP(t *testing.T) {
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.GetOrCreateGlobalID("ownership cancellation fixture", 0); err != nil {
		t.Fatal(err)
	}
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	entered, disconnected, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		select {
		case <-r.Context().Done():
			close(disconnected)
		case <-release:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	old := client
	upstream, _ := url.Parse(server.URL)
	client = &http.Client{Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
		copy := r.Clone(r.Context())
		target := *r.URL
		target.Scheme, target.Host = upstream.Scheme, upstream.Host
		copy.URL = &target
		return http.DefaultTransport.RoundTrip(copy)
	})}
	u := NewUpdater()
	done := make(chan error, 1)
	go func() { _, err := u.UpdateBatch(); done <- err }()
	t.Cleanup(func() { close(release); server.Close(); <-done; client = old })
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("updater did not reach HTTP")
	}
	u.RequestStop()
	select {
	case <-disconnected:
	case <-time.After(400 * time.Millisecond):
		t.Fatal("stopping updater left active HTTP running")
	}
	// Keep the result for cleanup, while verifying that cancellation stays visible.
	select {
	case err := <-done:
		done <- err
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("stop was reported as success: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("updater did not finish after HTTP cancellation")
	}
}

func TestUpdaterRejectsCancelledOwnerBeforeDatabaseAccess(t *testing.T) {
	owner, cancel := context.WithCancel(context.Background())
	u := NewUpdater()
	u.SetContext(owner)
	cancel()
	if _, err := u.UpdateBatch(); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled mode accepted a batch: %v", err)
	}
	if u.IsRunning() {
		t.Fatal("cancelled batch published running state")
	}
}

func TestSchedulerOwnerStopsLoopAndAllowsNextOwner(t *testing.T) {
	s := NewScheduler(0)
	owner, cancel := context.WithCancel(context.Background())
	s.StartContext(owner)
	s.mu.Lock()
	done := s.doneCh
	s.mu.Unlock()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scheduler loop survived owner cancellation")
	}
	if s.IsRunning() {
		t.Fatal("drained scheduler reported running")
	}
	s.StartContext(context.Background())
	t.Cleanup(s.Stop)
	if !s.IsRunning() {
		t.Fatal("new mode could not start its scheduler")
	}
	s.Stop()
	if _, err := s.TriggerNow(); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped owner accepted manual batch: %v", err)
	}
}

func TestSchedulerKeepsLatestQueuedInterval(t *testing.T) {
	s := NewScheduler(time.Minute)
	s.reconfigure = make(chan time.Duration, 1)
	s.SetInterval(2 * time.Minute)
	s.SetInterval(4 * time.Minute)
	if got := <-s.reconfigure; got != 4*time.Minute {
		t.Fatalf("queued stale interval: %v", got)
	}
}
