package outbound

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/operation"
	"golang.org/x/net/publicsuffix"
)

type routeTransport struct {
	base     http.RoundTripper
	decorate func(http.RoundTripper) http.RoundTripper
	category string
}

// WrapTransport retains the caller's transport settings and per-attempt accounting.
func WrapTransport(category string, base http.RoundTripper, decorate func(http.RoundTripper) http.RoundTripper) http.RoundTripper {
	// Keep a media session on its caller-owned transport. Pool readiness and
	// node/site cooldowns must not delay playback or change the egress between
	// a playlist, its keys and segments, probes, and a resumed download.
	switch category {
	case "playback", "lineprobe", "download":
		return decorate(base)
	}
	return &routeTransport{base: base, decorate: decorate, category: category}
}

func (t *routeTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
	if p := installed.Load(); p != nil {
		if base, ok := t.base.(*http.Transport); ok {
			p.closeBase(base)
		}
	}
}
func (t *routeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p := installed.Load()
	if p == nil || !p.config.Enabled {
		return t.decorate(t.base).RoundTrip(req)
	}
	ctx, cancel := p.requestContext(req.Context())
	response, err := t.route(p, req.Clone(ctx))
	if err != nil {
		cancel()
		return nil, err
	}
	if response.Body == nil {
		cancel()
	} else {
		response.Body = &releasedBody{ReadCloser: response.Body, release: cancel}
	}
	return response, nil
}

func (t *routeTransport) route(p *Pool, req *http.Request) (*http.Response, error) {
	if req.Body != nil && req.GetBody != nil {
		defer req.Body.Close()
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	private, err := p.classify(req.Context(), req.URL)
	if err != nil {
		return nil, err
	}
	if private {
		if base, ok := t.base.(*http.Transport); ok {
			return t.direct(p, base, req)
		}
		return t.decorate(t.base).RoundTrip(req)
	}
	if err := p.waitReady(req.Context()); err != nil {
		return nil, err
	}
	base, ok := t.base.(*http.Transport)
	if !ok {
		return nil, apperror.New(apperror.Unsupported, "该出站传输未支持公共代理")
	}
	site := req.URL.Hostname()
	if domain, err := publicsuffix.EffectiveTLDPlusOne(site); err == nil {
		site = domain
	}
	var lastErr error
	for _, n := range p.candidates(site) {
		endpoint, _ := url.Parse(n.URL)
		p.mu.Lock()
		p.status.PublicAttempts++
		p.mu.Unlock()
		response, err, wrote, connected := t.attempt(p, base, endpoint, req, "public_proxy")
		if err == nil && !retryStatus(response.StatusCode) {
			return response, nil
		}
		if req.Context().Err() != nil {
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			return nil, req.Context().Err()
		}
		// A reachable proxy can still be refused or reset by this destination.
		// Do not evict it from unrelated sites' in-flight routing in that case.
		p.failed(n.URL, site, err == nil || connected)
		if !canReplay(req, t.category, wrote) {
			return response, err
		}
		if response != nil {
			response.Body.Close()
			lastErr = apperror.New(apperror.Unavailable, "公共代理未能访问目标站点")
		} else {
			lastErr = err
		}
	}
	endpoint, err := localProxy(p.config.LocalProxy)
	if err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "本机代理配置无效")
	}
	if endpoint == nil {
		return nil, apperror.Wrap(apperror.Unavailable, lastErr, "公共代理池不可用，且未配置本机代理")
	}
	p.mu.Lock()
	p.status.LocalFallbacks++
	p.mu.Unlock()
	response, err, _, _ := t.attempt(p, base, endpoint, req, "local_proxy")
	return response, err
}

func (t *routeTransport) direct(p *Pool, base *http.Transport, req *http.Request) (*http.Response, error) {
	transport, release, err := p.lease(base, nil)
	if err != nil {
		return nil, err
	}
	response, err := t.decorate(transport).RoundTrip(req)
	if err != nil {
		release()
		return response, err
	}
	if response.Body == nil {
		release()
	} else {
		response.Body = &releasedBody{ReadCloser: response.Body, release: release}
	}
	return response, nil
}

func (t *routeTransport) attempt(p *Pool, base *http.Transport, endpoint *url.URL, req *http.Request, route string) (result *http.Response, failure error, sent, connected bool) {
	began := time.Now()
	defer func() {
		if _, traced := operation.IdentityFromContext(req.Context()); !traced {
			return
		}
		fields := applog.Fields{"route": route, "category": t.category, "proxy_sha256": applog.Fingerprint([]byte(endpoint.String())), "duration_ms": time.Since(began).Milliseconds(), "url": applog.SafeURL(req.URL.String())}
		fields["proxy_connected"], fields["request_sent"] = connected, sent
		if result != nil {
			fields["http_status"] = result.StatusCode
		}
		if failure != nil {
			fields["code"], fields["error"] = apperror.Public(failure).Code, failure.Error()
		}
		operation.Stage(req.Context(), "outbound_attempt", fields)
	}()
	transport, release, err := p.lease(base, endpoint)
	if err != nil {
		return nil, err, false, false
	}
	copy := req.Clone(req.Context())
	if req.Body != nil && req.GetBody != nil {
		copy.Body, err = req.GetBody()
		if err != nil {
			release()
			return nil, err, false, false
		}
	}
	var wrote atomic.Bool
	var reached atomic.Bool
	copy = copy.WithContext(httptrace.WithClientTrace(copy.Context(), &httptrace.ClientTrace{
		ConnectDone: func(_, _ string, err error) {
			if err == nil {
				reached.Store(true)
			}
		},
		GotConn:      func(httptrace.GotConnInfo) { reached.Store(true) },
		WroteHeaders: func() { wrote.Store(true) },
		WroteRequest: func(httptrace.WroteRequestInfo) { wrote.Store(true) },
	}))
	response, err := t.decorate(transport).RoundTrip(copy)
	if err != nil {
		release()
		return response, err, wrote.Load(), reached.Load()
	}
	if response.Body == nil {
		release()
	} else {
		response.Body = &releasedBody{ReadCloser: response.Body, release: release}
	}
	return response, nil, wrote.Load(), reached.Load()
}

type releasedBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *releasedBody) Read(data []byte) (int, error) {
	n, err := b.ReadCloser.Read(data)
	if errors.Is(err, io.EOF) {
		b.once.Do(b.release)
	}
	return n, err
}
func (b *releasedBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }
func routeScore(site, endpoint string) uint64 {
	sum := sha256.Sum256([]byte(site + "|" + endpoint))
	return binary.BigEndian.Uint64(sum[:8])
}
func retryStatus(status int) bool {
	return status == 403 || status == 407 || status == 429 || status == 502 || status == 503 || status == 504
}
func canReplay(req *http.Request, category string, wrote bool) bool {
	if req.Body != nil && req.GetBody == nil {
		return false
	}
	if !wrote || req.Method == http.MethodGet || req.Method == http.MethodHead {
		return true
	}
	return req.Method == http.MethodPost && (category == "collect" || category == "danmaku" || category == "reading" || category == "novel" || category == "comic")
}
