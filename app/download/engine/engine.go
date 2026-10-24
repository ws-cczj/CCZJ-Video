// Package engine owns download admission, execution, checkpoints and output files.
// The parent download package holds the contracts used by storage adapters; it
// deliberately does not import this implementation or its HTTP dependencies.
package engine

import (
	"context"
	"sync"

	"cczjVideo/app/apperror"
	downloadservice "cczjVideo/app/download"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netbudget"
)

// Options supplies host-owned dependencies. Progress preserves the task snapshot
// contract without coupling the engine to Wails or application mode state.
type Options struct {
	DataDirectory func() string
	Directory     *downloadservice.Directory
	Store         downloadservice.CheckpointStore
	Budget        *netbudget.Budget
	Progress      func(Status)
}

// Engine owns every accepted attempt until its HTTP, file and checkpoint work
// has finished. A start request's return or cancellation does not own that attempt.
type Engine struct {
	mu         sync.Mutex
	tasks      *downloadservice.Registry[downloadTask]
	store      downloadservice.CheckpointStore
	restoreErr error
	restored   bool
	executor   *downloadservice.Executor
	paths      map[string]*downloadservice.Run
	directory  *downloadservice.Directory
	dataDir    func() string
	budget     *netbudget.Budget
	owner      *lifecycle.Group
	progress   func(Status)
}

// New assembles a download owner. Restore must be called after the host opens
// storage; restored transfers stay paused until an explicit Resume.
func New(parent context.Context, options Options) *Engine {
	owner := lifecycle.NewGroupWithContext(parent)
	return &Engine{
		tasks: downloadservice.NewRegistry[downloadTask](), store: options.Store,
		executor: downloadservice.NewExecutor(owner.Context(), owner.Go, 2, 64),
		paths:    make(map[string]*downloadservice.Run), directory: options.Directory,
		dataDir: options.DataDirectory, budget: options.Budget, owner: owner, progress: options.Progress,
	}
}

// Restore atomically validates and publishes persisted tasks once. A restore
// failure remains visible to later calls instead of presenting an empty list.
func (a *Engine) Restore(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.restored {
		return a.restoreErr
	}
	if a.store == nil || a.dataDir == nil {
		return apperror.New(apperror.Unavailable, "下载存储尚未就绪")
	}
	a.restoreErr = a.loadPersistedTasks(ctx)
	a.restored = true
	return a.restoreErr
}

// PauseAll persists pause intent and joins active attempts before a mode switch.
func (a *Engine) PauseAll(ctx context.Context) error {
	items, err := a.List()
	if err != nil {
		return err
	}
	for _, item := range items {
		if item.Status == "queued" || item.Status == "downloading" {
			if _, err := a.Pause(ctx, item.TaskId); err != nil {
				return err
			}
		}
	}
	return nil
}

// Stats returns bounded admission statistics, not the persisted task history.
func (a *Engine) Stats() downloadservice.ExecutorStats { return a.executor.Stats() }

// Close cancels attempts and waits until none can access storage or output files.
func (a *Engine) Close(ctx context.Context) error {
	if !a.owner.StopContext(ctx) {
		return apperror.Wrap(apperror.Timeout, ctx.Err(), "下载任务尚未停止")
	}
	return nil
}
