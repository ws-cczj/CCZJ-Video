package outbound

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/netaddress"
	"cczjVideo/app/parallel"
)

// Refresh downloads bounded source lists and validates a finite batch of candidates.
// All work completes before return; the caller owns scheduling and cancellation.
func (p *Pool) Refresh(ctx context.Context) (resultErr error) {
	if !p.refreshMu.TryLock() {
		return nil
	}
	defer p.refreshMu.Unlock()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	now := time.Now()
	due := !now.Before(p.nextProbe)
	for _, source := range p.sources {
		if source.config.Enabled && source.status.NextAttempt <= now.Unix() {
			due = true
		}
	}
	if !due {
		return nil
	}
	p.nextProbe = now.Add(time.Duration(p.config.RefreshMinutes) * time.Minute)
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return context.Canceled
	}
	p.status.Refreshing = true
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.status.Refreshing = false
		p.status.LastRefresh = time.Now().Unix()
		p.status.LastError = ""
		if resultErr != nil {
			p.status.LastError = "代理源更新或节点验证失败，保留尚未过期的节点"
		}
		p.mu.Unlock()
		if resultErr != nil && !errors.Is(resultErr, context.Canceled) {
			applog.Warn("[OutboundProxy] 代理池更新失败，保留尚未过期节点并按配置使用本机代理")
		}
	}()
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if !p.config.Enabled {
		return nil
	}
	lists := []candidateBatch{}
	seeds := []candidate{}
	for _, raw := range p.config.Seeds {
		u, _ := parseEndpoint(raw)
		seeds = append(seeds, candidate{URL: u.String()})
	}
	if len(seeds) > 0 {
		lists = append(lists, candidateBatch{rows: seeds})
	}
	lists, failures := p.updateSources(ctx, lists)
	rows := p.validationBatch(lists)
	var resultsMu sync.Mutex
	validated := []node{}
	attempted := map[string]bool{}
	err := parallel.Each(ctx, len(rows), 3, func(ctx context.Context, i int) error {
		resultsMu.Lock()
		full := len(validated) >= p.config.MaxHealthy
		resultsMu.Unlock()
		if full {
			return nil
		}
		resultsMu.Lock()
		attempted[rows[i].URL] = true
		resultsMu.Unlock()
		u, _ := url.Parse(rows[i].URL)
		if err := p.probe(ctx, u); err != nil {
			p.failed(u.String(), "", false)
			return nil
		} // A dead candidate is not a failed batch.
		resultsMu.Lock()
		if len(validated) < p.config.MaxHealthy {
			validated = append(validated, node{URL: u.String(), CheckedAt: time.Now()})
		}
		resultsMu.Unlock()
		return nil
	})
	if err != nil {
		return err
	}
	if len(validated) == 0 {
		return errors.Join(append(failures, fmt.Errorf("no healthy proxies in %d candidates", len(rows)))...)
	}
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return context.Canceled
	}
	// Retain quarantines; refreshing a source must not reset failed routes.
	for i := range validated {
		for _, old := range p.nodes {
			if old.URL == validated[i].URL {
				validated[i].blocked = old.blocked
			}
		}
	}
	// Sampling a new source must not evict a different, still fresh healthy
	// node. Its original check time stays intact; failed probes are excluded.
	for _, old := range p.nodes {
		if len(validated) >= p.config.MaxHealthy {
			break
		}
		if !attempted[old.URL] && time.Since(old.CheckedAt) < 30*time.Minute {
			validated = append(validated, old)
		}
	}
	p.nodes = validated
	p.mu.Unlock()
	if p.store != nil {
		raw, err := json.Marshal(validated)
		if err != nil {
			return err
		}
		if err = p.store.Set(snapshotKey, string(raw)); err != nil {
			return fmt.Errorf("save proxy snapshot: %w", err)
		}
	}
	applog.Info("[OutboundProxy] 更新代理池：本轮检测 %d 个候选，保留 %d 个节点", len(attempted), len(validated))
	return nil
}

func (p *Pool) downloadSource(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	private, err := p.classify(ctx, u)
	if err != nil {
		return nil, err
	}
	if private {
		return nil, fmt.Errorf("proxy source resolves to a private address")
	}
	endpoints := []*url.URL{}
	for _, n := range p.candidates("proxy-list-control") {
		u, _ := url.Parse(n.URL)
		endpoints = append(endpoints, u)
	}
	local, err := localProxy(p.config.LocalProxy)
	if err != nil {
		return nil, err
	}
	if local != nil {
		endpoints = append(endpoints, local)
	}
	var failure error = fmt.Errorf("no proxy available to bootstrap source list")
	for _, endpoint := range endpoints {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		transport, release, err := p.lease(p.control, endpoint)
		if err != nil {
			return nil, err
		}
		client := &http.Client{Transport: p.controlTransport(transport), Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
		if err != nil {
			release()
			return nil, err
		}
		response, err := client.Do(req)
		if err != nil {
			release()
			failure = err
			continue
		}
		raw, readErr := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
		response.Body.Close()
		release()
		if response.StatusCode == 200 && readErr == nil && len(raw) <= 2<<20 {
			return raw, nil
		}
		if response.StatusCode != http.StatusOK {
			failure = sourceHTTPError{response.StatusCode}
		} else {
			failure = fmt.Errorf("proxy source invalid size/body")
		}
	}
	return nil, failure
}

func (p *Pool) probe(ctx context.Context, endpoint *url.URL) error {
	transport, release, err := p.lease(p.control, endpoint)
	if err != nil {
		return err
	}
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.probeURL, nil)
	if err != nil {
		return err
	}
	response, err := p.controlTransport(transport).RoundTrip(req)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health probe HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 {
		return fmt.Errorf("invalid health probe body")
	}
	var value struct {
		IP string `json:"ip"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return fmt.Errorf("invalid health probe JSON")
	}
	addr, err := netip.ParseAddr(value.IP)
	if err != nil || !netaddress.IsPublic(addr) {
		return fmt.Errorf("health probe did not return a public exit IP")
	}
	return nil
}
