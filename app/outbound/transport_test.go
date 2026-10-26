package outbound

import (
	"cczjVideo/app/apperror"
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestEmptyPoolWithoutFallbackReturnsError(t *testing.T) {
	fixturePool(t, "")
	req, _ := http.NewRequest("GET", "https://example.com/search", nil)
	response, err := WrapTransport("danmaku", &http.Transport{}, identityTransport).RoundTrip(req)
	if response != nil || err == nil || apperror.Public(err).Code != apperror.Unavailable {
		t.Fatalf("empty pool must report unavailable, got response=%v err=%v", response, err)
	}
}

func TestSystemWithoutProxyFallsBackToHostDirectConnection(t *testing.T) {
	p := fixturePool(t, "system")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("search result")) }))
	defer server.Close()
	base := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", server.Listener.Addr().String())
	}}
	defer base.CloseIdleConnections()
	req, _ := http.NewRequest("GET", "http://example.test/search", nil)
	transport := &routeTransport{base: base, decorate: identityTransport, category: "collect"}
	response, err := transport.fallback(p, base, nil, req, nil)
	if got := responseText(t, response, err); got != "search result" {
		t.Fatal(got)
	}
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func identityTransport(t http.RoundTripper) http.RoundTripper      { return t }

func fixturePool(t *testing.T, local string, proxies ...string) *Pool {
	t.Helper()
	c := DefaultConfig()
	c.LocalProxy = local
	c.Sources = nil
	c.Feeds = nil
	owner, cancel := context.WithCancel(context.Background())
	p, err := New(owner, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	p.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
	}
	for _, endpoint := range proxies {
		p.nodes = append(p.nodes, node{URL: endpoint, CheckedAt: time.Now()})
	}
	p.readyOnce.Do(func() { close(p.ready) })
	Install(p)
	t.Cleanup(func() { cancel(); p.Close(); Install(nil) })
	return p
}

func responseText(t *testing.T, response *http.Response, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	raw, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestMediaRequestsKeepCallerTransport(t *testing.T) {
	for _, category := range []string{"playback", "lineprobe", "download"} {
		for _, state := range []string{"refreshing", "exhausted", "healthy"} {
			t.Run(category+"/"+state, func(t *testing.T) {
				var proxyCalls, counted, proxyChecks atomic.Int32
				badProxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					proxyCalls.Add(1)
					w.WriteHeader(http.StatusForbidden)
				}))
				defer badProxy.Close()
				p := fixturePool(t, badProxy.URL, badProxy.URL)
				if state == "refreshing" {
					p.nodes = nil
					p.ready = make(chan struct{})
				} else if state == "exhausted" {
					p.nodes = nil
				}
				target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/master.m3u8" {
						http.SetCookie(w, &http.Cookie{Name: "session", Value: "media", Path: "/"})
						fmt.Fprint(w, "#EXTM3U\nchild.m3u8\n")
						return
					}
					cookie, err := r.Cookie("session")
					if err != nil || cookie.Value != "media" || r.Header.Get("Referer") != "http://media.example/" {
						http.Error(w, "lost media session", http.StatusForbidden)
						return
					}
					if r.URL.Path == "/segment.ts" {
						if r.Header.Get("Range") != "bytes=2-" {
							t.Error("media Range lost")
						}
						w.Header().Set("Content-Range", "bytes 2-6/7")
						w.WriteHeader(http.StatusPartialContent)
						fmt.Fprint(w, "chunk")
						return
					}
					fmt.Fprint(w, "#EXTM3U\nsegment.ts\n")
				}))
				defer target.Close()
				base := &http.Transport{
					Proxy: func(*http.Request) (*url.URL, error) { proxyChecks.Add(1); return nil, nil },
					DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
						if address != "media.example:80" {
							return nil, fmt.Errorf("unexpected media route %s", address)
						}
						return (&net.Dialer{}).DialContext(ctx, network, target.Listener.Addr().String())
					},
				}
				defer base.CloseIdleConnections()
				jar, err := cookiejar.New(nil)
				if err != nil {
					t.Fatal(err)
				}
				client := &http.Client{Timeout: 300 * time.Millisecond, Jar: jar, Transport: WrapTransport(category, base, func(selected http.RoundTripper) http.RoundTripper {
					return rtFunc(func(r *http.Request) (*http.Response, error) { counted.Add(1); return selected.RoundTrip(r) })
				})}
				for _, path := range []string{"master.m3u8", "child.m3u8", "segment.ts"} {
					req, err := http.NewRequest(http.MethodGet, "http://media.example/"+path, nil)
					if err != nil {
						t.Fatal(err)
					}
					req.Header.Set("Referer", "http://media.example/")
					if path == "segment.ts" {
						req.Header.Set("Range", "bytes=2-")
					}
					response, err := client.Do(req)
					body := responseText(t, response, err)
					if path == "segment.ts" {
						if response.StatusCode != http.StatusPartialContent || body != "chunk" {
							t.Fatalf("segment: status=%d body=%q", response.StatusCode, body)
						}
					} else if response.StatusCode != http.StatusOK || !strings.HasPrefix(body, "#EXTM3U") {
						t.Fatalf("playlist: status=%d body=%q", response.StatusCode, body)
					}
				}
				status := p.Snapshot()
				if proxyCalls.Load() != 0 || status.PublicAttempts != 0 || status.LocalFallbacks != 0 || counted.Load() != 3 || proxyChecks.Load() != 3 {
					t.Fatalf("media route changed: proxy=%d pool=%+v counted=%d callerProxy=%d", proxyCalls.Load(), status, counted.Load(), proxyChecks.Load())
				}
			})
		}
	}
}

