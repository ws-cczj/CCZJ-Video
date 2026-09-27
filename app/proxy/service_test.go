package proxy

import (
	"context"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestIsPublicAddr(t *testing.T) {
	if !IsPublicAddr(netip.MustParseAddr("1.1.1.1")) {
		t.Fatal("expected public address to be allowed")
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "::1", "fc00::1"} {
		if IsPublicAddr(netip.MustParseAddr(raw)) {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
}

func TestValidateTargetRejectsLocalhost(t *testing.T) {
	u, err := url.Parse("http://localhost/image.png")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateTarget(context.Background(), u); err == nil {
		t.Fatal("expected localhost to be rejected")
	}
}

func TestFetchRejectsOversizedImage(t *testing.T) {
	service := NewService()
	service.client.Transport = roundTripperFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"image/png"}},
			Body:       io.NopCloser(strings.NewReader(strings.Repeat("x", maxImageBytes+1))),
		}, nil
	})

	if _, err := service.fetch(context.Background(), "https://1.1.1.1/image.png"); err == nil {
		t.Fatal("expected oversized image to be rejected")
	}
}

func TestFetchRejectsRedirectToPrivateTarget(t *testing.T) {
	service := NewService()
	requests := 0
	service.client.Transport = roundTripperFunc(func(request *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://127.0.0.1/private.png"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    request,
		}, nil
	})

	if _, err := service.fetch(context.Background(), "https://1.1.1.1/image.png"); err == nil {
		t.Fatal("expected redirect to private target to be rejected")
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestWaitContextCancels(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitContext(ctx, time.Second); err != context.Canceled {
		t.Fatalf("waitContext() = %v, want context.Canceled", err)
	}
}
