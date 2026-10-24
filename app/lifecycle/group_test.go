package lifecycle

import (
	"context"
	"testing"
	"time"
)

func TestGroupStopReportsIncompleteDrain(t *testing.T) {
	group := NewGroup()
	release := make(chan struct{})
	group.Go("uncooperative", func(context.Context) { <-release })
	if group.Stop(time.Millisecond) {
		t.Fatal("incomplete drain reported success")
	}
	close(release)
	if !group.Stop(time.Second) {
		t.Fatal("completed drain reported failure")
	}
}

func TestGroupRejectsTasksAfterStop(t *testing.T) {
	group := NewGroup()
	started := make(chan struct{})
	if !group.Go("blocking", func(ctx context.Context) {
		close(started)
		<-ctx.Done()
	}) {
		t.Fatal("expected task to start")
	}
	<-started
	group.Stop(time.Second)
	if group.Go("late", func(context.Context) {}) {
		t.Fatal("expected task submitted after Stop to be rejected")
	}
}

func TestGroupStopIsIdempotent(t *testing.T) {
	group := NewGroup()
	group.Stop(time.Second)
	group.Stop(time.Second)
}

func TestChildOwnerStopsWithParentAndRejectsLateWork(t *testing.T) {
	parent := NewGroup()
	child := NewGroupWithContext(parent.Context())
	started, finished := make(chan struct{}), make(chan struct{})
	child.Go("child I/O", func(ctx context.Context) {
		close(started)
		<-ctx.Done()
		close(finished)
	})
	<-started
	parent.Cancel()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("child did not follow parent cancellation")
	}
	if child.Go("late", func(context.Context) { t.Error("late work executed") }) {
		t.Fatal("cancelled parent still admitted child work")
	}
	if release, accepted := child.Track("late request"); accepted {
		release()
		t.Fatal("cancelled parent still admitted a tracked request")
	}
	if !child.Stop(time.Second) || !parent.Stop(time.Second) {
		t.Fatal("owner did not drain")
	}
}

func TestTrackedRequestsDrainAndCannotReenterStoppedOwner(t *testing.T) {
	group := NewGroup()
	release, ok := group.Track("request")
	if !ok || group.ActiveTasks()["request"] != 1 {
		t.Fatal("request was not tracked")
	}
	if group.Stop(time.Millisecond) {
		t.Fatal("reported drained while request still owns resources")
	}
	if _, ok := group.Track("late"); ok {
		t.Fatal("stopped owner accepted request")
	}
	release()
	release()
	if !group.Stop(time.Second) || len(group.ActiveTasks()) != 0 {
		t.Fatal("request release did not drain")
	}
}
