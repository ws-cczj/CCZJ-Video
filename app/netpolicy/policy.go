// Package netpolicy provides shared outbound guardrails while preserving the
// existing cadence, cookies, address checks and retry policy of each module.
package netpolicy

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/lifecycle"
)

const maxResponses = 128
const maxResponseBytes = 8 << 20
const maxCaptureBytes = 1 << 20
const maxActiveRequests = 128

type Stat struct {
	Category string `json:"category"`
	Requests uint64 `json:"requests"`
	Reused   uint64 `json:"reused"`
	Blocked  uint64 `json:"blocked"`
}
type entry struct {
	until   time.Time
	reply   *responseCopy
	failure error
	done    chan struct{}
}
type bucket struct {
	tokens float64
	at     time.Time
}
type Manager struct {
	mu                        sync.Mutex
	store                     *Store
	settings                  Settings
	owner                     context.Context
	cancel                    context.CancelFunc
	closed                    bool
	entries                   map[string]*entry
	hosts                     map[string]time.Time
	stats                     map[string]*Stat
	active                    map[uint64]func()
	sequence                  uint64
	wg                        sync.WaitGroup
	bytes, capturing, waiting int
	buckets                   map[string]bucket
	now                       func() time.Time
	jitter                    func(time.Duration) time.Duration
}

func New(store *Store, owner context.Context, settings Settings) *Manager {
	if _, ok := profiles[settings.Frequency]; !ok {
		settings.Frequency = "standard"
	}
	ctx, cancel := context.WithCancel(owner)
	return &Manager{store: store, settings: settings, owner: ctx, cancel: cancel, entries: map[string]*entry{}, hosts: map[string]time.Time{}, stats: map[string]*Stat{}, active: map[uint64]func(){}, buckets: map[string]bucket{}, now: time.Now, jitter: randomJitter}
}
func (m *Manager) SetSettings(settings Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := profiles[settings.Frequency]; ok {
		m.settings = settings
	}
}
func (m *Manager) Initialize(ctx context.Context) error {
	records, err := m.store.loadActive(ctx, m.now())
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for key, until := range records {
		if strings.HasPrefix(key, "host:") {
			m.hosts[key] = until
		} else {
			m.entries[key] = &entry{until: until}
		}
	}
	return nil
}
func (m *Manager) Snapshot(categories map[string]bool) []Stat {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Stat{}
	for category, stat := range m.stats {
		if categories[category] {
			out = append(out, *stat)
		}
	}
	sortStats(out)
	return out
}

var active atomic.Pointer[Manager]

// Install attaches the application-owned manager after SQLite has opened.
// Existing package-level clients resolve it at request time.
func Install(manager *Manager) { active.Store(manager) }

type categoryKey struct{}
type scopeKey struct{}

func WithCategory(ctx context.Context, category string) context.Context {
	return context.WithValue(ctx, categoryKey{}, category)
}
func WithScope(ctx context.Context, scope string) context.Context {
	return context.WithValue(ctx, scopeKey{}, scope)
}

type transport struct {
	category string
	base     http.RoundTripper
}

func WrapTransport(category string, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &transport{category: category, base: base}
}
func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if manager := active.Load(); manager != nil {
		return manager.RoundTrip(t.category, t.base, req)
	}
	return t.base.RoundTrip(req)
}
func (t *transport) CloseIdleConnections() {
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}
func (m *Manager) stat(category string) *Stat {
	if m.stats[category] == nil {
		m.stats[category] = &Stat{Category: category}
	}
	return m.stats[category]
}
func (m *Manager) interval() time.Duration {
	p := profiles[m.settings.Frequency]
	return p.minimum + max(0, min(p.jitter, m.jitter(p.jitter)))
}
func (m *Manager) prune(now time.Time) {
	for key, value := range m.entries {
		if value.done == nil && !value.until.After(now) {
			if value.reply != nil {
				m.bytes -= len(value.reply.body)
			}
			delete(m.entries, key)
		}
	}
	for host, until := range m.hosts {
		if !until.After(now) {
			delete(m.hosts, host)
		}
	}
}

