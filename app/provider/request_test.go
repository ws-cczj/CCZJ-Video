package provider

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cczjVideo/app/apperror"
)

func TestHTTPFailuresExposeActionableMetadata(t *testing.T) {
	for _, tt := range []struct {
		status int
		code   apperror.Code
	}{
		{429, apperror.RateLimited}, {401, apperror.AuthenticationRequired}, {403, apperror.PermissionDenied},
	} {
		t.Run(string(tt.code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "15")
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			_, err := NewFetcher().Fetch(context.Background(), &Document{}, &Operation{Method: "GET"}, server.URL, Target{})
			response := apperror.Public(err)
			if response.Code != tt.code {
				t.Fatalf("HTTP %d: %v", tt.status, err)
			}
			if tt.status == 429 && response.RetryAfterSeconds != 15 {
				t.Fatalf("retry metadata: %#v", response)
			}
		})
	}
}

func TestFetchCancellationReachesUpstream(t *testing.T) {
	started, stopped := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(stopped)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := NewFetcher().Fetch(ctx, &Document{}, &Operation{Method: "GET"}, server.URL, Target{})
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request never reached source")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) || apperror.CodeOf(err) != apperror.Cancelled {
			t.Fatalf("cancellation lost: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("fetch did not stop")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("upstream request remains active")
	}
}
