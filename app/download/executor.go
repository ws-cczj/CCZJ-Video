package download

import (
	"context"
	"errors"
)

// ErrQueueFull reports bounded admission failure before a new goroutine starts.
var ErrQueueFull = errors.New("download queue is full")

// Executor limits both active downloads and accepted waiting work. The supplied
// launcher keeps every accepted run owned by the application's lifecycle.
type Executor struct {
	owner    context.Context
	launch   func(string, func(context.Context)) bool
	slots    chan struct{}
	accepted chan struct{}
}

// Run represents exactly one execution attempt, including time spent queued.
type Run struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// ExecutorStats describes admission, not persisted task history. Channel lengths
// are point-in-time observations; admission and execution may change between reads.
type ExecutorStats struct {
	Active           int `json:"active"`
	Accepted         int `json:"accepted"`
	ConcurrencyLimit int `json:"concurrency_limit"`
	AdmissionLimit   int `json:"admission_limit"`
}

func (e *Executor) Stats() ExecutorStats {
	return ExecutorStats{Active: len(e.slots), Accepted: len(e.accepted), ConcurrencyLimit: cap(e.slots), AdmissionLimit: cap(e.accepted)}
}

// NewExecutor creates an instance-local executor with explicit resource budgets.
func NewExecutor(owner context.Context, launch func(string, func(context.Context)) bool, concurrent, waiting int) *Executor {
	if concurrent < 1 {
		concurrent = 1
	}
	if waiting < 0 {
		waiting = 0
	}
	return &Executor{owner: owner, launch: launch, slots: make(chan struct{}, concurrent), accepted: make(chan struct{}, concurrent+waiting)}
}

// Submit either rejects immediately or returns an attempt whose Done is closed
// after callback cleanup. Cancellation never starts work that is still queued.
func (e *Executor) Submit(name string, work func(context.Context)) (*Run, error) {
	if err := e.owner.Err(); err != nil {
		return nil, err
	}
	select {
	case e.accepted <- struct{}{}:
	default:
		return nil, ErrQueueFull
	}
	ctx, cancel := context.WithCancel(e.owner)
	run := &Run{cancel: cancel, done: make(chan struct{})}
	if !e.launch(name, func(context.Context) {
		defer close(run.done)
		defer cancel()
		defer func() { <-e.accepted }()
		select {
		case e.slots <- struct{}{}:
		case <-ctx.Done():
			return
		}
		defer func() { <-e.slots }()
		if ctx.Err() == nil {
			work(ctx)
		}
	}) {
		<-e.accepted
		cancel()
		close(run.done)
		return nil, context.Canceled
	}
	return run, nil
}

// Cancel stops this attempt; Wait can be used before reusing its output files.
func (r *Run) Cancel() {
	if r != nil {
		r.cancel()
	}
}

// Done signals that this attempt can no longer touch its output or checkpoint.
func (r *Run) Done() <-chan struct{} { return r.done }

// Wait waits for cleanup without tying the attempt's lifetime to the waiter.
func (r *Run) Wait(ctx context.Context) error {
	if r == nil {
		return nil
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
