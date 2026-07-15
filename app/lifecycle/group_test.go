package lifecycle

import (
	"context"
	"testing"
	"time"
)

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
