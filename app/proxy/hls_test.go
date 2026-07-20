package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHLSPlaylistRewritesAllURIForms(t *testing.T) {
	s := NewHLSService()
	base, err := url.Parse("https://media.example.test/path/master.m3u8?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	playlist := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"keys/key.bin\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=1,URI=\"iframe.m3u8\"\nchild/level.m3u8\n"
	rewritten := s.rewritePlaylist(playlist, base)
	for _, upstream := range []string{
		"https://media.example.test/path/keys/key.bin",
		"https://media.example.test/path/init.mp4",
		"https://media.example.test/path/iframe.m3u8",
		"https://media.example.test/path/child/level.m3u8",
	} {
		want := s.URL(upstream)
		if !strings.Contains(rewritten, want) {
			t.Fatalf("missing rewritten URL for %s: %s", upstream, rewritten)
		}
	}
}

func TestHLSServeRewritesPlaylistAndStreamsMedia(t *testing.T) {
	s := NewHLSService()
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, ".m3u8") {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}}, Body: io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n"))}, nil
		}
		return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Type": []string{"video/mp2t"}, "Content-Range": []string{"bytes 0-2/3"}}, Body: io.NopCloser(strings.NewReader("abc"))}, nil
	})

	playlistRequest := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+s.URL("https://example.com/index.m3u8"), nil)
	playlistRecorder := httptest.NewRecorder()
	s.ServeHTTP(playlistRecorder, playlistRequest)
	if playlistRecorder.Code != http.StatusOK || !strings.Contains(playlistRecorder.Body.String(), s.URL("https://example.com/segment.ts")) {
		t.Fatalf("playlist was not rewritten: status=%d body=%s", playlistRecorder.Code, playlistRecorder.Body.String())
	}

	mediaRequest := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+s.URL("https://example.com/segment.ts"), nil)
	mediaRequest.Header.Set("Range", "bytes=0-2")
	mediaRecorder := httptest.NewRecorder()
	s.ServeHTTP(mediaRecorder, mediaRequest)
	if mediaRecorder.Code != http.StatusPartialContent || mediaRecorder.Body.String() != "abc" || mediaRecorder.Header().Get("Content-Range") != "bytes 0-2/3" {
		t.Fatalf("media was not streamed correctly: status=%d headers=%v body=%s", mediaRecorder.Code, mediaRecorder.Header(), mediaRecorder.Body.String())
	}
}

func TestHLSProxyURLIsStableAndRelative(t *testing.T) {
	s := NewHLSService()
	u := "https://cdn.example.test/video/index.m3u8?x=1"
	if got := s.URL(u); got != s.URL(u) || !strings.HasPrefix(got, "/__cczj/hls?u=") {
		t.Fatalf("unexpected proxy URL: %s", got)
	}
}
