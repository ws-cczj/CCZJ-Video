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
	started := make(chan struct{}, 3)
	var calls atomic.Int32
	var settled atomic.Int32
	http.DefaultTransport = checkCancellationTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		started <- struct{}{}
		<-request.Context().Done()
		settled.Add(1)
		return nil, request.Context().Err()
	})
	done := make(chan error, 1)
	go func() { _, err := CheckUpdateContext(ctx); done <- err }()
	for range 3 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("metadata batch did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("update check continued after cancellation")
	}
	if calls.Load() != 3 || settled.Load() != 3 {
		t.Fatal("cancelled update check visited other sources", calls.Load())
	}
}
