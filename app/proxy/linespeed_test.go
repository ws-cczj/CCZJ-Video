package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// allowPrivateProbeTargets 打开私网放行，否则 httptest 的 127.0.0.1 会被地址白名单拒掉。
// 开关是包级全局的，用完必须还原，不然同包其它测试的私网断言会失真。
func allowPrivateProbeTargets(t *testing.T) {
	t.Helper()
	previous := AllowPrivateTargets()
	SetAllowPrivateTargets(true)
	t.Cleanup(func() { SetAllowPrivateTargets(previous) })
}

func TestProbePlayURLMeasuresDirectMedia(t *testing.T) {
	allowPrivateProbeTargets(t)
	payload := strings.Repeat("x", 4096)
	var ranged, plain atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "" {
			ranged.Add(1)
		} else {
			plain.Add(1)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()

	sample := ProbePlayURL(context.Background(), server.URL+"/movie.mp4")
	if !sample.OK {
		t.Fatalf("sample = %+v; want a usable measurement", sample)
	}
	if sample.Bytes != int64(len(payload)) {
		t.Fatalf("bytes = %d; want %d", sample.Bytes, len(payload))
	}
	if sample.BytesPerSec <= 0 {
		t.Fatalf("bytes_per_sec = %v", sample.BytesPerSec)
	}
	if ranged.Load() != 1 || plain.Load() != 0 {
		t.Fatalf("requests: ranged=%d plain=%d; want exactly one ranged GET", ranged.Load(), plain.Load())
	}
}

// 列表只有几十 KB，按列表测速排不出快慢，必须下钻到它的分片再取样。
func TestProbePlayURLDrillsIntoFirstSegment(t *testing.T) {
	allowPrivateProbeTargets(t)
	segment := strings.Repeat("y", 2048)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/index.m3u8":
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-VERSION:3\n#EXTINF:10.0,\nseg/1.ts\n"))
		case "/seg/1.ts":
			_, _ = w.Write([]byte(segment))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	sample := ProbePlayURL(context.Background(), server.URL+"/index.m3u8")
	if !sample.OK {
		t.Fatalf("sample = %+v", sample)
	}
	if !strings.HasSuffix(sample.ProbedURL, "/seg/1.ts") {
		t.Fatalf("probed %q; want the first segment", sample.ProbedURL)
	}
	if sample.Bytes != int64(len(segment)) {
		t.Fatalf("bytes = %d; want the segment size %d", sample.Bytes, len(segment))
	}
}

// 主列表套子列表也要能落到真正的分片，但不能无限下钻。
func TestProbePlayURLFollowsNestedPlaylistOnce(t *testing.T) {
	allowPrivateProbeTargets(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000\n720.m3u8\n"))
		case "/720.m3u8":
			_, _ = w.Write([]byte("#EXTM3U\n#EXTINF:5.0,\npart.ts\n"))
		case "/part.ts":
			_, _ = w.Write([]byte("zzz"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	sample := ProbePlayURL(context.Background(), server.URL+"/master.m3u8")
	if !sample.OK || !strings.HasSuffix(sample.ProbedURL, "/part.ts") {
		t.Fatalf("sample = %+v; want the nested segment", sample)
	}
}

// 不接受 Range 的源不等于线路坏了：去掉 Range 重取一次，别把好线路标成不可选。
func TestProbePlayURLRetriesWithoutRangeWhenRejected(t *testing.T) {
	allowPrivateProbeTargets(t)
	hits := atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Range") != "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("0123456789"))
	}))
	defer server.Close()

	sample := ProbePlayURL(context.Background(), server.URL+"/movie.mp4")
	if !sample.OK {
		t.Fatalf("sample = %+v; want the ranged rejection to fall back", sample)
	}
	if sample.Bytes != 10 {
		t.Fatalf("bytes = %d; want 10", sample.Bytes)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits = %d; want one ranged attempt plus one plain retry", hits.Load())
	}
}

func TestProbePlayURLMarksDeadLineUnusable(t *testing.T) {
	allowPrivateProbeTargets(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	sample := ProbePlayURL(context.Background(), server.URL+"/gone.m3u8")
	if sample.OK {
		t.Fatalf("sample = %+v; want a dead line marked unusable", sample)
	}
	if !strings.Contains(sample.Error, "status 404") {
		t.Fatalf("error = %q; want it to record the status", sample.Error)
	}
}

// 测速不能成为绕过播放代理地址白名单的旁路，默认策略下私网目标必须被拒。
func TestProbePlayURLKeepsPrivateTargetsBlockedByDefault(t *testing.T) {
	previous := AllowPrivateTargets()
	SetAllowPrivateTargets(false)
	t.Cleanup(func() { SetAllowPrivateTargets(previous) })

	sample := ProbePlayURL(context.Background(), "http://127.0.0.1:9/index.m3u8")
	if sample.OK {
		t.Fatalf("sample = %+v; want the private address rejected", sample)
	}
	if !strings.Contains(sample.Error, "resolve probe target") {
		t.Fatalf("error = %q", sample.Error)
	}
}
