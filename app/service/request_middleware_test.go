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

func TestVideoTaskAdmissionStaysInsideItsModeEpoch(t *testing.T) {
	group := lifecycle.NewGroup()
	t.Cleanup(func() { group.Stop(time.Second) })
	a := &App{background: group}
	_, err := videoAdmission(a, context.Background(), "video.test", func(ctx context.Context) (bool, error) {
		if a.modeSwitchMu.TryLock() {
			a.modeSwitchMu.Unlock()
			t.Fatal("mode switch can pass a pending task acceptance")
		}
		if ctx.Err() != nil {
			t.Fatal(ctx.Err())
		}
		return true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !a.modeSwitchMu.TryLock() {
		t.Fatal("admission retained its mode lock")
	}
	a.mode = "comic"
	a.modeSwitchMu.Unlock()
	called := false
	_, err = videoAdmission(a, context.Background(), "video.test", func(context.Context) (bool, error) { called = true; return true, nil })
	if called || !apperror.Is(err, apperror.Conflict) {
		t.Fatal("video work accepted in reading mode", called, err)
	}
}

func TestHostRequestSurvivesModeExitAndDrainsWithApplication(t *testing.T) {
	appOwner, modeOwner := lifecycle.NewGroup(), lifecycle.NewGroup()
	defer appOwner.Stop(time.Second)
	defer modeOwner.Stop(time.Second)
	a := &App{background: appOwner, modeTasks: modeOwner, mode: "novel"}
	started := make(chan context.Context, 1)
	done := make(chan error, 1)
	go func() {
		_, err := runHostRequest(a, context.Background(), "app.DownloadUpdate", func(ctx context.Context) (bool, error) { started <- ctx; <-ctx.Done(); return false, ctx.Err() })
		done <- err
	}()
	request := <-started
	if !modeOwner.Stop(time.Second) || request.Err() != nil {
		t.Fatal("mode exit cancelled host work")
	}
	if !appOwner.Stop(time.Second) {
		t.Fatal("application did not drain its host request")
	}
	if err := <-done; !apperror.Is(err, apperror.Cancelled) {
		t.Fatal(err)
	}
}
