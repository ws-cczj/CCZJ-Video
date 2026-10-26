package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestPlaylistQualityUsesMetadataAndNeverDownloadsMedia(t *testing.T) {
	SetAllowPrivateTargets(true)
	defer SetAllowPrivateTargets(false)
	var media atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/master.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=8000000,RESOLUTION=1280x720\n720.m3u8\n#EXT-X-STREAM-INF:CODECS=\"avc1,mp4a\",RESOLUTION=1920x1080,BANDWIDTH=4000000\n1080.m3u8\n")
		case "/media.m3u8":
			fmt.Fprint(w, "#EXTM3U\n#EXT-X-TARGETDURATION:6\n#EXTINF:6,\n1080P.ts\n#EXT-X-ENDLIST\n")
		case "/redirect":
			http.Redirect(w, r, "/master.m3u8", http.StatusFound)
		default:
			media.Add(1)
			w.Write(make([]byte, 1024))
		}
	}))
	defer server.Close()
	for _, path := range []string{"/master.m3u8", "/redirect"} {
		quality, err := ProbePlaylistQuality(context.Background(), server.URL+path)
		if err != nil || !quality.OK || quality.Height != 1080 || quality.Width != 1920 || quality.Bandwidth != 4000000 {
			t.Fatalf("%s: %+v %v", path, quality, err)
		}
	}
	quality, err := ProbePlaylistQuality(context.Background(), server.URL+"/media.m3u8")
	if err != nil || !quality.OK || quality.Height != 0 {
		t.Fatalf("media playlist must remain unknown: %+v %v", quality, err)
	}
	if media.Load() != 0 {
		t.Fatalf("metadata probe downloaded %d media resources", media.Load())
	}
}

func TestPlaylistQualityDoesNotReportSuccessWhenBodyStalls(t *testing.T) {
	SetAllowPrivateTargets(true)
	defer SetAllowPrivateTargets(false)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	quality, err := ProbePlaylistQuality(ctx, server.URL)
	if quality.OK || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("stalled metadata must time out: %+v %v", quality, err)
	}
}

func TestPlaylistQualityKeepsPlaybackRefererAndRedirectCookies(t *testing.T) {
	SetAllowPrivateTargets(true)
	defer SetAllowPrivateTargets(false)
	var origin string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.SetCookie(w, &http.Cookie{Name: "media_session", Value: "accepted", Path: "/"})
			http.Redirect(w, r, "/playlist", http.StatusFound)
			return
		}
		cookie, err := r.Cookie("media_session")
		if err != nil || cookie.Value != "accepted" || r.Referer() != origin+"/" {
			http.Error(w, "missing playback context", http.StatusForbidden)
			return
		}
		fmt.Fprint(w, "#EXTM3U\n#EXT-X-STREAM-INF:BANDWIDTH=800000,RESOLUTION=1280x720\nmedia.m3u8\n")
	}))
	defer server.Close()
	origin = server.URL
	quality, err := ProbePlaylistQuality(context.Background(), server.URL+"/start")
	if err != nil || !quality.OK || quality.Height != 720 {
		t.Fatalf("probe must use the same redirect session as playback: %+v %v", quality, err)
	}
	// Cookies belong to this probe and must not authenticate another one.
	if quality, err := ProbePlaylistQuality(context.Background(), server.URL+"/playlist"); err == nil || quality.OK {
		t.Fatalf("probe leaked cookies into another session: %+v %v", quality, err)
	}
}
