package update

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/updater"
)

type controlledDownload func(context.Context, string, updater.DownloadProgress) (string, error)

func (download controlledDownload) DownloadContext(ctx context.Context, url string, progress updater.DownloadProgress) (string, error) {
	return download(ctx, url, progress)
}

func TestUpdateDownloadHasOneOwnerAndCanRestartAfterCancellation(t *testing.T) {
	started := make(chan struct{})
	var calls, events atomic.Int32
	download := controlledDownload(func(ctx context.Context, url string, progress updater.DownloadProgress) (string, error) {
		if calls.Add(1) > 1 {
			return "verified-package", nil
		}
		progress(1, 100, 1)
		close(started)
		<-ctx.Done()
		progress(2, 100, 1)
		return "", ctx.Err()
	})
	s := NewService(nil, func(string, any) { events.Add(1) }, download)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.DownloadContext(ctx, "first"); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	if _, err := s.DownloadContext(context.Background(), "second"); apperror.CodeOf(err) != apperror.Conflict {
		t.Fatal("second download can interleave progress", err)
	}
	if calls.Load() != 1 {
		t.Fatal("conflicting download entered package I/O")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not release admission")
	}
	if events.Load() != 1 {
		t.Fatal("cancelled download emitted late progress")
	}
	if path, err := s.DownloadContext(context.Background(), "retry"); err != nil || path != "verified-package" {
		t.Fatal(path, err)
	}
}
