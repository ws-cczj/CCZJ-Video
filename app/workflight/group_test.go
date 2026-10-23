package workflight

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedReadSurvivesFirstWaiterCancellation(t *testing.T) {
	g := New[string](context.Background(), 2)
	firstCtx, stopFirst := context.WithCancel(context.Background())
	defer stopFirst()
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	work := func(ctx context.Context) (string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			return "same", nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	first := make(chan error, 1)
	go func() { _, err := g.Do(firstCtx, "key", work); first <- err }()
	<-started
	second := make(chan string, 1)
	go func() {
		value, err := g.Do(context.Background(), "key", work)
		if err != nil {
			second <- err.Error()
		} else {
			second <- value
		}
	}()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, joined := g.Stats()
		if joined == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	stopFirst()
	if err := <-first; err != context.Canceled {
		t.Fatalf("first = %v", err)
	}
	close(release)
	if got := <-second; got != "same" || calls.Load() != 1 {
		t.Fatalf("shared result = %s, calls %d", got, calls.Load())
	}
	if err := g.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAllWaitersCancelWorkAndCapacityCountsDetachedWork(t *testing.T) {
	g := New[int](context.Background(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	started, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	returned := make(chan error, 1)
	go func() {
		_, err := g.Do(ctx, "one", func(ctx context.Context) (int, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			<-release
			return 0, ctx.Err()
		})
		returned <- err
	}()
	<-started
	cancel()
	<-returned
	<-cancelled
	if _, err := g.Do(context.Background(), "other", func(context.Context) (int, error) { t.Error("overflow ran"); return 0, nil }); err == nil {
		t.Fatal("uncooperative detached work escaped admission limit")
	}
	close(release)
	if err := g.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Do(context.Background(), "late", func(context.Context) (int, error) { return 1, nil }); err != context.Canceled {
		t.Fatalf("closed group = %v", err)
	}
}
