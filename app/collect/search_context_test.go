package collect

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

type cancelledSearchTransport func(*http.Request) (*http.Response, error)

func (f cancelledSearchTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSearchCancellationStopsIOAndDoesNotTryWildcard(t *testing.T) {
	old := client
	t.Cleanup(func() { client = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	var calls atomic.Int32
	client = &http.Client{Transport: cancelledSearchTransport(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	done := make(chan error, 1)
	go func() {
		_, err := FetchSearchPageContext(ctx, CreateStrategy("https://example.test/api"), "query", 1)
		done <- err
	}()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("search I/O ignored cancellation")
	}
	if calls.Load() != 1 {
		t.Fatal("cancelled search issued fallback requests", calls.Load())
	}
}
