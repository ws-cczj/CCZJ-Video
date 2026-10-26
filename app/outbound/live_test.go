package outbound_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/danmaku"
	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/netstats"
	"cczjVideo/app/outbound"
	"cczjVideo/app/settings"
)

// Live checks share the real pool, persistent cooldown and lifecycle.
func newLiveAutomaticPool(t *testing.T) *outbound.Pool {
	t.Helper()
	if os.Getenv("CCZJ_OUTBOUND_POOL_LIVE") != "1" {
		t.Skip("set CCZJ_OUTBOUND_POOL_LIVE=1 for bounded public proxy pool validation")
	}
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	owner := lifecycle.NewGroup()
	t.Cleanup(func() {
		if !owner.Stop(5 * time.Second) {
			t.Error("live pool did not stop")
		}
	})
	config := outbound.DefaultConfig()
	config.MaxCandidates = 24
	config.MaxHealthy = 3
	pool, err := outbound.New(owner.Context(), config, settings.NewService())
	if err != nil {
		t.Fatal(err)
	}
	outbound.Install(pool)
	t.Cleanup(func() { pool.Close(); outbound.Install(nil) })
	manager := netpolicy.New(netpolicy.NewStore(func() *sql.DB { return db.DB().DB }), owner.Context(), netpolicy.Settings{Frequency: "standard"})
	if err := manager.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	netpolicy.Install(manager)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		manager.Close(ctx)
		netpolicy.Install(nil)
	})
	if !owner.Go("outbound.pool.live", func(ctx context.Context) { pool.Run(ctx) }) {
		t.Fatal("failed to launch pool")
	}
	return pool
}

// Explicit opt-in only: bounded default-source update plus one real chart query.
func TestLiveAutomaticPoolAndChart(t *testing.T) {
	pool := newLiveAutomaticPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	client := &http.Client{Timeout: 100 * time.Second, Transport: netpolicy.WrapTransport("douban", netstats.WrapTransport(netstats.CategoryDouban, &http.Transport{})), CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://movie.douban.com/chart", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://movie.douban.com/")
	response, err := client.Do(req)
	status := pool.Snapshot()
	encoded, _ := json.Marshal(status)
	t.Logf("pool status: %s", encoded)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	response.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Count(string(raw), `<tr class="item">`)
	t.Logf("real chart HTTP=%d parsed_rows=%d", response.StatusCode, rows)
	if response.StatusCode != 200 || rows == 0 {
		t.Fatalf("live pool did not obtain chart: HTTP=%d rows=%d", response.StatusCode, rows)
	}
	before := pool.Snapshot()
	response, err = client.Do(req.Clone(ctx))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	after := pool.Snapshot()
	if after.PublicAttempts != before.PublicAttempts || after.LocalFallbacks != before.LocalFallbacks {
		t.Fatal("repeat bypassed query cooldown")
	}
	t.Log("repeat reused response without new public proxy or local fallback attempts")
	if status.Healthy == 0 || status.PublicAttempts == 0 {
		t.Fatal("chart used only fallback; automatic pool not validated")
	}
}

// A homepage or exit-IP response cannot establish danmaku availability. Exercise
// each provider's real search, episode and decoded comment-window path.
func TestLiveAutomaticPoolAndDanmaku(t *testing.T) {
	pool := newLiveAutomaticPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for !pool.Snapshot().Ready {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
		}
	}
	encoded, _ := json.Marshal(pool.Snapshot())
	t.Logf("initial pool: %s", encoded)
	for _, statement := range danmaku.Schema {
		if _, err := db.DB().DB.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	httpClient := &http.Client{Transport: netpolicy.WrapTransport("danmaku", netstats.WrapTransport(netstats.CategoryDanmaku, &http.Transport{})), CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) > 3 || !danmaku.AllowedURL(req.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}}
	defer httpClient.CloseIdleConnections()
	service := danmaku.NewService(danmaku.NewStore(func() *sql.DB { return db.DB().DB }), danmaku.NewClient(httpClient), context.Background())
	defer service.Close(context.Background())
	for _, spec := range []struct{ provider, query, ref string }{
		{"bilibili", "凡人修仙传", "BV17x411w7KC"},
		{"qq", "兰香如故", "c4102g9a01t"},
		{"iqiyi", "狂飙", "3493131456125200"},
	} {
		t.Run(spec.provider, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			checkRoute := func(before outbound.Status, stage string, err error) {
				after := pool.Snapshot()
				t.Logf("%s code=%s public_attempts=%d local_fallbacks=%d", stage, apperror.CodeOf(err), after.PublicAttempts-before.PublicAttempts, after.LocalFallbacks-before.LocalFallbacks)
				if err == nil && (after.PublicAttempts == before.PublicAttempts || after.LocalFallbacks != before.LocalFallbacks) {
					t.Errorf("%s did not exclusively validate public proxies", stage)
				}
			}
			before := pool.Snapshot()
			candidates, err := service.Search(ctx, spec.provider, spec.query)
			checkRoute(before, "search", err)
			t.Logf("search candidates=%d", len(candidates))
			if err != nil {
				t.Errorf("search: %v", err)
			}
			before = pool.Snapshot()
			episodes, err := service.Episodes(ctx, spec.provider, spec.ref)
			checkRoute(before, "episodes", err)
			if err != nil || len(episodes) == 0 {
				t.Fatalf("episodes: count=%d error=%v", len(episodes), err)
			}
			key := "public-proxy-live:" + spec.provider
			if err = service.Bind(ctx, danmaku.Binding{LocalKey: key, Confirmed: true, Episode: episodes[0]}); err != nil {
				t.Fatal(err)
			}
			before = pool.Snapshot()
			window, err := service.ReadWindow(ctx, key, 0, 1)
			checkRoute(before, "comment_window", err)
			if err != nil {
				t.Fatal("comment window:", err)
			}
			t.Logf("decoded comments=%d skipped=%d cached=%t", len(window.Items), window.Skipped, window.Cached)
			if window.Cached || len(window.Items) == 0 {
				t.Fatal("probe did not obtain a fresh populated comment window")
			}
			before = pool.Snapshot()
			second, err := service.ReadWindow(ctx, key, 0, 1)
			after := pool.Snapshot()
			if err != nil || !second.Cached || after.PublicAttempts != before.PublicAttempts || after.LocalFallbacks != before.LocalFallbacks {
				t.Fatal("repeat window did not reuse the decoded cache", err)
			}
		})
	}
}
