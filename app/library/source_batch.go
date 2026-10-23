package library

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/netstats"
	"cczjVideo/app/provider"
	"cczjVideo/app/proxy"
)

// BatchSource is immutable after parsing. Full-reading and search-only probes
// are separate capabilities; only the former can produce content quality scores.
type BatchSource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	URL       string `json:"url"`
	Language  string `json:"language"`
	CanRead   bool   `json:"canRead"`
	CanSearch bool   `json:"canSearch"`
	Reason    string `json:"reason,omitempty"`
	doc       *provider.Document
	searchDoc *provider.Document
	document  string
}

func ParseBatchSources(raw []byte, kind, origin string) ([]*BatchSource, error) {
	if len(raw) > maxCatalogBytes {
		return nil, fmt.Errorf("catalog exceeds 32 MiB")
	}
	rows, err := parseSourceCatalog(raw, kind, origin)
	if err != nil {
		return nil, err
	}
	out := make([]*BatchSource, 0, len(rows))
	for _, row := range rows {
		b := &BatchSource{ID: row.ID, Name: row.Name, Kind: row.Kind, URL: row.ApiURL, Language: row.language, CanRead: row.Compatible, Reason: row.Reason, searchDoc: row.searchProbe, document: row.document}
		if b.Language == "" {
			b.Language = "zh"
		}
		if row.Compatible {
			b.doc, err = provider.ParseDocument(row.document, row.Kind)
			if err != nil {
				return nil, err
			}
			b.searchDoc = b.doc
		}
		b.CanSearch = b.searchDoc != nil
		out = append(out, b)
	}
	return out, nil
}

func BundledBatchSources() ([]*BatchSource, error) {
	var out []*BatchSource
	for _, kind := range []string{"novel", "comic"} {
		rows, err := ParseBatchSources(bundledSourceCatalog, kind, "bundled-adapters")
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

// InterleaveOrigins avoids filling all five workers with aliases of one host.
// It is O(S) and leaves the original source identity and per-rule result intact.
func InterleaveOrigins(rows []*BatchSource) []*BatchSource {
	groups := map[string][]*BatchSource{}
	keys := []string{}
	for _, r := range rows {
		key, _ := probeOrigin(r.URL)
		if key == "" {
			key = r.ID
		}
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], r)
	}
	out := make([]*BatchSource, 0, len(rows))
	positions := map[string]int{}
	for len(out) < len(rows) {
		next := keys[:0]
		for _, key := range keys {
			i := positions[key]
			if i < len(groups[key]) {
				out = append(out, groups[key][i])
				positions[key]++
				if i+1 < len(groups[key]) {
					next = append(next, key)
				}
			}
		}
		keys = next
	}
	return out
}

type EndpointProbe struct {
	Origin     string                    `json:"origin"`
	Status     string                    `json:"status"`
	HTTPStatus int                       `json:"httpStatus,omitempty"`
	Homepage   *quality.HomepageEvidence `json:"homepage,omitempty"`
	Millis     int64                     `json:"millis"`
	Failure    *quality.Failure          `json:"failure,omitempty"`
}
type probeFlight struct {
	done   chan struct{}
	result EndpointProbe
}

type BatchOptions struct {
	ProbeTimeout     time.Duration
	OperationTimeout time.Duration
	Queries          int
	Matches          int
	Delay            time.Duration
	MaxDeepSources   int
	InspectHomepage  bool `json:"inspectHomepage,omitempty"`
	HomepagesOnly    bool `json:"homepagesOnly,omitempty"`
}
type BatchResult struct {
	Source          *BatchSource            `json:"source"`
	State           string                  `json:"state"`
	Probe           EndpointProbe           `json:"probe"`
	Discovery       *quality.Discovery      `json:"discovery,omitempty"`
	Quality         *quality.Report         `json:"quality,omitempty"`
	Failure         *quality.Failure        `json:"failure,omitempty"`
	Millis          int64                   `json:"millis"`
	HomepageMatches []quality.HomepageMatch `json:"homepageMatches,omitempty"`
}
type BatchNetworkStats struct {
	Requests        int64 `json:"endpointRequests"`
	Reused          int64 `json:"endpointReused"`
	PeakHTTP        int64 `json:"peakEndpointHTTP"`
	DeepStarted     int64 `json:"deepStarted"`
	HomepageLookups int64 `json:"homepageLookups"`
}

// BatchEngine is owned by one batch run. Workers execute network calls serially;
// there is no internal source fanout, so Dispatch's five-worker cap is global
// across novel/comic probes, searches, directory reads and image checks.
type BatchEngine struct {
	fetcher     *provider.Fetcher
	limits      provider.Limits
	gate        *quality.NetworkGate
	index       *quality.Index
	opts        BatchOptions
	client      *http.Client
	transport   *http.Transport
	mu          sync.Mutex
	probes      map[string]*probeFlight
	requests    atomic.Int64
	reused      atomic.Int64
	active      atomic.Int64
	peak        atomic.Int64
	deep        atomic.Int64
	homeLookups atomic.Int64
}

