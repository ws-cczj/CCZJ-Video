package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestReadingImageColdBurstSharesOneUpstreamRead(t *testing.T) {
	allowPrivateForTest(t)
	p := NewImageProxy(nil)
	defer p.Close(context.Background())
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	p.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"image/png"}}, Body: io.NopCloser(bytes.NewReader(tinyPNG)), Request: req}, nil
	})
	responses := make(chan *httptest.ResponseRecorder, 8)
	request := func() {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", p.URL("https://cdn.example.test/image.png", "https://reader.example.test/chapter", "source"), nil)
		p.ServeHTTP(rec, req)
		responses <- rec
	}
	go request()
	<-started
	for range 7 {
		go request()
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, joined := p.reads.Stats()
		if joined == 7 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_, joined := p.reads.Stats()
	if joined != 7 {
		close(release)
		t.Fatalf("only %d requests joined", joined)
	}
	close(release)
	for range 8 {
		rec := <-responses
		if rec.Code != 200 || !bytes.Equal(rec.Body.Bytes(), tinyPNG) {
			t.Fatalf("response = %d %q", rec.Code, rec.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("cold reads = %d, want 1", calls.Load())
	}
}
