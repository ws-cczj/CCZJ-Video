package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/operation"
	"context"
	"testing"
	"time"
)

func TestRequestMiddlewareStopsWithModeAndReleasesOwnership(t *testing.T) {
	group := lifecycle.NewGroup()
	a := &App{background: group, operations: &operation.Metrics{}}
	started, done := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := runRequest(a, context.Background(), "test.read", func(ctx context.Context) (bool, error) { close(started); <-ctx.Done(); return false, ctx.Err() })
		done <- err
	}()
	<-started
	if !group.Stop(time.Second) {
		t.Fatal("request ownership failed to drain")
	}
	if err := <-done; !apperror.Is(err, apperror.Cancelled) {
		t.Fatal(err)
	}
	rows := a.operations.Snapshot("video")
	if len(rows) != 1 || rows[0].Cancelled != 1 {
		t.Fatal(rows)
	}
	called := false
	_, err := runRequest(a, context.Background(), "test.late", func(context.Context) (bool, error) { called = true; return true, nil })
	if called || !apperror.Is(err, apperror.Cancelled) {
		t.Fatal("late work started", called, err)
	}
}
