package download

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestExecutorBoundsAdmissionAndDoesNotRunCancelledQueue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var wg sync.WaitGroup
	launch := func(_ string, work func(context.Context)) bool {
		wg.Add(1)
		go func() { defer wg.Done(); work(ctx) }()
		return true
	}
	e := NewExecutor(ctx, launch, 1, 1)
	started, release := make(chan struct{}), make(chan struct{})
	first, err := e.Submit("first", func(context.Context) { close(started); <-release })
	if err != nil {
		t.Fatal(err)
	}
	<-started
	ran := make(chan struct{}, 1)
	queued, err := e.Submit("queued", func(context.Context) { ran <- struct{}{} })
	if err != nil {
		t.Fatal(err)
	}
	if stats := e.Stats(); stats.Active != 1 || stats.Accepted != 2 || stats.ConcurrencyLimit != 1 || stats.AdmissionLimit != 2 {
		t.Fatalf("executor diagnostic admission = %+v", stats)
	}
	if _, err := e.Submit("overflow", func(context.Context) {}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("admission = %v", err)
	}
	queued.Cancel()
	waitCtx, stop := context.WithTimeout(context.Background(), time.Second)
	defer stop()
	if err := queued.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := first.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if stats := e.Stats(); stats.Active != 0 || stats.Accepted != 0 {
		t.Fatalf("released execution still registered: %+v", stats)
	}
	select {
	case <-ran:
		t.Fatal("cancelled queued work executed")
	default:
	}
	third, err := e.Submit("reused", func(context.Context) {})
	if err != nil {
		t.Fatal(err)
	}
	if err := third.Wait(waitCtx); err != nil {
		t.Fatal(err)
	}
}

func TestExecutorRejectedLauncherReleasesCapacity(t *testing.T) {
	e := NewExecutor(context.Background(), func(string, func(context.Context)) bool { return false }, 1, 0)
	for range 3 {
		if _, err := e.Submit("rejected", func(context.Context) { t.Error("rejected work ran") }); !errors.Is(err, context.Canceled) {
			t.Fatalf("rejection = %v", err)
		}
	}
}
