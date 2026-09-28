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

// TestAllowPrivateTargetsDefaultsToDeny 钉住两件事：默认必须拒绝私网，
// 以及放开后确实是同一份判定在放行（而不是另一条没人看的分支）。
func TestAllowPrivateTargetsDefaultsToDeny(t *testing.T) {
	private := []string{
		"http://127.0.0.1:8080/index.m3u8",
		"http://192.168.1.10/video/x.m3u8",
		"http://10.0.0.5/x.ts",
		"http://[::1]/x.m3u8",
		"http://localhost:9999/x.m3u8",
		"http://nas.local:5000/x.m3u8",
	}
	if AllowPrivateTargets() {
		t.Fatal("内网放行默认就是开的，SSRF 闸门等于没有")
	}
	for _, raw := range private {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateTarget(context.Background(), u); err == nil {
			t.Errorf("默认策略放行了 %s", raw)
		}
	}

	SetAllowPrivateTargets(true)
	t.Cleanup(func() { SetAllowPrivateTargets(false) })
	for _, raw := range private {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := ValidateTarget(context.Background(), u); err != nil {
			t.Errorf("开关打开后仍然拒绝 %s: %v", raw, err)
		}
	}
	// 空主机名不是「私网」问题，开关再开也不该放过。
	if _, err := url.Parse("http:///x.m3u8"); err == nil {
		if err := ValidateTarget(context.Background(), mustParse(t, "http:///x.m3u8")); err == nil {
			t.Error("放行私网后，没有主机名的地址也被放过了")
		}
	}
}

func mustParse(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
