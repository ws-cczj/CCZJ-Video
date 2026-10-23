package quality

import (
	"io"
	"net/http"
	"sync"
	"sync/atomic"
)

// NetworkGate bounds complete response lifetimes, not merely header receipt.
// The same instance must wrap endpoint, provider and image transports.
type NetworkGate struct {
	slots                  chan struct{}
	active, peak, requests atomic.Int64
}
type GateStats struct {
	Capacity     int   `json:"capacity"`
	Requests     int64 `json:"requests"`
	PeakInFlight int64 `json:"peakInFlight"`
}

func NewNetworkGate() *NetworkGate { return &NetworkGate{slots: make(chan struct{}, MaxChannels)} }
func (g *NetworkGate) Stats() GateStats {
	return GateStats{cap(g.slots), g.requests.Load(), g.peak.Load()}
}
func (g *NetworkGate) Wrap(base http.RoundTripper) http.RoundTripper { return &gatedTransport{g, base} }

type gatedTransport struct {
	gate *NetworkGate
	base http.RoundTripper
}

func (t *gatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case t.gate.slots <- struct{}{}:
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	n := t.gate.active.Add(1)
	for old := t.gate.peak.Load(); n > old; old = t.gate.peak.Load() {
		if t.gate.peak.CompareAndSwap(old, n) {
			break
		}
	}
	t.gate.requests.Add(1)
	release := func() { t.gate.active.Add(-1); <-t.gate.slots }
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		release()
		return nil, err
	}
	if resp.Body == nil {
		release()
	} else {
		resp.Body = &gatedBody{ReadCloser: resp.Body, release: release}
	}
	return resp, nil
}
func (t *gatedTransport) CloseIdleConnections() {
	if c, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

type gatedBody struct {
	io.ReadCloser
	once    sync.Once
	release func()
}

func (b *gatedBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }
