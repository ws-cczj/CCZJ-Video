// Package netbudget owns instance-scoped upstream concurrency. Permits cover
// response bodies, including streaming playback, rather than only response headers.
package netbudget

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/lifecycle"
)

type Policy struct {
	Total, PerHost, PlaybackReserve, HostPlaybackReserve, Waiting int
	Lanes                                                         map[string]int
}

func DefaultPolicy() Policy {
	return Policy{Total: 24, PerHost: 8, PlaybackReserve: 4, HostPlaybackReserve: 2, Waiting: 128, Lanes: map[string]int{"playback": 12, "reading": 8, "images": 8, "download": 12, "danmaku": 3}}
}

type Snapshot struct {
	Active           int            `json:"active"`
	Waiting          int            `json:"waiting"`
	Rejected         uint64         `json:"rejected"`
	WaitMilliseconds uint64         `json:"waitMilliseconds"`
	Lanes            map[string]int `json:"lanes"`
}
type Budget struct {
	mu               sync.Mutex
	owner            context.Context
	policy           Policy
	active, waiting  int
	hosts, lanes     map[string]int
	changed          chan struct{}
	rejected, waitMS uint64
}

func New(owner context.Context, policy Policy) *Budget {
	if policy.Total < 1 {
		policy.Total = 1
	}
	if policy.PerHost < 1 {
		policy.PerHost = 1
	}
	if policy.Waiting < 0 {
		policy.Waiting = 0
	}
	if policy.PlaybackReserve < 0 || policy.PlaybackReserve >= policy.Total {
		policy.PlaybackReserve = 0
	}
	if policy.HostPlaybackReserve < 0 || policy.HostPlaybackReserve >= policy.PerHost {
		policy.HostPlaybackReserve = 0
	}
	limits := make(map[string]int)
	for lane, limit := range policy.Lanes {
		if limit > 0 {
			limits[lane] = limit
		}
	}
	policy.Lanes = limits
	return &Budget{owner: owner, policy: policy, hosts: make(map[string]int), lanes: make(map[string]int), changed: make(chan struct{})}
}
func (b *Budget) acquire(ctx context.Context, lane, host string) (func(), error) {
	started := time.Now()
	waiting := false
	defer func() {
		if waiting {
			b.mu.Lock()
			b.waiting--
			b.waitMS += uint64(time.Since(started).Milliseconds())
			b.mu.Unlock()
		}
	}()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		b.mu.Lock()
		total, perHost := b.policy.Total, b.policy.PerHost
		if lane != "playback" {
			total -= b.policy.PlaybackReserve
			perHost -= b.policy.HostPlaybackReserve
		}
		limit := b.policy.Lanes[lane]
		if limit == 0 {
			limit = total
		}
		if b.active < total && b.hosts[host] < perHost && b.lanes[lane] < limit {
			b.active++
			b.hosts[host]++
			b.lanes[lane]++
			b.mu.Unlock()
			var once sync.Once
			return func() {
				once.Do(func() {
					b.mu.Lock()
					b.active--
					b.hosts[host]--
					b.lanes[lane]--
					if b.hosts[host] == 0 {
						delete(b.hosts, host)
					}
					if b.lanes[lane] == 0 {
						delete(b.lanes, lane)
					}
					close(b.changed)
					b.changed = make(chan struct{})
					b.mu.Unlock()
				})
			}, nil
		}
		if !waiting {
			if b.waiting >= b.policy.Waiting {
				b.rejected++
				b.mu.Unlock()
				return nil, apperror.New(apperror.Unavailable, "网络等待队列已满，请稍后重试")
			}
			b.waiting++
			waiting = true
		}
		changed := b.changed
		b.mu.Unlock()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}
func (b *Budget) Snapshot() Snapshot {
	b.mu.Lock()
	defer b.mu.Unlock()
	lanes := make(map[string]int, len(b.lanes))
	for lane, count := range b.lanes {
		lanes[lane] = count
	}
	return Snapshot{Active: b.active, Waiting: b.waiting, Rejected: b.rejected, WaitMilliseconds: b.waitMS, Lanes: lanes}
}
func (b *Budget) Wrap(lane string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{budget: b, lane: lane, base: base}
}

type transport struct {
	budget *Budget
	lane   string
	base   http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx, cancel := lifecycle.WithShutdown(req.Context(), t.budget.owner)
	release, err := t.budget.acquire(ctx, t.lane, strings.ToLower(req.URL.Hostname()))
	if err != nil {
		cancel()
		return nil, err
	}
	resp, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		release()
		cancel()
		return resp, err
	}
	if resp.Body == nil {
		release()
		cancel()
		return resp, nil
	}
	resp.Body = &body{ReadCloser: resp.Body, release: func() { release(); cancel() }}
	return resp, nil
}
func (t *transport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type body struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *body) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil {
		b.once.Do(b.release)
	}
	return n, err
}
func (b *body) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }
