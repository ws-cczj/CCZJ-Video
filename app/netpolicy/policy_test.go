package netpolicy

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	_ "modernc.org/sqlite"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func fixtureStore(t *testing.T) *Store {
	t.Helper()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	database.SetMaxOpenConns(1)
	t.Cleanup(func() { database.Close() })
	for _, query := range Schema {
		if _, err = database.Exec(query); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(func() *sql.DB { return database })
}
func fixtureManager(t *testing.T, store *Store, clock *time.Time) *Manager {
	t.Helper()
	manager := New(store, context.Background(), Settings{Frequency: "standard"})
	manager.now = func() time.Time { return *clock }
	manager.jitter = func(limit time.Duration) time.Duration { return min(limit, 30*time.Second) }
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := manager.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return manager
}
func request(ctx context.Context, url string) *http.Request {
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	return req
}
func probeRequest(ctx context.Context, url string) *http.Request {
	return request(WithProbeCooldown(ctx), url)
}
func reply(status int, data string, retry string) *http.Response {
	header := make(http.Header)
	if retry != "" {
		header.Set("Retry-After", retry)
	}
	return &http.Response{StatusCode: status, Header: header, Body: io.NopCloser(strings.NewReader(data))}
}
func readResponse(t *testing.T, response *http.Response, err error) string {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	data, readErr := io.ReadAll(response.Body)
	closeErr := response.Body.Close()
	if readErr != nil || closeErr != nil {
		t.Fatal(readErr, closeErr)
	}
	return string(data)
}
func waitCondition(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for !condition() {
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("condition did not become true")
		}
	}
}

func TestExplicitProbesReuseResponsesDuringCooldown(t *testing.T) {
	store := fixtureStore(t)
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, store, &now)
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return reply(200, "fresh", ""), nil })
	for _, category := range []string{"collect", "douban", "novel", "comic", "update"} {
		for i := 0; i < 2; i++ {
			response, err := manager.RoundTrip(category, base, probeRequest(context.Background(), "https://source.example/metadata"))
			if data := readResponse(t, response, err); data != "fresh" {
				t.Fatal(data)
			}
		}
	}
	if calls.Load() != 5 {
		t.Fatal("same query reached upstream during cooldown", calls.Load())
	}
	database, _ := store.db()
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM network_request_limits`).Scan(&count); err != nil || count != 5 {
		t.Fatal("query cooldown was not persisted", count, err)
	}
}

func TestMediaMetadataUsesPersistedFiveMinuteCooldownWithoutThrottlingPlayback(t *testing.T) {
	store := fixtureStore(t)
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, store, &now)
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return reply(200, "#EXTM3U\n", ""), nil })
	metadata := request(WithProbeCooldown(context.Background()), "https://media.example/index.m3u8")
	response, err := manager.RoundTrip("lineprobe", base, metadata)
	readResponse(t, response, err)
	now = now.Add(299 * time.Second)
	response, err = manager.RoundTrip("lineprobe", base, metadata)
	readResponse(t, response, err)
	if calls.Load() != 1 {
		t.Fatal("metadata reached upstream before five minutes", calls.Load())
	}
	restarted := fixtureManager(t, store, &now)
	_, err = restarted.RoundTrip("lineprobe", base, metadata)
	if apperror.CodeOf(err) != apperror.RateLimited || apperror.Public(err).RetryAfterSeconds < 1 {
		t.Fatal("restart bypassed metadata cooldown", err)
	}
	response, err = manager.RoundTrip("playback", base, request(context.Background(), "https://media.example/index.m3u8"))
	readResponse(t, response, err)
	if calls.Load() != 2 {
		t.Fatal("metadata cooldown blocked playback", calls.Load())
	}
}

func TestFailedProbeRetainsCauseAndCooldown(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	failure := errors.New("connection reset")
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, failure })
	for i := 0; i < 2; i++ {
		_, err := manager.RoundTrip("collect", base, probeRequest(context.Background(), "https://source.example/list"))
		if !errors.Is(err, failure) || apperror.Public(err).RetryAfterSeconds != 300 {
			t.Fatal("failure lost cause or cooldown", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal(calls.Load())
	}
}

func TestProbeIdentityIgnoresRotatingSignaturesAcrossRestart(t *testing.T) {
	store := fixtureStore(t)
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, store, &now)
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return reply(200, "comments", ""), nil })
	req := probeRequest(context.Background(), "https://api.bilibili.com/x/web-interface/wbi/search/type?keyword=sample&wts=1&w_rid=a")
	response, err := manager.RoundTrip("danmaku", base, req)
	readResponse(t, response, err)
	signed := probeRequest(context.Background(), "https://api.bilibili.com/x/web-interface/wbi/search/type?w_rid=b&wts=2&keyword=sample")
	response, err = manager.RoundTrip("danmaku", base, signed)
	if data := readResponse(t, response, err); data != "comments" {
		t.Fatal(data)
	}
	if calls.Load() != 1 {
		t.Fatal("signed duplicate reached upstream")
	}
	restarted := fixtureManager(t, store, &now)
	_, err = restarted.RoundTrip("danmaku", base, signed)
	if !apperror.Is(err, apperror.RateLimited) || apperror.Public(err).RetryAfterSeconds != 300 {
		t.Fatal(err)
	}
	now = now.Add(299 * time.Second)
	_, err = manager.RoundTrip("danmaku", base, signed)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	response, err = restarted.RoundTrip("danmaku", base, signed)
	readResponse(t, response, err)
	if calls.Load() != 2 {
		t.Fatal("expiry did not allow next request")
	}
}

func TestProbeFailureKeepsFiveMinuteDeadlineAcrossTierChanges(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	manager.SetSettings(Settings{Frequency: "steady"})
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return nil, errors.New("offline") })
	req := probeRequest(context.Background(), "https://dm.video.qq.com/barrage/base/sample")
	_, err := manager.RoundTrip("danmaku", base, req)
	if apperror.Public(err).RetryAfterSeconds != 300 {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	manager.SetSettings(Settings{Frequency: "low"})
	_, err = manager.RoundTrip("danmaku", base, req)
	if apperror.Public(err).RetryAfterSeconds != 240 || calls.Load() != 1 {
		t.Fatal("click reset deadline", err, calls.Load())
	}
}

func TestDifferentSegmentsSourcesAndSessionsNeverShareResponses(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return reply(200, "comments", ""), nil })
	requests := []*http.Request{
		request(context.Background(), "https://dm.video.qq.com/barrage/segment/sample/1"),
		request(context.Background(), "https://dm.video.qq.com/barrage/segment/sample/2"),
		request(WithScope(context.Background(), "another-source"), "https://dm.video.qq.com/barrage/segment/sample/1"),
		request(context.Background(), "https://dm.video.qq.com/barrage/segment/sample/1"),
	}
	requests[3].Header.Set("X-API-Key", "another-secret")
	for _, req := range requests {
		response, err := manager.RoundTrip("danmaku", base, req)
		readResponse(t, response, err)
	}
	if calls.Load() != 4 {
		t.Fatal("different identities collapsed", calls.Load())
	}
}

func TestConcurrentProbesCoalesceAndReuseUntilCooldownExpires(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	var calls atomic.Int32
	started, release := make(chan struct{}), make(chan struct{})
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
		}
		return reply(200, "catalog", ""), nil
	})
	const readers = 8
	done := make(chan error, readers)
	for i := 0; i < readers; i++ {
		go func() {
			response, err := manager.RoundTrip("novel", base, probeRequest(context.Background(), "https://source.example/catalog"))
			if err == nil {
				_, err = io.ReadAll(response.Body)
				response.Body.Close()
			}
			done <- err
		}()
	}
	<-started
	waitCondition(t, func() bool { manager.mu.Lock(); defer manager.mu.Unlock(); return manager.waiting == readers-1 })
	close(release)
	for i := 0; i < readers; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("duplicate in-flight query")
	}
	response, err := manager.RoundTrip("novel", base, probeRequest(context.Background(), "https://source.example/catalog"))
	readResponse(t, response, err)
	if calls.Load() != 1 {
		t.Fatal("cached catalog refetched during cooldown")
	}
	now = now.Add(300 * time.Second)
	response, err = manager.RoundTrip("novel", base, probeRequest(context.Background(), "https://source.example/catalog"))
	readResponse(t, response, err)
	if calls.Load() != 2 {
		t.Fatal("catalog did not refresh after cooldown")
	}
}

func TestWaitingCallerCancellationLeavesOwnerRequestRunning(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, "catalog", ""), nil })
	req := request(context.Background(), "https://source.example/catalog")
	owner, err := manager.RoundTrip("novel", base, req)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := manager.RoundTrip("novel", base, req.WithContext(ctx)); done <- err }()
	waitCondition(t, func() bool { manager.mu.Lock(); defer manager.mu.Unlock(); return manager.waiting == 1 })
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if data := readResponse(t, owner, nil); data != "catalog" {
		t.Fatal(data)
	}
}

func TestUnshareableProbeResponseKeepsCooldownForWaitingCaller(t *testing.T) {
	for _, scenario := range []string{"no-store", "large", "http-error"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Unix(1792380000, 0)
			manager := fixtureManager(t, fixtureStore(t), &now)
			var calls atomic.Int32
			base := roundTripFunc(func(*http.Request) (*http.Response, error) {
				calls.Add(1)
				response := reply(200, "catalog", "")
				switch scenario {
				case "no-store":
					response.Header.Set("Cache-Control", "no-store")
				case "large":
					response.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxCaptureBytes+1)))
				case "http-error":
					response.StatusCode = 404
				}
				return response, nil
			})
			req := probeRequest(context.Background(), "https://source.example/catalog")
			first, err := manager.RoundTrip("novel", base, req)
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() {
				response, err := manager.RoundTrip("novel", base, req)
				if err == nil {
					_, err = io.Copy(io.Discard, response.Body)
					response.Body.Close()
				}
				done <- err
			}()
			waitCondition(t, func() bool { manager.mu.Lock(); defer manager.mu.Unlock(); return manager.waiting == 1 })
			if _, err = io.Copy(io.Discard, first.Body); err != nil {
				t.Fatal(err)
			}
			first.Body.Close()
			want := apperror.RateLimited
			if scenario == "http-error" {
				want = apperror.NotFound
			}
			if err = <-done; !apperror.Is(err, want) || apperror.Public(err).RetryAfterSeconds != 300 {
				t.Fatal("waiting query lost cooldown or failure classification", err)
			}
			if calls.Load() != 1 {
				t.Fatal("non-shareable query repeated during cooldown", calls.Load())
			}
			now = now.Add(300 * time.Second)
			next, err := manager.RoundTrip("novel", base, req)
			readResponse(t, next, err)
			if calls.Load() != 2 {
				t.Fatal("query stayed blocked after cooldown", calls.Load())
			}
		})
	}
}

func TestQueryLedgerCapacityDoesNotBlockContinuousPlayback(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	for i := 0; i < maxRequestKeys; i++ {
		manager.entries[fmt.Sprint(i)] = &entry{until: now.Add(time.Hour)}
	}
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, "segment", ""), nil })
	response, err := manager.RoundTrip("playback", base, request(context.Background(), "https://source.example/play.m3u8"))
	if data := readResponse(t, response, err); data != "segment" {
		t.Fatal(data)
	}
}

func TestUpstreamRateLimitProtectsOtherQueriesAndSurvivesRestart(t *testing.T) {
	store := fixtureStore(t)
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, store, &now)
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return reply(429, "rate limited", "600"), nil
	})
	response, err := manager.RoundTrip("douban", base, request(context.Background(), "https://source.example/search?q=one"))
	readResponse(t, response, err)
	restarted := fixtureManager(t, store, &now)
	for _, category := range []string{"collect", "playback", "comic"} {
		_, err = restarted.RoundTrip(category, base, request(context.Background(), "https://source.example/another"))
		if !apperror.Is(err, apperror.RateLimited) || apperror.Public(err).RetryAfterSeconds < 600 {
			t.Fatal(category, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("host wait bypassed")
	}
}

func TestContinuousTransfersAreNotCapturedOrDelayedByFiveMinutes(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	data := bytes.Repeat([]byte{'x'}, maxCaptureBytes+1)
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(data))}, nil
	})
	for _, category := range []string{"playback", "download", "image", "reading-image", "lineprobe", "cast"} {
		for i := 0; i < 2; i++ {
			response, err := manager.RoundTrip(category, base, request(WithCategory(context.Background(), "comic"), "https://source.example/segment"))
			if err != nil {
				t.Fatal(category, err)
			}
			body, ok := response.Body.(*captureBody)
			if !ok || body.capture {
				t.Fatal("stream capture enabled", category)
			}
			if n, err := io.Copy(io.Discard, response.Body); err != nil || n != int64(len(data)) {
				t.Fatal(n, err)
			}
			response.Body.Close()
		}
	}
	manager.mu.Lock()
	defer manager.mu.Unlock()
	if manager.bytes != 0 || manager.capturing != 0 {
		t.Fatal("stream retained bytes")
	}
}

func TestPartialOrOversizedResponseDoesNotBecomeCachedSuccess(t *testing.T) {
	for _, partial := range []bool{true, false} {
		t.Run(map[bool]string{true: "partial", false: "large"}[partial], func(t *testing.T) {
			now := time.Unix(1792380000, 0)
			manager := fixtureManager(t, fixtureStore(t), &now)
			data := strings.Repeat("x", maxCaptureBytes+1)
			base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, data, ""), nil })
			req := probeRequest(context.Background(), "https://dm.video.qq.com/barrage/base/sample")
			response, err := manager.RoundTrip("danmaku", base, req)
			if err != nil {
				t.Fatal(err)
			}
			if partial {
				buffer := make([]byte, 16)
				response.Body.Read(buffer)
			} else {
				io.Copy(io.Discard, response.Body)
			}
			response.Body.Close()
			if _, err = manager.RoundTrip("danmaku", base, req); !apperror.Is(err, apperror.RateLimited) {
				t.Fatal("partial reply cached", err)
			}
			if manager.capturing != 0 || manager.bytes != 0 {
				t.Fatal("capture budget leaked")
			}
		})
	}
}

func TestBurstProtectionDoesNotChangeNormalCadence(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	manager.SetSettings(Settings{Frequency: "low"})
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, "ok", ""), nil })
	for i := 0; i < 10; i++ {
		response, err := manager.RoundTrip("collect", base, request(context.Background(), fmt.Sprintf("https://source.example/list?page=%d", i)))
		readResponse(t, response, err)
	}
	_, err := manager.RoundTrip("collect", base, request(context.Background(), "https://source.example/list?page=10"))
	if !apperror.Is(err, apperror.RateLimited) || apperror.Public(err).RetryAfterSeconds > 3 {
		t.Fatal(err)
	}
	now = now.Add(time.Second)
	response, err := manager.RoundTrip("collect", base, request(context.Background(), "https://source.example/list?page=10"))
	readResponse(t, response, err)
}

func TestPolicyShutdownClosesUnconsumedBodies(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, "ok", ""), nil })
	_, err := manager.RoundTrip("collect", base, request(context.Background(), "https://source.example/list"))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err = manager.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = manager.RoundTrip("collect", base, request(context.Background(), "https://source.example/list")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestMissingProbeResourceRemainsNotFoundDuringCooldown(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls.Add(1); return reply(404, "missing", ""), nil })
	req := probeRequest(context.Background(), "https://dm.video.qq.com/barrage/segment/sample/missing")
	response, err := manager.RoundTrip("danmaku", base, req)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	_, err = manager.RoundTrip("danmaku", base, req)
	if !apperror.Is(err, apperror.NotFound) || apperror.Public(err).RetryAfterSeconds != 300 || calls.Load() != 1 {
		t.Fatal("missing segment changed meaning", err, calls.Load())
	}
}
