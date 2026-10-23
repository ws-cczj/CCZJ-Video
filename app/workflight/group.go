// Package workflight shares concurrent immutable reads without coupling their
// lifetime to the first caller. The last departing caller cancels upstream work.
package workflight

import (
	"context"
	"fmt"
	"sync"

	"cczjVideo/app/apperror"
)

type call[T any] struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	waiters int
	value   T
	err     error
}
type Group[T any] struct {
	mu            sync.Mutex
	owner         context.Context
	entries       map[string]*call[T]
	active, limit int
	joined        uint64
	closed        bool
	wg            sync.WaitGroup
}

func New[T any](owner context.Context, limit int) *Group[T] {
	if limit < 1 {
		limit = 1
	}
	return &Group[T]{owner: owner, limit: limit, entries: make(map[string]*call[T])}
}

func (g *Group[T]) Do(ctx context.Context, key string, work func(context.Context) (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	g.mu.Lock()
	if g.closed || g.owner.Err() != nil {
		g.mu.Unlock()
		return zero, context.Canceled
	}
	c := g.entries[key]
	if c == nil {
		if g.active >= g.limit {
			g.mu.Unlock()
			return zero, apperror.New(apperror.Unavailable, "并发读取已达上限，请稍后重试")
		}
		workerCtx, cancel := context.WithCancel(g.owner)
		c = &call[T]{ctx: workerCtx, cancel: cancel, done: make(chan struct{})}
		g.entries[key] = c
		g.active++
		g.wg.Add(1)
		go func() {
			defer g.wg.Done()
			func() {
				defer func() {
					if failure := recover(); failure != nil {
						c.err = fmt.Errorf("shared read panic: %v", failure)
					}
				}()
				c.value, c.err = work(workerCtx)
			}()
			g.mu.Lock()
			if g.entries[key] == c {
				delete(g.entries, key)
			}
			g.active--
			close(c.done)
			g.mu.Unlock()
			cancel()
		}()
	} else {
		g.joined++
	}
	c.waiters++
	g.mu.Unlock()
	select {
	case <-ctx.Done():
		g.mu.Lock()
		c.waiters--
		if c.waiters == 0 {
			c.cancel()
			if g.entries[key] == c {
				delete(g.entries, key)
			}
		}
		g.mu.Unlock()
		return zero, ctx.Err()
	case <-c.done:
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		return c.value, c.err
	}
}

func (g *Group[T]) Close(ctx context.Context) error {
	g.mu.Lock()
	g.closed = true
	for _, c := range g.entries {
		c.cancel()
	}
	g.mu.Unlock()
	done := make(chan struct{})
	go func() { g.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (g *Group[T]) Stats() (active int, joined uint64) {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.active, g.joined
}
