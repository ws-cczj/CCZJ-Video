package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

type cleanupTransport struct {
	base   http.RoundTripper
	closes atomic.Int32
}

func (t *cleanupTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return t.base.RoundTrip(req)
}
func (t *cleanupTransport) CloseIdleConnections() {
	t.closes.Add(1)
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func TestCompletedDownloadReleasesItsPrivateTransport(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		if r.Method != "HEAD" {
			_, _ = fmt.Fprint(w, "ok")
		}
	}))
	defer server.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	transport := &cleanupTransport{base: base}
	task := &downloadTask{httpClient: &http.Client{Transport: transport}, status: VideoDownloadStatus{TaskId: "fixture", Url: server.URL, SavePath: filepath.Join(t.TempDir(), "video.bin")}}
	app := newTransportTestApp()
	app.runDownload(context.Background(), task)
	if task.snapshot().Status != "done" {
		t.Fatalf("download fixture failed: %+v", task.snapshot())
	}
	if transport.closes.Load() == 0 {
		t.Fatal("completed download retained its private connection pool")
	}
}
