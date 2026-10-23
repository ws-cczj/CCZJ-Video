package proxy

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func contextPNG(t *testing.T, value uint8) []byte {
	t.Helper()
	var data bytes.Buffer
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	picture.SetRGBA(0, 0, color.RGBA{R: value, A: 255})
	if err := png.Encode(&data, picture); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestImageCacheSeparatesSourceRefererAndHeaderRevision(t *testing.T) {
	allowPrivateForTest(t)
	dir := t.TempDir()
	p := NewImageProxy(func() string { return dir })
	var requests int
	version := "one"
	p.SetHeaderResolver(func(source string) map[string]string {
		return map[string]string{"X-Source": source, "X-Version": version}
	})
	p.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(contextPNG(t, uint8(requests))))}, nil
	})
	const target = "http://127.0.0.1:9/shared.png"
	read := func(source, ref string) []byte {
		t.Helper()
		w := httptest.NewRecorder()
		p.ServeHTTP(w, imageRequest(target, ref, source))
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("image response: status=%d headers=%v", w.Code, w.Header())
		}
		return w.Body.Bytes()
	}
	a := read("a", "https://site.example/book/1")
	if !bytes.Equal(a, read("a", "https://site.example/book/1")) || requests != 1 {
		t.Fatal("equivalent requests did not use disk cache")
	}
	b := read("b", "https://site.example/book/1")
	c := read("a", "https://site.example/book/2")
	version = "two"
	d := read("a", "https://site.example/book/1")
	if requests != 4 || bytes.Equal(a, b) || bytes.Equal(a, c) || bytes.Equal(a, d) {
		t.Fatalf("source, Referer or configuration reused stale image: %d upstream requests", requests)
	}
}

func TestImageFailureCooldownIsContextSpecific(t *testing.T) {
	allowPrivateForTest(t)
	dir := t.TempDir()
	p := NewImageProxy(func() string { return dir })
	count := 0
	p.SetHeaderResolver(func(source string) map[string]string { return map[string]string{"X-Source": source} })
	p.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		count++
		status := http.StatusOK
		if r.Header.Get("X-Source") == "broken" {
			status = http.StatusForbidden
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"image/png"}}, Body: io.NopCloser(bytes.NewReader(tinyPNG))}, nil
	})
	for _, tc := range []struct {
		source string
		status int
	}{{"broken", 502}, {"broken", 503}, {"working", 200}} {
		w := httptest.NewRecorder()
		p.ServeHTTP(w, imageRequest("http://127.0.0.1:9/image.png", "", tc.source))
		if w.Code != tc.status {
			t.Fatalf("%s: got %d, want %d", tc.source, w.Code, tc.status)
		}
	}
	if count != 2 {
		t.Fatalf("upstream called %d times, want 2", count)
	}
}

func TestImageSessionsSeparateCookiesAndBypassDiskForMutableSessions(t *testing.T) {
	allowPrivateForTest(t)
	dir := t.TempDir()
	p := NewImageProxy(func() string { return dir })
	var seen []string
	p.client.Transport = roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		seen = append(seen, r.Header.Get("Cookie"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"image/png"}, "Set-Cookie": {"session=owned; Path=/"}}, Body: io.NopCloser(bytes.NewReader(tinyPNG))}, nil
	})
	for _, source := range []string{"a", "b", "a"} {
		w := httptest.NewRecorder()
		p.ServeHTTP(w, imageRequest("http://127.0.0.1:9/session.png", "", source))
		if w.Code != 200 {
			t.Fatalf("image status: %d", w.Code)
		}
	}
	if len(seen) != 3 || seen[0] != "" || seen[1] != "" || seen[2] != "session=owned" {
		t.Fatalf("cookie session isolation/cache bypass failed: %v", seen)
	}
}
