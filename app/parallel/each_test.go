package parallel

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEachBoundsWorkersAndVisitsEachIndexOnce(t *testing.T) {
	var active, peak atomic.Int32
	visited := make([]atomic.Int32, 1000)
	entered, release := make(chan struct{}, 4), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- Each(context.Background(), len(visited), 4, func(_ context.Context, index int) error {
			current := active.Add(1)
			for old := peak.Load(); current > old && !peak.CompareAndSwap(old, current); old = peak.Load() {
			}
			if index < 4 {
				entered <- struct{}{}
				<-release
			}
			visited[index].Add(1)
			active.Add(-1)
			return nil
		})
	}()
	for range 4 {
		<-entered
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if peak.Load() != 4 || active.Load() != 0 {
		t.Fatal(peak.Load(), active.Load())
	}
	for index := range visited {
		if visited[index].Load() != 1 {
			t.Fatal(index, visited[index].Load())
		}
	}
}

func TestEachStopsDispatchAndWaitsForStartedWorkers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{}, 3)
	var calls, exited atomic.Int32
	done := make(chan error, 1)
	go func() {
		done <- Each(ctx, 1000, 3, func(ctx context.Context, _ int) error {
			calls.Add(1)
			entered <- struct{}{}
			<-ctx.Done()
			exited.Add(1)
			return ctx.Err()
		})
	}()
	for range 3 {
		<-entered
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if calls.Load() != 3 || exited.Load() != 3 {
		t.Fatal(calls.Load(), exited.Load())
	}
}

func TestEachPropagatesFailureAndRecoversWorkerPanic(t *testing.T) {
	failure := errors.New("read failed")
	if err := Each(context.Background(), 10, 1, func(context.Context, int) error { return failure }); !errors.Is(err, failure) {
		t.Fatal(err)
	}
	if err := Each(context.Background(), 10, 2, func(context.Context, int) error { panic("worker") }); err == nil {
		t.Fatal("lost worker panic")
	}
	if err := Each(context.Background(), 0, 4, func(context.Context, int) error { t.Fatal("empty job"); return nil }); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkEach(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = Each(context.Background(), count, 4, func(context.Context, int) error { return nil })
			}
		})
	}
}

// The previous probe dispatcher bounded I/O but still spawned count goroutines.
func BenchmarkSemaphoreFanout(b *testing.B) {
	for _, count := range []int{100, 10000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				sem := make(chan struct{}, 4)
				var group sync.WaitGroup
				for range count {
					group.Add(1)
					go func() { defer group.Done(); sem <- struct{}{}; <-sem }()
				}
				group.Wait()
			}
		})
	}
}