// Burst ceilings sit well above the modules' ordinary request cadence. They
// reject accidental storms without rescheduling successful legacy requests.
func (m *Manager) burstAllowed(host string, now time.Time) bool {
	p := profiles[m.settings.Frequency]
	for key, value := range m.buckets {
		if now.Sub(value.at) > time.Minute {
			delete(m.buckets, key)
		}
	}
	keys := []string{"all", host}
	for _, key := range keys {
		capacity, rate := p.burst, p.rate
		if key == "all" {
			capacity *= 4
			rate *= 4
		}
		value, ok := m.buckets[key]
		if !ok {
			if len(m.buckets) >= 512 {
				return false
			}
			value = bucket{tokens: capacity, at: now}
		}
		value.tokens = min(capacity, value.tokens+max(0, now.Sub(value.at).Seconds())*rate)
		value.at = now
		m.buckets[key] = value
		if value.tokens < 1 {
			return false
		}
	}
	for _, key := range keys {
		value := m.buckets[key]
		value.tokens--
		m.buckets[key] = value
	}
	return true
}
func (m *Manager) RoundTrip(category string, base http.RoundTripper, original *http.Request) (*http.Response, error) {
	query := queryRequest(category, original)
	if category == "update-download" {
		category = "update"
	}
	if value, ok := original.Context().Value(categoryKey{}).(string); ok {
		category = value
	}
	if err := original.Context().Err(); err != nil {
		return nil, err
	}
	key, host, err := identity(original, category)
	if err != nil {
		return nil, err
	}
	// Only the newly introduced danmaku reads adopt the five-minute minimum.
	// Legacy healthy queries retain their existing cache and polling schedule.
	longCooldown := category == "danmaku" && query
	m.mu.Lock()
	if m.closed || m.owner.Err() != nil {
		m.mu.Unlock()
		return nil, context.Canceled
	}
	now := m.now()
	m.prune(now)
	if current := m.entries[key]; current != nil {
		if current.done != nil {
			if m.waiting >= 128 {
				m.stat(category).Blocked++
				m.mu.Unlock()
				return nil, cooldown(now.Add(time.Second), now)
			}
			m.waiting++
			done := current.done
			m.mu.Unlock()
			var waitErr error
			select {
			case <-done:
			case <-original.Context().Done():
				waitErr = original.Context().Err()
			case <-m.owner.Done():
				waitErr = context.Canceled
			}
			m.mu.Lock()
			m.waiting--
			m.mu.Unlock()
			if waitErr != nil {
				return nil, waitErr
			}
			m.mu.Lock()
			reply, failure, until := current.reply, current.failure, current.until
			if reply != nil {
				m.stat(category).Reused++
			} else if longCooldown || failure != nil {
				m.stat(category).Blocked++
			}
			m.mu.Unlock()
			if err := original.Context().Err(); err != nil {
				return nil, err
			}
			if reply != nil {
				return reply.response(original), nil
			}
			if failure != nil {
				return nil, failure
			}
			if !longCooldown {
				// An oversized, partial or no-store response cannot be shared.
				// Preserve each legacy caller's ability to obtain its own body.
				return m.RoundTrip(category, base, original)
			}
			return nil, cooldown(until, m.now())
		}
		if current.reply != nil {
			m.stat(category).Reused++
			response := current.reply.response(original)
			m.mu.Unlock()
			return response, nil
		}
		m.stat(category).Blocked++
		until := current.until
		failure := current.failure
		m.mu.Unlock()
		if failure != nil && apperror.CodeOf(failure) != apperror.Cancelled {
			return nil, delayed(failure, until, now)
		}
		return nil, cooldown(until, now)
	}
	if until := m.hosts[host]; until.After(now) {
		m.stat(category).Blocked++
		m.mu.Unlock()
		return nil, cooldown(until, now)
	}
	if (query && len(m.entries) >= maxRequestKeys) || len(m.active) >= maxActiveRequests {
		m.stat(category).Blocked++
		m.mu.Unlock()
		return nil, cooldown(now.Add(time.Second), now)
	}
	if query && !m.burstAllowed(host, now) {
		m.stat(category).Blocked++
		until := now.Add(time.Second + max(0, min(2*time.Second, m.jitter(2*time.Second))))
		m.mu.Unlock()
		return nil, &apperror.Error{Code: apperror.RateLimited, Message: "短时间请求过多，请稍后重试", RetryAfterSeconds: seconds(until, now)}
	}
	until := now.Add(time.Second)
	if longCooldown {
		next, allowed, reserveErr := m.store.reserveRequest(original.Context(), key, host, now, now.Add(m.interval()))
		if reserveErr != nil {
			m.mu.Unlock()
			return nil, apperror.Wrap(apperror.Storage, reserveErr, "保存请求冷却记录失败")
		}
		if !allowed {
			m.stat(category).Blocked++
			m.mu.Unlock()
			return nil, cooldown(next, now)
		}
		until = next
	}
	var pending *entry
	if query {
		pending = &entry{until: until, done: make(chan struct{})}
		m.entries[key] = pending
	}
	m.stat(category).Requests++
	m.sequence++
	id := m.sequence
	m.wg.Add(1)
	ctx, cancel := lifecycle.WithShutdown(original.Context(), m.owner)
	m.active[id] = cancel
	m.mu.Unlock()
	var finishOnce, settleOnce sync.Once
	finish := func() {
		finishOnce.Do(func() { cancel(); m.mu.Lock(); delete(m.active, id); m.mu.Unlock(); m.wg.Done() })
	}
	settle := func(reply *responseCopy, failure error) {
		settleOnce.Do(func() {
			if pending == nil {
				return
			}
			m.mu.Lock()
			defer m.mu.Unlock()
			pending.failure = failure
			if reply != nil {
				pending.reply = reply
			}
			if longCooldown && !m.closed && m.entries[key] == pending && reply != nil {
				m.keep(key, pending, reply)
			}
			if (!longCooldown || m.closed) && m.entries[key] == pending {
				delete(m.entries, key)
			}
			done := pending.done
			pending.done = nil
			close(done)
		})
	}
	response, err := base.RoundTrip(original.Clone(ctx))
	if err != nil {
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		if longCooldown && apperror.CodeOf(err) != apperror.Cancelled {
			err = delayed(err, until, m.now())
		}
		settle(nil, err)
		finish()
		return nil, err
	}
	if response == nil {
		err = apperror.New(apperror.Internal, "网络客户端没有返回响应")
		settle(nil, err)
		finish()
		return nil, err
	}
	response.Header = response.Header.Clone()
	if response.Header == nil {
		response.Header = make(http.Header)
	}
	delay := retryAfter(response.Header.Get("Retry-After"), m.now())
	hostLimited := response.StatusCode == http.StatusTooManyRequests || (response.StatusCode == http.StatusServiceUnavailable && delay > 0)
	if hostLimited {
		deadline, saveErr := m.limitHost(host, delay)
		if saveErr != nil {
			if response.Body != nil {
				response.Body.Close()
			}
			settle(nil, saveErr)
			finish()
			return nil, saveErr
		}
		if deadline.After(until) {
			until = deadline
		}
		if pending != nil {
			m.mu.Lock()
			pending.until = until
			m.mu.Unlock()
		}
		response.Header.Set("Retry-After", fmt.Sprint(seconds(until, m.now())))
	}
	if longCooldown && response.StatusCode >= 400 {
		response.Header.Set("Retry-After", fmt.Sprint(seconds(until, m.now())))
	}
	var httpFailure error
	if longCooldown || hostLimited {
		httpFailure = responseFailure(response, original.URL.Hostname(), until, m.now())
	}
	if response.Body == nil {
		settle(nil, httpFailure)
		finish()
		return response, nil
	}
	cacheable := query && response.StatusCode == http.StatusOK && response.ContentLength <= maxCaptureBytes && !strings.Contains(strings.ToLower(response.Header.Get("Cache-Control")), "no-store")
	body := &captureBody{ReadCloser: response.Body, limit: maxCaptureBytes, capture: cacheable, reserve: m.reserveCapture, release: m.releaseCapture}
	body.complete = func(data []byte, complete bool, readErr error) {
		var reply *responseCopy
		if cacheable && complete {
			reply = &responseCopy{status: response.StatusCode, header: response.Header.Clone(), body: data, proto: response.Proto, major: response.ProtoMajor, minor: response.ProtoMinor, uncompressed: response.Uncompressed}
		}
		if readErr != nil && longCooldown && apperror.CodeOf(readErr) != apperror.Cancelled {
			readErr = delayed(readErr, until, m.now())
		}
		if readErr == nil {
			readErr = httpFailure
		}
		settle(reply, readErr)
		finish()
	}
	response.Body = body
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		body.Close()
		return nil, context.Canceled
	}
	m.active[id] = func() { body.Close() }
	m.mu.Unlock()
	return response, nil
}
func (m *Manager) limitHost(host string, server time.Duration) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delay := max(time.Second, server)
	until := m.now().Add(delay + max(0, min(time.Second, m.jitter(time.Second))))
	if current := m.hosts[host]; current.After(until) {
		until = current
	}
	if _, exists := m.hosts[host]; !exists && len(m.hosts) >= 512 {
		return time.Time{}, apperror.New(apperror.RateLimited, "限流记录已达上限，请稍后重试")
	}
	m.hosts[host] = until
	if err := m.store.deferRequestHost(m.owner, host, m.now(), until); err != nil {
		return until, apperror.Wrap(apperror.Storage, err, "保存源站限流状态失败")
	}
	return until, nil
}
func (m *Manager) reserveCapture(size int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || m.capturing+size > maxResponseBytes {
		return false
	}
	m.capturing += size
	return true
}
func (m *Manager) releaseCapture(size int) { m.mu.Lock(); m.capturing -= size; m.mu.Unlock() }
func (m *Manager) keep(key string, current *entry, reply *responseCopy) {
	count := 0
	for _, value := range m.entries {
		if value != current && value.reply != nil {
			count++
		}
	}
	for m.bytes+len(reply.body) > maxResponseBytes || count >= maxResponses {
		var victim *entry
		var earliest time.Time
		for _, value := range m.entries {
			if value != current && value.reply != nil && (earliest.IsZero() || value.until.Before(earliest)) {
				victim, earliest = value, value.until
			}
		}
		if victim == nil {
			break
		}
		m.bytes -= len(victim.reply.body)
		victim.reply = nil
		count--
	}
	m.bytes += len(reply.body)
}
func (m *Manager) Close(ctx context.Context) error {
	// Keep the closed guard installed until a later application instance replaces
	// it: late module calls must not bypass policy during shutdown.
	m.mu.Lock()
	m.closed = true
	m.cancel()
	closers := []func(){}
	for _, close := range m.active {
		closers = append(closers, close)
	}
	m.mu.Unlock()
	for _, close := range closers {
		close()
	}
	done := make(chan struct{})
	go func() { m.wg.Wait(); close(done) }()
	select {
	case <-done:
		m.mu.Lock()
		m.entries = map[string]*entry{}
		m.bytes = 0
		m.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
