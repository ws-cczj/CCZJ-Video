package proxy

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// AVPlayer fetches the rewritten children itself; unlike Android's WebView
// interceptor it cannot put the capability prefix back onto root-relative URLs.
func TestIOSMediaBridgeRetainsCapabilityForPlaylistKeysAndRanges(t *testing.T) {
	hls := NewHLSService()
	defer hls.Close()
	hls.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		body := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"key.bin\"\nchild.m3u8\nsegment.ts\n"
		header := http.Header{"Content-Type": {"application/vnd.apple.mpegurl"}}
		status := http.StatusOK
		if r.URL.Path == "/segment.ts" {
			if r.Header.Get("Range") != "bytes=0-2" || r.Header.Get("Referer") != "https://1.1.1.1/" {
				t.Error("lost native media range or referer")
			}
			body, status = "abc", http.StatusPartialContent
			header = http.Header{"Content-Type": {"video/mp2t"}, "Content-Range": {"bytes 0-2/3"}}
		}
		return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
	bridge := NewMediaBridge(context.Background(), hls)
	defer bridge.Close()
	endpoint, err := bridge.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(endpoint + strings.Replace(hls.URL("https://1.1.1.1/master.m3u8"), "/__cczj/hls", "/__cczj/ios-hls", 1))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	playlist, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		t.Fatalf("native playlist status %d", response.StatusCode)
	}
	origin := endpoint[:strings.LastIndex(endpoint, "/")]
	for _, name := range []string{"key.bin", "child.m3u8", "segment.ts"} {
		child := strings.TrimPrefix(endpoint, origin) + strings.Replace(hls.urlFor("https://1.1.1.1/"+name, "https://1.1.1.1/"), "/__cczj/hls", "/__cczj/ios-hls", 1)
		if !strings.Contains(string(playlist), child) {
			t.Fatalf("child lost capability: %s", name)
		}
		request, err := http.NewRequest(http.MethodGet, origin+child, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Range", "bytes=0-2")
		stream, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		data, readErr := io.ReadAll(stream.Body)
		stream.Body.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if name == "segment.ts" && (stream.StatusCode != 206 || string(data) != "abc") {
			t.Fatalf("native segment did not stream: %d", stream.StatusCode)
		}
	}
}

func TestMediaBridgeStreamsBeforeEOFAndPreservesRange(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	release := make(chan struct{})
	defer close(release)
	bridge := NewMediaBridge(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=2-3" || r.URL.Query().Get("u") != "signed+value" {
			http.Error(w, "lost request metadata", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Range", "bytes 2-3/10")
		w.Header().Set("Content-Length", "2")
		w.WriteHeader(http.StatusPartialContent)
		if _, err := io.WriteString(w, "a"); err != nil {
			return
		}
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if _, err := io.WriteString(w, "b"); err != nil {
			return
		}
	}))
	defer func() {
		if err := bridge.Close(); err != nil {
			t.Error(err)
		}
	}()
	endpoint, err := bridge.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/__cczj/hls?u=signed%2Bvalue", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=2-3")
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 206 || response.Header.Get("Content-Range") != "bytes 2-3/10" {
		t.Fatalf("lost range response: %v", response)
	}
	var first [1]byte
	if _, err := io.ReadFull(response.Body, first[:]); err != nil {
		t.Fatalf("first byte waited for EOF: %v", err)
	}
	if first[0] != 'a' {
		t.Fatalf("corrupt first byte: %q", first)
	}
}

func TestMediaBridgeCapabilityScopeAndClose(t *testing.T) {
	bridge := NewMediaBridge(context.Background(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	endpoint, err := bridge.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := bridge.Close(); err != nil {
			t.Error(err)
		}
	}()
	base := endpoint[:strings.LastIndex(endpoint, "/")]
	for _, tc := range []struct {
		name, method, url string
		status            int
	}{
		{"missing capability", "GET", base + "/__cczj/hls", 404},
		{"other assets", "GET", endpoint + "/index.html", 404},
		{"write method", "POST", endpoint + "/__cczj/hls", 405},
		{"authorized stream", "GET", endpoint + "/__cczj/hls", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, tc.method, tc.url, nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer response.Body.Close()
			if response.StatusCode != tc.status {
				t.Fatalf("status %d, want %d", response.StatusCode, tc.status)
			}
		})
	}
	if err := bridge.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := bridge.Endpoint(); err == nil {
		t.Fatal("closed bridge restarted")
	}
}

func TestMobileImageBridgePreservesIdentityAndQuery(t *testing.T) {
	bridge := NewMediaBridgeWithImages(context.Background(), http.NotFoundHandler(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/__cczj/img" || r.URL.Query().Get("u") != "signed+image" || r.URL.Query().Get("ref") != "chapter/ref" || r.URL.Query().Get("k") != "source_b" {
			http.Error(w, "lost image request context", 400)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		io.WriteString(w, "image-bytes")
	}))
	defer bridge.Close()
	endpoint, err := bridge.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(endpoint + "/__cczj/img?u=signed%2Bimage&ref=chapter%2Fref&k=source_b")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != 200 || response.Header.Get("Content-Type") != "image/png" || string(body) != "image-bytes" {
		t.Fatalf("image response: %d %q %v", response.StatusCode, body, err)
	}
}
