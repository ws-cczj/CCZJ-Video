package updater

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type checkCancellationTransport func(*http.Request) (*http.Response, error)

func (f checkCancellationTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCancelledUpdateCheckDoesNotVisitRetryOrFallbackSources(t *testing.T) {
	old := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var calls atomic.Int32
	http.DefaultTransport = checkCancellationTransport(func(request *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-request.Context().Done()
		return nil, request.Context().Err()
	})
	done := make(chan error, 1)
	go func() { _, err := CheckUpdateContext(ctx); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("update check continued after cancellation")
	}
	if calls.Load() != 1 {
		t.Fatal("cancelled update check visited other sources", calls.Load())
	}
}
