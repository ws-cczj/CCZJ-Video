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

	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/netstats"
	"cczjVideo/app/outbound"
	"cczjVideo/app/settings"
)

// Explicit opt-in only: bounded default-source update plus one real chart query.
func TestLiveAutomaticPoolAndChart(t *testing.T) {
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
