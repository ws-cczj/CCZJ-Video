package service

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	downloadservice "cczjVideo/app/download"
)

// 走哪个分支只看一件事：带 manifest 的 .part 是稀疏文件，剩余量再小也不能交给单连接。
// 单连接拿文件大小当连续前缀去 O_APPEND，空洞留在中间，补齐字节数后照样判定成功。
func TestParallelManifestKeepsTheSparseFileOffTheAppendPath(t *testing.T) {
	if !useParallelDirectDownload(true, true, 0) {
		t.Fatal("a manifest-backed sparse .part must never fall into the append path")
	}
	// 上游不认 Range 时并行分支收不到 206，无从校验分片；这条走整条重来，不是追加。
	if useParallelDirectDownload(false, true, 10<<20) {
		t.Fatal("without range support a parallel write cannot be verified")
	}
	if useParallelDirectDownload(true, false, minParallelSize) {
		t.Fatal("small files are cheaper on a single connection")
	}
	if !useParallelDirectDownload(true, false, minParallelSize+1) {
		t.Fatal("large files should be split across connections")
	}
}

// 移除任务记录承诺"不删除已下载文件"，manifest 就得跟 .part 一起留在原地。单独删掉它，
// 剩下的稀疏文件只有大小可看：下一次同链接下载从尾部追加，中间的空洞永远没人补。
func TestRemoveDownloadKeepsSparsePartWithItsManifest(t *testing.T) {
	root := t.TempDir()
	t.Setenv("APPDATA", root)
	dir := filepath.Join(root, "CCZJ Video")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	const url = "https://cdn.example.test/movie.bin"
	savePath := filepath.Join(dir, "movie.bin")
	tmpPath := savePath + ".part"

	manifest, err := newDirectDownloadManifest(url, 200, 2)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Chunks[0].Done = 40 // 第一片只下到 40 字节
	manifest.Chunks[1].Done = 60 // 第二片写到 160 字节，中间 40-100 是空洞
	if err := saveDirectDownloadManifest(directManifestPath(tmpPath), manifest); err != nil {
		t.Fatal(err)
	}
	sparse := make([]byte, 160)
	copy(sparse[:40], bytes.Repeat([]byte("A"), 40))
	copy(sparse[100:], bytes.Repeat([]byte("B"), 60))
	if err := os.WriteFile(tmpPath, sparse, 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{downloads: downloadservice.NewRegistry[downloadTask]()}
	app.downloads.Add("t1", &downloadTask{
		status: VideoDownloadStatus{TaskId: "t1", Url: url, SavePath: savePath, Status: "paused"},
	})
	if !app.RemoveDownload("t1") {
		t.Fatal("RemoveDownload reported failure")
	}
	for _, p := range []string{tmpPath, directManifestPath(tmpPath)} {
		if _, statErr := os.Stat(p); statErr != nil {
			t.Fatalf("%s went away with the record: %v", filepath.Base(p), statErr)
		}
	}
	// 进度按分片完成量算，不是文件大小：两者差的正好是那段空洞。
	loaded, err := loadDirectDownloadManifest(directManifestPath(tmpPath), url, 200)
	if err != nil {
		t.Fatalf("manifest no longer matches the request: %v", err)
	}
	if got := loaded.downloaded(); got != 100 {
		t.Fatalf("manifest downloaded = %d, want 100 (file size 160 counts the hole)", got)
	}
}

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
// 上一轮用并行下过一半，这一轮换到的节点不认 Range 了。稀疏 .part 的中间是有洞的，
// 拿文件大小当续传点追加会补出一个"字节数刚好、内容全错"的成品，所以必须从头整条重来。
func TestDirectDownloadRestartsWhenRangeSupportDisappears(t *testing.T) {
	payload := bytes.Repeat([]byte("CCZJ-VIDEO"), 40) // 400 字节
	var rangedGets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			// 探测拿到 200 + 完整长度：size 已知，但不支持 Range。
			w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
			w.WriteHeader(http.StatusOK)
			return
		}
		if r.Header.Get("Range") != "" {
			rangedGets.Add(1)
			http.Error(w, "range unsupported", http.StatusNotImplemented)
			return
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	dir := t.TempDir()
	savePath := filepath.Join(dir, "movie.bin")
	urlStr := server.URL + "/movie.bin"
	manifest, err := newDirectDownloadManifest(urlStr, int64(len(payload)), 2)
	if err != nil {
		t.Fatal(err)
	}
	manifest.Chunks[0].Done = 100
	manifest.Chunks[1].Done = 100
	if err := saveDirectDownloadManifest(directManifestPath(savePath+".part"), manifest); err != nil {
		t.Fatal(err)
	}
	holey := make([]byte, 300) // 文件大小看着像下了 300 字节，实际中段是空洞
	copy(holey[:100], bytes.Repeat([]byte("X"), 100))
	copy(holey[200:], bytes.Repeat([]byte("Y"), 100))
	if err := os.WriteFile(savePath+".part", holey, 0644); err != nil {
		t.Fatal(err)
	}

	task := &downloadTask{httpClient: server.Client(), status: VideoDownloadStatus{
		Url: urlStr,
		// 上一轮并行下报过 6 段进度，这一轮换成单连接：这些条必须跟着作废。
		Chunks: []ChunkProgress{{ID: 0, Start: 0, End: 199, Done: 100}},
	}}
	(&App{}).downloadDirect(context.Background(), task, urlStr, savePath)

	if got := task.snapshot(); len(got.Chunks) != 0 {
		t.Fatalf("单连接下载还带着作废的分片进度: %+v", got.Chunks)
	}

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("download did not finish: %+v (%v)", task.snapshot(), err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("finished file kept the sparse leftovers: %d bytes, first 8 = %q", len(data), data[:8])
	}
	if _, statErr := os.Stat(directManifestPath(savePath + ".part")); !os.IsNotExist(statErr) {
		t.Fatalf("manifest outlived the download: %v", statErr)
	}
	if got := rangedGets.Load(); got != 0 {
		t.Fatalf("ranged GETs = %d, want 0：续传点已经作废，不该再发 Range", got)
	}
}

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
