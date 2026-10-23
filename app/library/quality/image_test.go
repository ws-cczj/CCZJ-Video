package quality

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"

	"cczjVideo/app/proxy"
)

func TestImageProbeDecodesPixelsAndRetainsContext(t *testing.T) {
	proxy.SetAllowPrivateTargets(true)
	defer proxy.SetAllowPrivateTargets(false)
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		write := func(data []byte) {
			if _, err := w.Write(data); err != nil {
				t.Error(err)
			}
		}
		if r.Header.Get("Referer") != "https://origin.example/chapter" || r.Header.Get("X-Source") != "required" {
			w.WriteHeader(403)
			return
		}
		switch r.URL.Path {
		case "/ok":
			write(b.Bytes())
		case "/truncated":
			write(b.Bytes()[:40])
		case "/login":
			w.WriteHeader(401)
		default:
			w.Header().Set("Content-Type", "image/png")
			write([]byte("<html>not an image</html>"))
		}
	}))
	defer srv.Close()
	probe, closeProbe := NewImageProbe(map[string]string{"X-Source": "required"})
	defer closeProbe()
	valid := probe(context.Background(), srv.URL+"/ok", "https://origin.example/chapter")
	if valid.Failure != nil || valid.Width != 2 || valid.Height != 2 || len(valid.Fingerprint) != 64 {
		t.Fatal(valid)
	}
	for _, path := range []string{"/truncated", "/html"} {
		got := probe(context.Background(), srv.URL+path, "https://origin.example/chapter")
		if got.Failure == nil || got.Failure.Code != "CORRUPT" {
			t.Fatal(path, got)
		}
	}
	if got := probe(context.Background(), srv.URL+"/login", "https://origin.example/chapter"); got.Failure == nil || got.Failure.Code != "AUTHENTICATION_REQUIRED" {
		t.Fatal(got)
	}
}
