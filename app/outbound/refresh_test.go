package outbound

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"cczjVideo/app/applog"
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "cczj-outbound-tests-")
	if err != nil {
		panic(err)
	}
	if err = applog.Init(dir); err != nil {
		panic(err)
	}
	code := m.Run()
	applog.Default().Close()
	os.RemoveAll(dir)
	os.Exit(code)
}

type memoryStore struct {
	mu     sync.Mutex
	values map[string]string
	fail   bool
}

func (s *memoryStore) Get(k string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.values[k], nil
}
func (s *memoryStore) Set(k, v string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errors.New("storage unavailable")
	}
	s.values[k] = v
	return nil
}

func TestRefreshValidatesCONNECTAndRestoresSnapshotWithoutRenewingAge(t *testing.T) {
	var mu sync.Mutex
	sourceCalls, healthCalls := 0, 0
	target := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if r.URL.Path == "/list" {
			sourceCalls++
			io.WriteString(w, `[{"protocol":"http","ip":"8.8.8.8","port":8080,"ssl":true,"uptime_percent":90}]`)
		} else {
			healthCalls++
			io.WriteString(w, `{"ip":"1.1.1.1"}`)
		}
	}))
	defer target.Close()
	proxy := connectProxy(t)
	store := &memoryStore{values: map[string]string{}}
	c := DefaultConfig()
	c.LocalProxy = proxy.URL
	c.Sources = []string{target.URL + "/list"}
	c.Feeds = nil
	c.MaxCandidates = 3
	p, err := New(context.Background(), c, store)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	p.probeURL = target.URL + "/health"
	roots := x509.NewCertPool()
	roots.AddCert(target.Certificate())
	p.control.TLSClientConfig = target.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	p.control.TLSClientConfig.InsecureSkipVerify = false
	p.control.TLSClientConfig.RootCAs = roots
	proxyAddress := strings.TrimPrefix(proxy.URL, "http://")
	p.dialProxy = func(ctx context.Context, network, address string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, proxyAddress)
	}
	if err := p.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.Snapshot().Healthy != 1 {
		t.Fatal(p.Snapshot())
	}
	saved, _ := store.Get(snapshotKey)
	var nodes []node
	if json.Unmarshal([]byte(saved), &nodes) != nil || len(nodes) != 1 {
		t.Fatal(saved)
	}
	restarted, err := New(context.Background(), c, store)
	if err != nil {
		t.Fatal(err)
	}
	defer restarted.Close()
	if len(restarted.nodes) != 1 || !restarted.nodes[0].CheckedAt.Equal(nodes[0].CheckedAt) {
		t.Fatal("restart renewed health time")
	}
	// A broken source cannot erase the usable node; no partial pool is published.
	p.sources[0].config.URL = target.URL + "/invalid"
	p.sources[0].status.NextAttempt = 0
	p.nextProbe = time.Time{}
	p.config.Seeds = nil
	p.dialProxy = func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("offline") }
	p.closeBase(p.control)
	// Drop old cloned transports so the injected failure is used by this refresh.
	p.mu.Lock()
	for k, v := range p.transports {
		v.transport.CloseIdleConnections()
		delete(p.transports, k)
	}
	p.mu.Unlock()
	if err := p.Refresh(context.Background()); err == nil || len(p.nodes) != 1 || p.Snapshot().Healthy != 0 || p.Snapshot().LastError == "" {
		t.Fatal("failed refresh erased prior snapshot", err, p.Snapshot())
	}
	mu.Lock()
	defer mu.Unlock()
	if sourceCalls != 1 || healthCalls != 1 {
		t.Fatal(sourceCalls, healthCalls)
	}
}

func TestSourceDecodingRejectsUntrustedAddressesAndBounds(t *testing.T) {
	rows, err := decodeCandidates([]byte(`[{"protocol":"http","ip":"8.8.8.8","port":80,"ssl":true},{"protocol":"http","ip":"1.1.1.1","port":80,"ssl":false},{"proxy":"socks5://9.9.9.9:1080","protocol":"socks5"},{"proxy":"http://127.0.0.1:80","protocol":"http","https":true}]`))
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	rows, err = decodeCandidates([]byte("http://8.8.8.8:80\n10.0.0.1:80\nhttps://1.1.1.1:443\n"))
	if err != nil || len(rows) != 2 {
		t.Fatal(rows, err)
	}
	if _, err = decodeCandidates([]byte(strings.Repeat("x", (2<<20)+1))); err == nil {
		t.Fatal("unbounded source admitted")
	}
	for _, raw := range []string{"null", "[]", `{"maxCandidates":1000}`, `{"sources":["http://example.com/list"]}`, `{"localProxy":"file:///tmp/x"}`} {
		if _, err := DecodeConfig(raw); err == nil {
			t.Fatal(raw)
		}
	}
}

func TestRefreshCancellationAndTransportCapacity(t *testing.T) {
	p := fixturePool(t, "", []string{}...)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Refresh(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		endpoint, err := parseEndpoint(fmt.Sprintf("http://8.8.8.8:%d", i+1))
		if err != nil {
			t.Fatal(err)
		}
		_, release, err := p.lease(&http.Transport{}, endpoint)
		if err != nil {
			t.Fatal(err)
		}
		release()
	}
	p.mu.Lock()
	count := len(p.transports)
	p.mu.Unlock()
	if count > 128 {
		t.Fatal("unbounded transports", count)
	}
}

func TestExpiredOrMalformedSnapshotIsNotPublished(t *testing.T) {
	for _, raw := range []string{`{"invalid":true}`, `[{"url":"http://127.0.0.1:80","checkedAt":"2026-10-23T00:00:00Z"}]`} {
		store := &memoryStore{values: map[string]string{snapshotKey: raw}}
		p, err := New(context.Background(), DefaultConfig(), store)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.nodes) != 0 {
			t.Fatal("unvalidated snapshot restored")
		}
		p.Close()
	}
	rows := []node{{URL: "http://8.8.8.8:80", CheckedAt: time.Now().Add(-31 * time.Minute)}}
	raw, _ := json.Marshal(rows)
	p, err := New(context.Background(), DefaultConfig(), &memoryStore{values: map[string]string{snapshotKey: string(raw)}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if len(p.nodes) != 0 {
		t.Fatal("expired snapshot restored")
	}
}
