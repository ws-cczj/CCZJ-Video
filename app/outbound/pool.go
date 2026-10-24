package outbound

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netaddress"
	"cczjVideo/app/netbudget"
)

// Store is satisfied by the existing settings service; no separate database is needed.
type Store interface {
	Get(string) (string, error)
	Set(string, string) error
}
type node struct {
	URL       string    `json:"url"`
	CheckedAt time.Time `json:"checkedAt"`
	blocked   time.Time
}
type transportKey struct {
	base     *http.Transport
	endpoint string
}
type cachedTransport struct {
	transport *http.Transport
	used      uint64
	refs      int
	removed   bool
}

// Status reports health and routing without exposing proxy credentials or request URLs.
type Status struct {
	Enabled        bool           `json:"enabled"`
	Ready          bool           `json:"ready"`
	Healthy        int            `json:"healthy"`
	Refreshing     bool           `json:"refreshing"`
	LastRefresh    int64          `json:"lastRefresh"`
	LastError      string         `json:"lastError"`
	PublicAttempts uint64         `json:"publicAttempts"`
	LocalFallbacks uint64         `json:"localFallbacks"`
	Sources        []SourceStatus `json:"sources"`
}

// Pool owns the bounded node set, transport cache and refresh state.
type Pool struct {
	mu           sync.Mutex
	refreshMu    sync.Mutex
	config       Config
	owner        context.Context
	store        Store
	nodes        []node
	targetBlocks map[string]time.Time
	transports   map[transportKey]*cachedTransport
	sequence     uint64
	ready        chan struct{}
	readyOnce    sync.Once
	closed       bool
	status       Status
	sources      []*sourceState
	nextProbe    time.Time
	batchCursor  int
	lookup       func(context.Context, string) ([]netip.Addr, error)
	dialProxy    func(context.Context, string, string) (net.Conn, error)
	// Test seams still execute real TLS/CONNECT and HTTP routing.
	probeURL      string
	control       *http.Transport
	controlBudget *netbudget.Budget
}

var installed atomic.Pointer[Pool]

// SetControlBudget connects list updates and health probes to the shared budget.
// Call before Install or Run, as with other transport configuration.
func (p *Pool) SetControlBudget(budget *netbudget.Budget) { p.controlBudget = budget }

func (p *Pool) controlTransport(base http.RoundTripper) http.RoundTripper {
	if p.controlBudget != nil {
		return p.controlBudget.Wrap("proxy-pool", base)
	}
	return base
}

// Install connects existing HTTP clients at request time, including package-level clients.
func Install(pool *Pool) { installed.Store(pool) }

// New validates configuration and restores a bounded recent snapshot.
func New(owner context.Context, c Config, store Store) (*Pool, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	c, err = DecodeConfig(string(raw))
	if err != nil {
		return nil, err
	}
	p := &Pool{config: c, owner: owner, store: store, ready: make(chan struct{}), targetBlocks: map[string]time.Time{}, transports: map[transportKey]*cachedTransport{}, probeURL: "https://api.ipify.org?format=json", control: &http.Transport{TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 8 * time.Second, IdleConnTimeout: 30 * time.Second, MaxIdleConns: 8, MaxIdleConnsPerHost: 2}}
	p.lookup = func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}
	p.status.Enabled = c.Enabled
	for _, source := range c.sourceConfigs() {
		state := "pending"
		if !source.Enabled {
			state = "disabled"
		}
		p.sources = append(p.sources, &sourceState{config: source, status: SourceStatus{ID: source.ID, Enabled: source.Enabled, State: state}})
	}
	p.dialProxy = (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	if store != nil {
		saved, readErr := store.Get(snapshotKey)
		if readErr != nil {
			return nil, fmt.Errorf("read proxy snapshot: %w", readErr)
		}
		if len(saved) > 0 && len(saved) <= 65536 {
			var nodes []node
			if json.Unmarshal([]byte(saved), &nodes) == nil && len(nodes) <= 16 {
				for _, n := range nodes {
					if _, err := parseEndpoint(n.URL); err == nil && !n.CheckedAt.After(time.Now()) && time.Since(n.CheckedAt) < 30*time.Minute {
						p.nodes = append(p.nodes, n)
						if len(p.nodes) >= c.MaxHealthy {
							break
						}
					}
				}
			}
		}
	}
	return p, nil
}

// Run refreshes immediately, then periodically, on the application's lifecycle group.
func (p *Pool) Run(ctx context.Context) {
	defer p.readyOnce.Do(func() { close(p.ready) })
	if !p.config.Enabled {
		p.readyOnce.Do(func() { close(p.ready) })
		return
	}
	_ = p.Refresh(ctx) // Refresh records the classified failure for diagnostics.
	p.readyOnce.Do(func() { close(p.ready) })
	// The clock only checks due times; individual sources own their cadence and
	// failure backoff. Health checks retain the configured global interval.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = p.Refresh(ctx)
		}
	}
}

// Snapshot returns a detached, bounded health view.
func (p *Pool) Snapshot() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	s := p.status
	s.Sources = make([]SourceStatus, 0, len(p.sources))
	for _, source := range p.sources {
		s.Sources = append(s.Sources, source.status)
	}
	select {
	case <-p.ready:
		s.Ready = true
	default:
	}
	for _, n := range p.nodes {
		if time.Since(n.CheckedAt) < 30*time.Minute && !time.Now().Before(n.blocked) {
			s.Healthy++
		}
	}
	return s
}