func TestPoolExhaustionPrecedesLocalFallbackAndNeverDialsTarget(t *testing.T) {
	var mu sync.Mutex
	order := []string{}
	proxy := func(name string, status int) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mu.Lock()
			order = append(order, name)
			mu.Unlock()
			if r.URL.Host != "public.example" {
				t.Errorf("lost target identity: %s", r.URL)
			}
			w.WriteHeader(status)
			fmt.Fprint(w, name)
		}))
	}
	a, b, local := proxy("a", 502), proxy("b", 403), proxy("local", 200)
	defer a.Close()
	defer b.Close()
	defer local.Close()
	p := fixturePool(t, local.URL, a.URL, b.URL)
	var direct atomic.Int32
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		direct.Add(1)
		return nil, errors.New("direct forbidden")
	}}
	var counted atomic.Int32
	client := &http.Client{Transport: WrapTransport("douban", base, func(base http.RoundTripper) http.RoundTripper {
		return rtFunc(func(r *http.Request) (*http.Response, error) { counted.Add(1); return base.RoundTrip(r) })
	})}
	response, err := client.Get("http://public.example/data")
	if got := responseText(t, response, err); got != "local" {
		t.Fatal(got)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 3 || order[2] != "local" || direct.Load() != 0 || counted.Load() != 3 {
		t.Fatalf("order=%v direct=%d counted=%d", order, direct.Load(), counted.Load())
	}
	if p.Snapshot().LocalFallbacks != 1 {
		t.Fatal(p.Snapshot())
	}
}

func TestPoolFailureNeverFallsThroughToDirect(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(502) }))
	defer proxy.Close()
	fixturePool(t, "", proxy.URL)
	var direct atomic.Int32
	base := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
		direct.Add(1)
		return nil, errors.New("direct forbidden")
	}}
	client := &http.Client{Transport: WrapTransport("danmaku", base, identityTransport)}
	if _, err := client.Get("http://public.example/video"); err == nil || direct.Load() != 0 {
		t.Fatalf("direct=%d err=%v", direct.Load(), err)
	}
}

func TestLANStaysDirectAndMixedDNSIsRejected(t *testing.T) {
	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { proxied.Add(1); fmt.Fprint(w, "proxy") }))
	defer proxy.Close()
	lan := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "LAN") }))
	defer lan.Close()
	p := fixturePool(t, proxy.URL, proxy.URL)
	p.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	proxyURL, _ := url.Parse(proxy.URL)
	client := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{Proxy: http.ProxyURL(proxyURL)}, identityTransport)}
	response, err := client.Get(lan.URL)
	if got := responseText(t, response, err); got != "LAN" {
		t.Fatal(got)
	}
	p.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("127.0.0.1"), netip.MustParseAddr("8.8.8.8")}, nil
	}
	if _, err := client.Get("http://mixed.example"); err == nil || proxied.Load() != 0 {
		t.Fatalf("mixed DNS routed: %v", err)
	}
}

func connectProxy(t *testing.T) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			w.WriteHeader(400)
			return
		}
		upstream, err := net.DialTimeout("tcp", r.Host, time.Second)
		if err != nil {
			w.WriteHeader(502)
			return
		}
		downstream, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			upstream.Close()
			return
		}
		defer downstream.Close()
		defer upstream.Close()
		fmt.Fprint(buffer, "HTTP/1.1 200 Connection Established\r\n\r\n")
		buffer.Flush()
		done := make(chan struct{})
		go func() { io.Copy(upstream, downstream); upstream.Close(); close(done) }()
		io.Copy(downstream, upstream)
		downstream.Close()
		<-done
	}))
	t.Cleanup(server.Close)
	return server
}

func TestCONNECTStreamingRangeCancellationAndTLSValidation(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Range") != "bytes=2-" {
			t.Error("Range lost")
		}
		w.WriteHeader(206)
		fmt.Fprint(w, "chunk")
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer target.Close()
	proxy := connectProxy(t)
	fixturePool(t, "", proxy.URL)
	trusted := target.Client().Transport.(*http.Transport).Clone()
	trusted.TLSClientConfig.InsecureSkipVerify = false
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	trusted.TLSClientConfig.RootCAs = roots
	client := &http.Client{Transport: WrapTransport("danmaku", trusted, identityTransport)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", target.URL, nil)
	req.Header.Set("Range", "bytes=2-")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 206 {
		t.Fatal(response.StatusCode)
	}
	data := make([]byte, 5)
	if _, err := io.ReadFull(response.Body, data); err != nil || string(data) != "chunk" {
		t.Fatal(string(data), err)
	}
	cancel()
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("cancel did not stop streamed body")
	}
	untrusted := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{}, identityTransport)}
	if _, err = untrusted.Get(target.URL); err == nil {
		t.Fatal("untrusted certificate was accepted")
	}
}

func TestWritesAreNotReplayedAfterDelivery(t *testing.T) {
	var first, fallback atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first.Add(1)
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(503)
	}))
	defer proxy.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fallback.Add(1); w.WriteHeader(200) }))
	defer local.Close()
	fixturePool(t, local.URL, proxy.URL)
	client := &http.Client{Transport: WrapTransport("douban", &http.Transport{}, identityTransport)}
	response, err := client.Post("http://public.example/session", "application/json", strings.NewReader(`{"token":"x"}`))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if first.Load() != 1 || fallback.Load() != 0 || response.StatusCode != 503 {
		t.Fatalf("write replayed: first=%d fallback=%d", first.Load(), fallback.Load())
	}
}

func TestConcurrentRequestsKeepStableSiteRoute(t *testing.T) {
	var a, b atomic.Int32
	makeProxy := func(count *atomic.Int32) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { count.Add(1); fmt.Fprint(w, "ok") }))
	}
	pa, pb := makeProxy(&a), makeProxy(&b)
	defer pa.Close()
	defer pb.Close()
	fixturePool(t, "", pa.URL, pb.URL)
	client := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{}, identityTransport)}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			response, err := client.Get("http://chapter.example.com/body")
			if err != nil {
				t.Error(err)
				return
			}
			io.Copy(io.Discard, response.Body)
			response.Body.Close()
		}()
	}
	wg.Wait()
	if !((a.Load() == 16 && b.Load() == 0) || (a.Load() == 0 && b.Load() == 16)) {
		t.Fatalf("unstable session routing: %d %d", a.Load(), b.Load())
	}
}

func TestTargetConnectionFailureDoesNotQuarantineOtherSites(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Hostname() == "blocked.example" {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			connection.Close()
			return
		}
		fmt.Fprint(w, "public")
	}))
	defer proxy.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "local") }))
	defer local.Close()
	p := fixturePool(t, local.URL, proxy.URL)
	client := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{}, identityTransport)}
	defer client.CloseIdleConnections()
	for _, step := range []struct{ target, want string }{
		{"http://working.example/master", "public"},
		{"http://blocked.example/search", "local"},
		{"http://working.example/child", "public"},
	} {
		response, err := client.Get(step.target)
		if got := responseText(t, response, err); got != step.want {
			t.Fatalf("%s routed through %q, want %q", step.target, got, step.want)
		}
	}
	if len(p.candidates("blocked.example")) != 0 || len(p.candidates("working.example")) != 1 {
		t.Fatal("a target failure must cool only that site's route")
	}
}

