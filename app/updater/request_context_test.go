package updater

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCancelledUpdateClosesBodyAndDoesNotTryFallbackOrPublishPartial(t *testing.T) {
	started, closed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var fallback atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fallback" {
			fallback.Add(1)
			return
		}
		w.WriteHeader(200)
		_, _ = fmt.Fprint(w, "partial package")
		w.(http.Flusher).Flush()
		close(started)
		select {
		case <-r.Context().Done():
			close(closed)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := downloadFromSources(ctx, dir, "https://github.com/ws-cczj/CCZJ-Video/releases/download/v9.1.0/app.exe", strings.Repeat("a", 64), []speedTestResult{{url: server.URL + "/slow"}, {url: server.URL + "/fallback"}}, server.Client(), nil)
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel did not stop download")
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("download response body remains open")
	}
	if fallback.Load() != 0 {
		t.Fatal("cancellation tried another source")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("cancelled package left staged bytes or digest", entries, err)
	}
}

func TestDirectChecksumSuccessCancelsUnneededProxyAndJoinsItsRequest(t *testing.T) {
	proxyStarted, proxyClosed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	digest := strings.Repeat("a", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/direct" {
			select {
			case <-proxyStarted:
			case <-r.Context().Done():
				return
			}
			fmt.Fprintln(w, digest+"  app.exe")
			return
		}
		close(proxyStarted)
		select {
		case <-r.Context().Done():
			close(proxyClosed)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	sums, err := fetchChecksumChannels(ctx, server.Client(), server.URL+"/direct", []checksumChannel{{party: "github.com"}, {party: "proxy", prefix: server.URL + "/proxy?url="}})
	if err != nil || sums["app.exe"] != digest {
		t.Fatal(sums, err)
	}
	select {
	case <-proxyClosed:
	case <-ctx.Done():
		t.Fatal("trusted direct result left proxy work running")
	}
}

func TestCancellingChecksumStageStopsAllActiveChannels(t *testing.T) {
	started, closed := make(chan struct{}, 2), make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		select {
		case <-r.Context().Done():
			closed <- struct{}{}
		case <-release:
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := fetchChecksumChannels(ctx, server.Client(), server.URL+"/direct", []checksumChannel{{party: "github.com"}, {party: "proxy", prefix: server.URL + "/proxy?url="}})
		done <- err
	}()
	for range 2 {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("checksum request did not start")
		}
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("checksum batch did not drain")
	}
	for range 2 {
		select {
		case <-closed:
		case <-time.After(time.Second):
			t.Fatal("checksum body remains open")
		}
	}
}