// Close stops admission and releases all cloned idle transports after owner cancellation.
func (p *Pool) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	for key, t := range p.transports {
		t.removed = true
		t.transport.CloseIdleConnections()
		delete(p.transports, key)
	}
	p.control.CloseIdleConnections()
}

func (p *Pool) closeBase(base *http.Transport) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, t := range p.transports {
		if k.base == base {
			t.transport.CloseIdleConnections()
		}
	}
}

func (p *Pool) classify(ctx context.Context, u *url.URL) (bool, error) {
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true, nil
	}
	addresses, err := p.lookup(ctx, host)
	if err != nil {
		return false, apperror.Wrap(apperror.Unavailable, err, "解析出站目标失败")
	}
	if len(addresses) == 0 {
		return false, apperror.New(apperror.Unavailable, "出站目标没有可用地址")
	}
	public, private := false, false
	for _, addr := range addresses {
		if netaddress.IsPublic(addr) {
			public = true
		} else if addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || isCGNAT(addr) {
			private = true
		} else {
			return false, apperror.New(apperror.PermissionDenied, "出站目标地址不可用")
		}
	}
	if public && private {
		return false, apperror.New(apperror.PermissionDenied, "出站目标同时解析为公网和内网地址")
	}
	return private, nil
}
func isCGNAT(addr netip.Addr) bool {
	return netip.MustParsePrefix("100.64.0.0/10").Contains(addr.Unmap())
}

func (p *Pool) candidates(site string) []node {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, until := range p.targetBlocks {
		if !now.Before(until) {
			delete(p.targetBlocks, k)
		}
	}
	rows := []node{}
	for _, n := range p.nodes {
		if now.Sub(n.CheckedAt) < 30*time.Minute && !now.Before(n.blocked) && !now.Before(p.targetBlocks[site+"|"+n.URL]) {
			rows = append(rows, n)
		}
	}
	// Stable ordering gives a site a stable exit until that node fails.
	sort.Slice(rows, func(i, j int) bool { return routeScore(site, rows[i].URL) > routeScore(site, rows[j].URL) })
	return rows
}

func (p *Pool) failed(endpoint, site string, remote bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if remote {
		if len(p.targetBlocks) >= 512 {
			for k := range p.targetBlocks {
				delete(p.targetBlocks, k)
				break
			}
		}
		p.targetBlocks[site+"|"+endpoint] = time.Now().Add(5 * time.Minute)
		return
	}
	for i := range p.nodes {
		if p.nodes[i].URL == endpoint {
			p.nodes[i].blocked = time.Now().Add(5 * time.Minute)
		}
	}
}

func (p *Pool) lease(base *http.Transport, endpoint *url.URL) (*http.Transport, func(), error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed || p.owner.Err() != nil {
		return nil, nil, context.Canceled
	}
	endpointKey := ""
	if endpoint != nil {
		endpointKey = endpoint.String()
	}
	key := transportKey{base, endpointKey}
	t := p.transports[key]
	if t == nil {
		if len(p.transports) >= 128 {
			var victim transportKey
			var least *cachedTransport
			for k, v := range p.transports {
				if least == nil || v.used < least.used {
					victim, least = k, v
				}
			}
			least.removed = true
			least.transport.CloseIdleConnections()
			delete(p.transports, victim)
		}
		clone := base.Clone()
		// Clone initializes ALPN on the original transport. Preserve its protocol
		// set before adding a custom dialer, which otherwise disables HTTP/2 in Go.
		if clone.Protocols == nil && base.TLSNextProto["h2"] != nil {
			clone.Protocols = new(http.Protocols)
			clone.Protocols.SetHTTP1(true)
			clone.Protocols.SetHTTP2(true)
		}
		clone.Proxy = nil
		// The proxy endpoint is explicit. Validate target addresses separately;
		// SafeDialContext would otherwise reject a configured loopback fallback.
		if endpoint != nil {
			clone.Proxy = http.ProxyURL(endpoint)
			clone.DialContext = p.dialProxy
			clone.DialTLSContext = nil
			clone.DialTLS = nil
		}
		if clone.TLSHandshakeTimeout == 0 || clone.TLSHandshakeTimeout > 8*time.Second {
			clone.TLSHandshakeTimeout = 8 * time.Second
		}
		if clone.ResponseHeaderTimeout == 0 || clone.ResponseHeaderTimeout > 10*time.Second {
			clone.ResponseHeaderTimeout = 10 * time.Second
		}
		if clone.IdleConnTimeout == 0 || clone.IdleConnTimeout > 90*time.Second {
			clone.IdleConnTimeout = 90 * time.Second
		}
		if endpoint != nil && clone.TLSClientConfig != nil && clone.TLSClientConfig.InsecureSkipVerify {
			return nil, nil, apperror.New(apperror.PermissionDenied, "公共代理要求验证 TLS 证书")
		}
		t = &cachedTransport{transport: clone}
		p.transports[key] = t
	}
	p.sequence++
	t.used = p.sequence
	t.refs++
	var once sync.Once
	return t.transport, func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			t.refs--
			if t.removed || p.closed {
				t.transport.CloseIdleConnections()
			}
		})
	}, nil
}

func (p *Pool) waitReady(ctx context.Context) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return context.Canceled
	}
	has := len(p.nodes) > 0
	p.mu.Unlock()
	if has {
		return nil
	}
	select {
	case <-p.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-p.owner.Done():
		return p.owner.Err()
	}
}

func (p *Pool) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return lifecycle.WithShutdown(ctx, p.owner)
}
