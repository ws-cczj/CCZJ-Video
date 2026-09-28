package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
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
	app.downloadDirectSingle(context.Background(), task, server.URL, tmpPath, int64(len(payload)), int64(len("stale")), "")

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
	app.downloadDirectParallel(context.Background(), task, server.URL, tmpPath, int64(len(payload)), 4, "")

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
	app.downloadDirectParallel(context.Background(), task, server.URL, t.TempDir()+"/video.part", 13, 2, "")
	if status := task.snapshot(); status.Status != "error" || !strings.Contains(status.Error, "range request returned HTTP 200") {
		t.Fatalf("unexpected status: %+v", status)
	}
}

// CDN 掐掉长连接是常态：单连接这一轮只写下了一半，也要能拿着 .part 的尾部自己接上，
// 而不是把一次抖动报成失败、等用户手动点续传。
func TestDirectDownloadResumesAfterTruncatedResponse(t *testing.T) {
	payload := []byte(strings.Repeat("abcdefghij", 20))
	const cut = 80
	if len(payload) != 200 {
		t.Fatalf("fixture payload = %d, want 200", len(payload))
	}
	var fullAttempts, resumeAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Range", fmt.Sprintf("bytes 0-0/%d", len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			return
		}
		rangeHeader := r.Header.Get("Range")
		if rangeHeader == "" {
			fullAttempts.Add(1)
			conn, writer, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = writer.WriteString(fmt.Sprintf("HTTP/1.1 200 OK\r\nContent-Length: %d\r\n\r\n", len(payload)))
			_, _ = writer.Write(payload[:cut])
			_ = writer.Flush()
			_ = conn.Close()
			return
		}
		resumeAttempts.Add(1)
		start, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(rangeHeader, "bytes="), "-"), 10, 64)
		if err != nil {
			http.Error(w, "bad range", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, len(payload)-1, len(payload)))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[start:])
	}))
	defer server.Close()

	dir := t.TempDir()
	savePath := dir + "/movie.bin"
	task := &downloadTask{httpClient: server.Client(), status: VideoDownloadStatus{Url: server.URL}}
	(&App{}).downloadDirect(context.Background(), task, server.URL+"/movie.bin", savePath)

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("download did not finish: %+v (%v)", task.snapshot(), err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("file = %q, want the whole payload", data)
	}
	if fullAttempts.Load() != 1 || resumeAttempts.Load() != 1 {
		t.Fatalf("attempts = full %d, resumed %d, want 1 and 1", fullAttempts.Load(), resumeAttempts.Load())
	}
	if status := task.snapshot(); status.Status != "done" || status.Downloaded != int64(len(payload)) {
		t.Fatalf("unexpected status: %+v", status)
	}
}

// 上游不认这个地址（404）时不该重试：字节一步都没前进，重试四轮只是让用户多等三轮退避。
func TestDirectDownloadDoesNotRetryPermanentFailure(t *testing.T) {
	var getAttempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			getAttempts.Add(1)
			http.Error(w, "gone", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Range", "bytes 0-0/4096")
		w.WriteHeader(http.StatusPartialContent)
	}))
	defer server.Close()

	dir := t.TempDir()
	task := &downloadTask{httpClient: server.Client(), status: VideoDownloadStatus{Url: server.URL}}
	(&App{}).downloadDirect(context.Background(), task, server.URL+"/movie.bin", dir+"/movie.bin")

	status := task.snapshot()
	if status.Status != "error" || !strings.Contains(status.Error, "HTTP 404") {
		t.Fatalf("unexpected status: %+v", status)
	}
	if got := getAttempts.Load(); got != 1 {
		t.Fatalf("GET attempts = %d, want 1", got)
	}
}
