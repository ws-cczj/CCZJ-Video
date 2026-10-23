package netbudget

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/apperror"
)

type tripFunc func(*http.Request) (*http.Response, error)

func (f tripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestBudgetKeepsPermitUntilBodyAndReservesPlayback(t *testing.T) {
	b := New(context.Background(), Policy{Total: 2, PerHost: 2, PlaybackReserve: 1, HostPlaybackReserve: 1, Waiting: 1})
	base := tripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("ok")), Header: make(http.Header)}, nil
	})
	req, _ := http.NewRequest("GET", "https://same.test/a", nil)
	background := b.Wrap("images", base)
	playback := b.Wrap("playback", base)
	first, err := background.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().Active != 1 {
		t.Fatal("permit released at response headers")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	waiting := make(chan error, 1)
	go func() { _, err := background.RoundTrip(req.WithContext(ctx)); waiting <- err }()
	deadline := time.Now().Add(time.Second)
	for b.Snapshot().Waiting != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if b.Snapshot().Waiting != 1 {
		t.Fatal("request did not wait")
	}
	if _, err := background.RoundTrip(req); !apperror.Is(err, apperror.Unavailable) {
		t.Fatalf("overflow = %v", err)
	}
	video, err := playback.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	if b.Snapshot().Active != 2 {
		t.Fatal("reserved playback slot unavailable")
	}
	video.Body.Close()
	cancel()
	if err := <-waiting; err != context.Canceled {
		t.Fatalf("waiting cancellation = %v", err)
	}
	first.Body.Close()
	first.Body.Close()
	if s := b.Snapshot(); s.Active != 0 || s.Waiting != 0 || s.Rejected != 1 {
		t.Fatalf("leaked budget: %+v", s)
	}
}

func TestBudgetShutdownReleasesWaitingRequestAndErrorsReleaseActive(t *testing.T) {
	owner, stop := context.WithCancel(context.Background())
	defer stop()
	b := New(owner, Policy{Total: 1, PerHost: 1, Waiting: 1})
	var calls atomic.Int32
	base := tripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	transport := b.Wrap("reading", base)
	req, _ := http.NewRequest("GET", "https://example.test", nil)
	done := make(chan error, 2)
	go func() { _, err := transport.RoundTrip(req); done <- err }()
	deadline := time.Now().Add(time.Second)
	for calls.Load() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	go func() { _, err := transport.RoundTrip(req); done <- err }()
	stop()
	for range 2 {
		select {
		case err := <-done:
			if err != context.Canceled {
				t.Fatalf("shutdown error = %v", err)
			}
		case <-time.After(time.Second):
			t.Fatal("shutdown did not cancel network")
		}
	}
	if s := b.Snapshot(); s.Active != 0 || s.Waiting != 0 {
		t.Fatalf("shutdown leaked: %+v", s)
	}
}