func TestUnreachableProxyIsStillQuarantinedGlobally(t *testing.T) {
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { fmt.Fprint(w, "local") }))
	defer local.Close()
	p := fixturePool(t, local.URL, "http://8.8.8.8:8080")
	p.dialProxy = func(ctx context.Context, network, address string) (net.Conn, error) {
		if address == "8.8.8.8:8080" {
			return nil, errors.New("proxy connection refused")
		}
		return (&net.Dialer{}).DialContext(ctx, network, address)
	}
	client := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{}, identityTransport)}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://working.example/data")
	if got := responseText(t, response, err); got != "local" {
		t.Fatal(got)
	}
	if len(p.candidates("unrelated.example")) != 0 {
		t.Fatal("unreachable proxy stayed eligible for other sites")
	}
}

func TestFailedCONNECTOnlyCoolsTargetSite(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodConnect {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		fmt.Fprint(w, "public")
	}))
	defer proxy.Close()
	p := fixturePool(t, "", proxy.URL)
	client := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{}, identityTransport)}
	defer client.CloseIdleConnections()
	if response, err := client.Get("https://blocked.example/data"); err == nil {
		response.Body.Close()
		t.Fatal("failed CONNECT was reported as success")
	}
	response, err := client.Get("http://working.example/data")
	if got := responseText(t, response, err); got != "public" {
		t.Fatal(got)
	}
	if len(p.candidates("blocked.example")) != 0 || len(p.candidates("working.example")) != 1 {
		t.Fatal("CONNECT rejection quarantined unrelated sites")
	}
}

func TestShutdownCancelsRequestsWaitingForInitialPool(t *testing.T) {
	p := fixturePool(t, "", []string{}...)
	p.ready = make(chan struct{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://public.example", nil)
	_, err := WrapTransport("danmaku", &http.Transport{}, identityTransport).RoundTrip(req)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
	p.Close()
	req, _ = http.NewRequest("GET", "http://public.example", nil)
	// Closed pools must fail before waiting on a never-started refresh.
	ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = WrapTransport("danmaku", &http.Transport{}, identityTransport).RoundTrip(req.WithContext(ctx))
	if err == nil {
		t.Fatal("closed pool admitted request")
	}
}

func TestProxyEndpointValidation(t *testing.T) {
	for _, raw := range []string{"http://127.0.0.1:8080", "http://10.0.0.1:80", "http://100.64.0.1:80", "http://public.example:80", "file://8.8.8.8:80", "http://u:p@8.8.8.8:80"} {
		if _, err := parseEndpoint(raw); err == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"8.8.8.8:8080", "socks5://1.1.1.1:1080", "https://8.8.4.4:443"} {
		if _, err := parseEndpoint(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
	u, _ := url.Parse("http://8.8.8.8:80")
	if u.Hostname() == "" {
		t.Fatal("invalid fixture")
	}
}

func TestProxyCloneKeepsNegotiatedHTTP2Support(t *testing.T) {
	p := fixturePool(t, "", []string{}...)
	base := &http.Transport{}
	// Go initializes the base's ALPN protocols when Clone is called.
	base.Clone()
	u, _ := parseEndpoint("http://8.8.8.8:80")
	clone, release, err := p.lease(base, u)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	if clone.Protocols == nil || !clone.Protocols.HTTP2() {
		t.Fatal("proxy dial override disabled HTTP/2 while retaining h2 ALPN")
	}
}

func TestOnlyDoubanAndDanmakuRouteThroughPool(t *testing.T) {
	var attempts atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { attempts.Add(1); io.WriteString(w, "proxied") }))
	defer proxy.Close()
	fixturePool(t, "", proxy.URL)
	for _, category := range []string{"douban", "danmaku"} {
		client := &http.Client{Transport: WrapTransport(category, &http.Transport{}, identityTransport)}
		response, err := client.Get("http://public.example/request")
		if got := responseText(t, response, err); got != "proxied" {
			t.Fatal(category, got)
		}
		client.CloseIdleConnections()
	}
	if attempts.Load() != 2 {
		t.Fatal(attempts.Load())
	}
}
