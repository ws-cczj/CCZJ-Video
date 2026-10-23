package quality

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const MaxChannels = 5

type DispatchStats struct {
	Workers       int   `json:"workers"`
	QueueCapacity int   `json:"queueCapacity"`
	Submitted     int64 `json:"submitted"`
	Completed     int64 `json:"completed"`
	PeakActive    int64 `json:"peakActive"`
	Millis        int64 `json:"millis"`
}

// Dispatch creates only workers+2 goroutines, even for tens of thousands of
// sources. Both channels have capacity five; a slow writer backpressures workers.
// consume is serial and can append a journal without locks or quadratic rewrites.
func Dispatch[T, R any](ctx context.Context, tasks []T, workers int, execute func(context.Context, T) R, consume func(T, R) error) (DispatchStats, error) {
	stats := DispatchStats{Workers: workers, QueueCapacity: MaxChannels}
	if workers < 1 || workers > MaxChannels {
		return stats, fmt.Errorf("worker count must be 1..5")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	start := time.Now()
	type result struct {
		task  T
		value R
	}
	jobs := make(chan T, MaxChannels)
	results := make(chan result, MaxChannels)
	var wg sync.WaitGroup
	var active, peak, submitted atomic.Int64
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case task, ok := <-jobs:
					if !ok {
						return
					}
					if ctx.Err() != nil {
						return
					}
					n := active.Add(1)
					for old := peak.Load(); n > old; old = peak.Load() {
						if peak.CompareAndSwap(old, n) {
							break
						}
					}
					value := execute(ctx, task)
					active.Add(-1)
					select {
					case results <- result{task, value}:
					case <-ctx.Done():
						return
					}
				}
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(jobs)
		for _, task := range tasks {
			select {
			case <-ctx.Done():
				return
			case jobs <- task:
				submitted.Add(1)
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()
	var first error
	for r := range results {
		if first != nil {
			continue
		}
		if err := consume(r.task, r.value); err != nil {
			first = err
			cancel()
			continue
		}
		stats.Completed++
	}
	stats.Submitted, stats.PeakActive, stats.Millis = submitted.Load(), peak.Load(), time.Since(start).Milliseconds()
	if first != nil {
		return stats, first
	}
	return stats, ctx.Err()
}
