// Package parallel runs finite batches with a bounded number of owned workers.
package parallel

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
)

// Each waits for every started worker, cancels siblings on failure, and stops
// taking jobs on cancellation. Work must pass ctx through its blocking I/O.
func Each(ctx context.Context, count, limit int, work func(context.Context, int) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if count < 0 || limit < 1 {
		return fmt.Errorf("invalid parallel batch: count=%d limit=%d", count, limit)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var next atomic.Int64
	var group sync.WaitGroup
	var once sync.Once
	var failure error
	fail := func(err error) { once.Do(func() { failure = err; cancel() }) }
	for range min(count, limit) {
		group.Add(1)
		go func() {
			defer group.Done()
			defer func() {
				if recovered := recover(); recovered != nil {
					fail(fmt.Errorf("parallel worker panic: %v", recovered))
				}
			}()
			for ctx.Err() == nil {
				index := int(next.Add(1) - 1)
				if index >= count {
					return
				}
				if ctx.Err() != nil {
					return
				}
				if err := work(ctx, index); err != nil {
					fail(err)
					return
				}
			}
		}()
	}
	group.Wait()
	if failure != nil {
		return failure
	}
	return ctx.Err()
}