func NewBatchEngine(index *quality.Index, opts BatchOptions) (*BatchEngine, error) {
	return newBatchEngine(index, opts, nil)
}
func newBatchEngine(index *quality.Index, opts BatchOptions, gate *quality.NetworkGate) (*BatchEngine, error) {
	if index == nil || opts.ProbeTimeout <= 0 || opts.OperationTimeout <= 0 || opts.Queries < 1 || opts.Queries > 64 || opts.Matches < 1 || opts.Matches > 20 || opts.MaxDeepSources < 0 || opts.Delay < 0 {
		return nil, fmt.Errorf("invalid batch options")
	}
	if gate == nil {
		gate = quality.NewNetworkGate()
	}
	fetcher := provider.NewFetcher()
	if err := fetcher.WrapTransport(gate.Wrap); err != nil {
		fetcher.Close()
		return nil, err
	}
	t := &http.Transport{DialContext: proxy.SafeDialContext, MaxIdleConns: 16, MaxIdleConnsPerHost: 1, TLSHandshakeTimeout: opts.ProbeTimeout, IdleConnTimeout: 15 * time.Second}
	c := &http.Client{Transport: gate.Wrap(netpolicy.WrapTransport(string(netstats.CategoryReading), netstats.WrapTransport(netstats.CategoryReading, t))), Timeout: opts.ProbeTimeout, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	return &BatchEngine{fetcher: fetcher, limits: provider.DefaultLimits(), gate: gate, index: index, opts: opts, client: c, transport: t, probes: map[string]*probeFlight{}}, nil
}
func (e *BatchEngine) Close()                          { e.fetcher.Close(); e.transport.CloseIdleConnections() }
func (e *BatchEngine) NetworkStats() quality.GateStats { return e.gate.Stats() }
func (e *BatchEngine) Stats() BatchNetworkStats {
	return BatchNetworkStats{e.requests.Load(), e.reused.Load(), e.peak.Load(), e.deep.Load(), e.homeLookups.Load()}
}
func probeOrigin(address string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(address))
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return "", fmt.Errorf("invalid HTTP(S) origin")
	}
	return strings.ToLower(u.Scheme + "://" + u.Host), nil
}
func batchFailure(stage string, err error) *quality.Failure {
	if errors.Is(err, context.DeadlineExceeded) {
		return &quality.Failure{Stage: stage, Code: "TIMEOUT", Message: "探测超时，状态待复核"}
	}
	if errors.Is(err, context.Canceled) {
		return &quality.Failure{Stage: stage, Code: "CANCELLED", Message: "操作已取消"}
	}
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return &quality.Failure{Stage: stage, Code: "DNS_ERROR", Message: "域名解析失败"}
	}
	var cert *tls.CertificateVerificationError
	if errors.As(err, &cert) {
		return &quality.Failure{Stage: stage, Code: "TLS_ERROR", Message: "TLS证书校验失败"}
	}
	p := apperror.Public(err)
	if p.Code == apperror.Internal {
		p.Code = apperror.Unavailable
		p.Message = "网络请求失败，状态待复核"
	}
	return &quality.Failure{Stage: stage, Code: string(p.Code), Message: p.Message}
}

func (e *BatchEngine) probe(ctx context.Context, address string) EndpointProbe {
	origin, err := probeOrigin(address)
	if err != nil {
		return EndpointProbe{Status: "invalid_address", Failure: &quality.Failure{Stage: "endpoint", Code: "VALIDATION", Message: "没有可探测的 HTTP(S) 站点地址"}}
	}
	e.mu.Lock()
	flight, found := e.probes[origin]
	if found {
		e.reused.Add(1)
		e.mu.Unlock()
		select {
		case <-ctx.Done():
			return EndpointProbe{Origin: origin, Status: "cancelled", Failure: batchFailure("endpoint", ctx.Err())}
		case <-flight.done:
			return flight.result
		}
	}
	flight = &probeFlight{done: make(chan struct{})}
	e.probes[origin] = flight
	e.mu.Unlock()
	start := time.Now()
	probe := EndpointProbe{Origin: origin, Status: "network_unknown"}
	call, cancel := context.WithTimeout(ctx, e.opts.ProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(call, "GET", origin+"/", nil)
	if err == nil {
		req.Header.Set("User-Agent", "CCZJ-Reading-Source-Audit/1.0")
		e.requests.Add(1)
		active := e.active.Add(1)
		for old := e.peak.Load(); active > old; old = e.peak.Load() {
			if e.peak.CompareAndSwap(old, active) {
				break
			}
		}
		resp, callErr := e.client.Do(req)
		if callErr != nil {
			err = callErr
		} else {
			probe.HTTPStatus = resp.StatusCode
			probe.Status = "http_reached"
			// A root HTTP status only proves an endpoint responded. Even 200 does
			// not prove its search/chapters work; no content score is assigned here.
			var readErr error
			if e.opts.InspectHomepage && resp.StatusCode >= 200 && resp.StatusCode < 300 && strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "html") {
				var home quality.HomepageEvidence
				home, readErr = quality.MatchHomepage(resp.Body, resp.Request.URL.String(), e.index)
				if readErr == nil {
					probe.Homepage = &home
					e.homeLookups.Add(int64(home.Lookups))
				}
			} else {
				_, readErr = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			}
			closeErr := resp.Body.Close()
			if readErr != nil {
				err = readErr
			} else if closeErr != nil {
				err = closeErr
			}
		}
		e.active.Add(-1)
	}
	if err != nil {
		probe.Failure = batchFailure("endpoint", err)
		if call.Err() != nil {
			probe.Failure = batchFailure("endpoint", call.Err())
		}
		probe.Status = "network_unknown"
	}
	probe.Millis = time.Since(start).Milliseconds()
	flight.result = probe
	close(flight.done)
	if errors.Is(err, context.Canceled) {
		e.mu.Lock()
		if e.probes[origin] == flight {
			delete(e.probes, origin)
		}
		e.mu.Unlock()
	}
	return probe
}

