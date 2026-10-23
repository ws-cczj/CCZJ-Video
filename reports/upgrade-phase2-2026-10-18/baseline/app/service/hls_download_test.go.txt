package service

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	downloadservice "cczjVideo/app/download"
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

func TestHLSDownloadDecryptsAES128SegmentsWithSequenceIV(t *testing.T) {
	const segmentCount = 3
	key := []byte("0123456789abcdef")
	plain := make([][]byte, segmentCount)
	ciphered := make([][]byte, segmentCount)
	for i := 0; i < segmentCount; i++ {
		plain[i] = []byte(fmt.Sprintf("segment-%d-payload", i))
		block, err := aes.NewCipher(key)
		if err != nil {
			t.Fatal(err)
		}
		iv := downloadservice.SegmentIV(nil, int64(i))
		ciphered[i] = encryptCBC(block, iv, padPKCS7(plain[i]))
	}

	var keyRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/playlist.m3u8":
			lines := []string{"#EXTM3U", `#EXT-X-KEY:METHOD=AES-128,URI="key"`}
			for i := 0; i < segmentCount; i++ {
				lines = append(lines, "#EXTINF:4,", fmt.Sprintf("seg-%d.ts", i))
			}
			w.Header().Set("Content-Type", "application/vnd.m3u8+xml")
			_, _ = io.WriteString(w, strings.Join(lines, "\n")+"\n")
		case r.URL.Path == "/key":
			keyRequests.Add(1)
			_, _ = w.Write(key)
		default:
			index, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/seg-"), ".ts"))
			if err != nil || index < 0 || index >= segmentCount {
				http.Error(w, "bad segment", http.StatusBadRequest)
				return
			}
			_, _ = w.Write(ciphered[index])
		}
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	savePath := filepath.Join(t.TempDir(), "decrypted.ts")
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", savePath)

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	var want []byte
	for _, part := range plain {
		want = append(want, part...)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("file = %q, want %q", data, want)
	}
	if got := keyRequests.Load(); got != 1 {
		t.Fatalf("key requests = %d, want 1 (one key per playlist, cached across segments)", got)
	}
	if status := task.snapshot(); status.Status != "done" || status.Downloaded != int64(len(want)) {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHLSDownloadSplicesByteRangeSegments(t *testing.T) {
	payload := []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ")
	content := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-BYTERANGE:10@0",
		"#EXTINF:1,",
		"big.bin",
		"#EXT-X-BYTERANGE:10@10",
		"#EXTINF:1,",
		"big.bin",
		"#EXT-X-BYTERANGE:10",
		"#EXTINF:1,",
		"big.bin",
	}, "\n")

	var mu sync.Mutex
	var ranges []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			_, _ = io.WriteString(w, content+"\n")
			return
		case "/big.bin":
			mu.Lock()
			ranges = append(ranges, r.Header.Get("Range"))
			mu.Unlock()
			rangeHeader := r.Header.Get("Range")
			if rangeHeader == "" {
				_, _ = w.Write(payload)
				return
			}
			limits := strings.Split(strings.TrimPrefix(rangeHeader, "bytes="), "-")
			if len(limits) != 2 {
				http.Error(w, "bad range", http.StatusBadRequest)
				return
			}
			start, _ := strconv.Atoi(limits[0])
			end, _ := strconv.Atoi(limits[1])
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(payload)))
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write(payload[start : end+1])
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	savePath := filepath.Join(t.TempDir(), "spliced.bin")
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", savePath)

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if want := payload[:30]; !bytes.Equal(data, want) {
		t.Fatalf("file = %q, want %q", data, want)
	}
	mu.Lock()
	defer mu.Unlock()
	// 分片是并发取的，落盘顺序由下载器保证，这里只要求每个区间各取一次。
	seen := make(map[string]int, len(ranges))
	for _, value := range ranges {
		seen[value]++
	}
	for _, want := range []string{"bytes=0-9", "bytes=10-19", "bytes=20-29"} {
		if seen[want] != 1 {
			t.Fatalf("range %q requested %d times, want exactly 1 (all ranges = %v)", want, seen[want], ranges)
		}
	}
}

func TestHLSDownloadFailsWhenUpstreamIgnoresByteRange(t *testing.T) {
	payload := strings.Repeat("x", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-BYTERANGE:16@0\n#EXTINF:1,\nbig.bin\n#EXT-X-BYTERANGE:16@16\n#EXTINF:1,\nbig.bin\n")
			return
		}
		// 无视 Range：整份返回 200。按旧实现这会被当成第一个片段，第二个片段又写一遍
		// 整份，得到一个大小翻倍、内容错位的文件。
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", filepath.Join(t.TempDir(), "wrong.bin"))
	status := task.snapshot()
	if status.Status != "error" || !strings.Contains(status.Error, "ignores Range") {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHLSDownloadRetriesTransientSegmentFailure(t *testing.T) {
	const body = "recovered segment"
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\n#EXTINF:4,\nflaky.ts\n")
			return
		}
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	savePath := filepath.Join(t.TempDir(), "retried.ts")
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", savePath)

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != body || attempts.Load() != 2 {
		t.Fatalf("file = %q after %d attempts, want %q", data, attempts.Load(), body)
	}
	if status := task.snapshot(); status.Status != "done" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHLSDownloadWritesInitSegmentOnceAtItsPlace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/playlist.m3u8":
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\na.m4s\n#EXTINF:4,\nb.m4s\n")
		case "/init.mp4":
			_, _ = io.WriteString(w, "INIT")
		case "/a.m4s":
			_, _ = io.WriteString(w, "AAA")
		case "/b.m4s":
			_, _ = io.WriteString(w, "BBB")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	savePath := filepath.Join(t.TempDir(), "ordered.mp4")
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", savePath)

	data, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatal(err)
	}
	// fMP4 分片缺了 moov 就解不出画面，所以初始化段必须在最前面且只出现一次。
	if string(data) != "INITAAABBB" {
		t.Fatalf("file = %q, want %q", data, "INITAAABBB")
	}
	if status := task.snapshot(); status.Status != "done" {
		t.Fatalf("unexpected status: %+v", status)
	}
}

func TestHLSDownloadRejectsEncryptedMapWithoutExplicitIV(t *testing.T) {
	var segmentRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/playlist.m3u8" {
			_, _ = io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\na.m4s\n")
			return
		}
		segmentRequests.Add(1)
		_, _ = w.Write(bytes.Repeat([]byte{0}, 32))
	}))
	defer server.Close()

	task := &downloadTask{httpClient: server.Client()}
	(&App{}).downloadM3u8(context.Background(), task, server.URL+"/playlist.m3u8", filepath.Join(t.TempDir(), "fmp4.mp4"))
	status := task.snapshot()
	if status.Status != "error" || !strings.Contains(status.Error, "unsupported tags") {
		t.Fatalf("unexpected status: %+v", status)
	}
	if segmentRequests.Load() != 0 {
		t.Fatal("an undecryptable playlist must fail before any segment is fetched")
	}
}

func encryptCBC(block cipher.Block, iv, plaintext []byte) []byte {
	out := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, plaintext)
	return out
}

func padPKCS7(data []byte) []byte {
	pad := aes.BlockSize - len(data)%aes.BlockSize
	out := make([]byte, 0, len(data)+pad)
	out = append(out, data...)
	for i := 0; i < pad; i++ {
		out = append(out, byte(pad))
	}
	return out
}
