// Package lifecycle coordinates application-scoped background work.
package lifecycle

import (
	"cczjVideo/app/applog"
	"context"
	"sync"
	"time"
)

// Group owns cancellation and bounded waiting for application background work.
type Group struct {
	ctx      context.Context
	cancel   context.CancelFunc
	wg       sync.WaitGroup
	mu       sync.Mutex
	stopped  bool
	stopOnce sync.Once
	done     chan struct{}
	tasks    map[string]int
}

// NewGroup creates a background task group.
func NewGroup() *Group {
	ctx, cancel := context.WithCancel(context.Background())
	return &Group{ctx: ctx, cancel: cancel, done: make(chan struct{}), tasks: make(map[string]int)}
}

// Context returns the cancellation context shared by application tasks.
func (g *Group) Context() context.Context {
	return g.ctx
}

// ActiveTasks returns a point-in-time diagnostic snapshot. The returned map is
// detached from the group so callers can safely include it in startup and
// shutdown telemetry without holding the lifecycle lock while logging.
func (g *Group) ActiveTasks() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()
	result := make(map[string]int, len(g.tasks))
	for name, count := range g.tasks {
		result[name] = count
	}
	return result
}

// Go starts a panic-protected application background task. It returns false
// when shutdown has started, ensuring WaitGroup.Add never races with Wait.
func (g *Group) Go(name string, fn func(context.Context)) bool {
	g.mu.Lock()
	if g.stopped {
		g.mu.Unlock()
		applog.Warn("background task %s was rejected during shutdown", name)
		return false
	}
	g.wg.Add(1)
	g.tasks[name]++
	g.mu.Unlock()
	go func() {
		defer func() {
			g.mu.Lock()
			g.tasks[name]--
			if g.tasks[name] == 0 {
				delete(g.tasks, name)
			}
			g.mu.Unlock()
			g.wg.Done()
		}()
		defer func() {
			if recovered := recover(); recovered != nil {
				applog.Error("[PANIC] background task %s crashed: %v", name, recovered)
			}
		}()
		fn(g.ctx)
	}()
	return true
}

// Stop cancels all tasks and waits up to timeout for their completion.
func (g *Group) Stop(timeout time.Duration) {
	g.stopOnce.Do(func() {
		g.mu.Lock()
		g.stopped = true
		g.mu.Unlock()
		g.cancel()
		go func() {
			g.wg.Wait()
			close(g.done)
		}()
	})
	select {
	case <-g.done:
	case <-time.After(timeout):
		g.mu.Lock()
		pending := make([]string, 0, len(g.tasks))
		for name := range g.tasks {
			pending = append(pending, name)
		}
		g.mu.Unlock()
		applog.Warn("background tasks did not stop within %s: %v", timeout, pending)
	}
}