// Execute performs admission, indexed discovery and optional deep sampling.
// Unsupported rule bodies remain visible after a real endpoint attempt.
func (e *BatchEngine) Execute(ctx context.Context, source *BatchSource) BatchResult {
	return e.execute(ctx, source, true)
}
func (e *BatchEngine) execute(ctx context.Context, source *BatchSource, deep bool) BatchResult {
	start := time.Now()
	result := BatchResult{Source: source, State: "adapter_required"}
	result.Probe = e.probe(ctx, source.URL)
	if result.Probe.Homepage != nil {
		for _, match := range result.Probe.Homepage.Matches {
			if match.Kind == source.Kind {
				result.HomepageMatches = append(result.HomepageMatches, match)
			}
		}
	}
	finish := func() BatchResult { result.Millis = time.Since(start).Milliseconds(); return result }
	if ctx.Err() != nil {
		result.State = "cancelled"
		result.Failure = batchFailure("dispatch", ctx.Err())
		return finish()
	}
	if e.opts.HomepagesOnly {
		result.State = "homepage_unavailable"
		if result.Probe.Status == "http_reached" {
			result.State = "homepage_no_html"
		}
		if result.Probe.Homepage != nil {
			result.State = "homepage_inspected"
		}
		if len(result.HomepageMatches) > 0 {
			result.State = "homepage_candidate"
		}
		return finish()
	}
	if !source.CanSearch {
		if len(result.HomepageMatches) > 0 {
			result.State = "homepage_candidate"
		}
		if result.Probe.Status == "invalid_address" {
			result.State = "invalid_address"
		}
		return finish()
	}
	// Unreachable endpoints get a short-pass result, not dozens of identical
	// timeout requests. API-specific adapters may still use a different root.
	if result.Probe.Status == "network_unknown" && !source.CanRead {
		result.State = "network_unknown"
		return finish()
	}
	reader := provider.NewPipeline(source.searchDoc, e.fetcher.ScopeMode(source.ID, source.Kind), e.limits)
	bounded := &operationBoundReader{Reader: reader, timeout: e.opts.OperationTimeout}
	discovery, err := quality.Discover(ctx, bounded, quality.Source{ID: source.ID, Name: source.Name, Kind: source.Kind, URL: source.URL}, e.index, source.Language, source.searchDoc.CheckKeyword, e.opts.Queries, e.opts.Matches, e.opts.Delay)
	result.Discovery = &discovery
	if err != nil {
		result.State = "discovery_unfinished"
		result.Failure = batchFailure("discovery", err)
		return finish()
	}
	result.State = "no_identity_match"
	if len(discovery.Hits) == 0 {
		return finish()
	}
	result.State = "search_matched"
	if !source.CanRead || !deep {
		return finish()
	}
	for {
		used := e.deep.Load()
		if used >= int64(e.opts.MaxDeepSources) {
			result.State = "deep_budget_exhausted"
			return finish()
		}
		if e.deep.CompareAndSwap(used, used+1) {
			break
		}
	}
	probe, closeProbe := quality.NewImageProbeWithGate(source.doc.Headers, e.gate)
	defer closeProbe()
	report, err := quality.EvaluateHits(ctx, bounded, quality.Source{ID: source.ID, Name: source.Name, Kind: source.Kind, URL: source.URL}, e.index, discovery, quality.Options{Delay: e.opts.Delay}, probe)
	if err != nil {
		result.State = "deep_unfinished"
		result.Failure = batchFailure("reading", err)
		return finish()
	}
	result.Quality = report
	result.State = "deep_checked"
	return finish()
}

// operationBoundReader gives failed endpoints a short per-operation deadline
// while keeping the outer source-run cancellation as the owner of all I/O.
type operationBoundReader struct {
	quality.Reader
	timeout time.Duration
}

func (r *operationBoundReader) Search(ctx context.Context, b, q string, p int, n string) (*provider.ItemPage, error) {
	call, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return r.Reader.Search(call, b, q, p, n)
}
