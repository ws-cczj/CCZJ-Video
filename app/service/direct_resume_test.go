package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestDirectManifestRoundTripAndValidation(t *testing.T) {
	manifest, err := newDirectDownloadManifest("https://example.test/video.mp4", 17, 3)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Chunks[0].Done = 6
	manifest.Chunks[1].Done = 2
	path := directManifestPath(t.TempDir() + "/video.part")
	if err := saveDirectDownloadManifest(path, manifest); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadDirectDownloadManifest(path, manifest.URL, manifest.Total)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := loaded.downloaded(), int64(8); got != want {
		t.Fatalf("downloaded = %d, want %d", got, want)
	}
	if err := loaded.validate(manifest.URL, manifest.Total); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
}

func TestDirectSingleRestartsWhenRangeIsIgnored(t *testing.T) {
	const payload = "fresh-body"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	tmpPath := t.TempDir() + "/video.part"
	if err := os.WriteFile(tmpPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	task := &downloadTask{httpClient: server.Client(), status: VideoDownloadStatus{Total: int64(len(payload))}}
	app := &App{}
	app.downloadDirectSingle(context.Background(), task, server.URL, tmpPath, int64(len(payload)), int64(len("stale")))

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != payload {
		t.Fatalf("file = %q, want %q", got, payload)
	}
	if status := task.snapshot(); status.Status != "done" || status.Downloaded != int64(len(payload)) {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestDirectParallelWritesOnlyVerifiedRanges(t *testing.T) {
	payload := []byte("0123456789abcdefghijklmnopqrstuvwxyz")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		raw := strings.TrimPrefix(request.Header.Get("Range"), "bytes=")
		limits := strings.Split(raw, "-")
		if len(limits) != 2 {
			http.Error(w, "missing range", http.StatusBadRequest)
			return
		}
		start, _ := strconv.Atoi(limits[0])
		end, _ := strconv.Atoi(limits[1])
		if start < 0 || end < start || end >= len(payload) {
			http.Error(w, "bad range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start : end+1])
	}))
	defer server.Close()

	tmpPath := t.TempDir() + "/video.part"
	task := &downloadTask{httpClient: server.Client()}
	app := &App{}
	app.downloadDirectParallel(context.Background(), task, server.URL, tmpPath, int64(len(payload)), 4)

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(payload) {
		t.Fatalf("parallel file = %q, want %q", data, payload)
	}
	if status := task.snapshot(); status.Status != "done" || status.Downloaded != int64(len(payload)) {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestDirectParallelRejectsIgnoredRange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("full response"))
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	app := &App{}
	app.downloadDirectParallel(context.Background(), task, server.URL, t.TempDir()+"/video.part", 13, 2)
	if status := task.snapshot(); status.Status != "error" || !strings.Contains(status.Error, "range request returned HTTP 200") {
		t.Fatalf("unexpected status: %+v", status)
	}
}
