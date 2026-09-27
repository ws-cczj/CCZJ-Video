package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestHLSDownloadWritesOrderedSegmentsWithBoundedWorkers(t *testing.T) {
	const segments = 12
	var active atomic.Int32
	var maxActive atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".ts"))
		if err != nil {
			http.Error(w, "bad segment", http.StatusBadRequest)
			return
		}
		current := active.Add(1)
		for {
			maximum := maxActive.Load()
			if current <= maximum || maxActive.CompareAndSwap(maximum, current) {
				break
			}
		}
		defer active.Add(-1)
		// Earlier segments complete later, forcing the downloader to buffer and
		// flush results in playlist order.
		time.Sleep(time.Duration(segments-index) * 5 * time.Millisecond)
		_, _ = io.WriteString(w, strconv.Itoa(index)+"|")
	}))
	defer server.Close()

	urls := make([]string, segments)
	for i := range urls {
		urls[i] = server.URL + "/" + strconv.Itoa(i) + ".ts"
	}
	task := &downloadTask{httpClient: server.Client(), segments: urls}
	savePath := filepath.Join(t.TempDir(), "ordered.ts")
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", savePath)

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	var want strings.Builder
	for i := 0; i < segments; i++ {
		want.WriteString(strconv.Itoa(i))
		want.WriteByte('|')
	}
	if got := string(data); got != want.String() {
		t.Fatalf("ordered output = %q, want %q", got, want.String())
	}
	if got := maxActive.Load(); got > 5 {
		t.Fatalf("in-flight segment requests = %d, want at most 5 workers", got)
	}
	if status := task.snapshot(); status.Status != "done" || status.Downloaded != int64(len(want.String())) {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHLSDownloadCancelsInFlightRequests(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	task := &downloadTask{httpClient: server.Client(), segments: []string{server.URL + "/segment.ts"}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		(&App{}).downloadM3u8(ctx, task, server.URL+"/playlist.m3u8", filepath.Join(t.TempDir(), "cancelled.ts"))
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("segment request did not start")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("HLS download did not stop after cancellation")
	}
	if status := task.snapshot(); status.Status != "cancelled" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHLSDownloadRejectsOversizedSegmentBeforeReadingBody(t *testing.T) {
	const limit = 64 << 20
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(limit+1))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client(), segments: []string{server.URL + "/oversized.ts"}}
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", filepath.Join(t.TempDir(), "oversized.ts"))
	if status := task.snapshot(); status.Status != "error" || !strings.Contains(status.Error, "exceeds size limit") {
		t.Fatalf("unexpected status: %+v", status)
	}
}
