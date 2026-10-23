package danmaku_test

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/danmaku"
	"cczjVideo/app/proxy"
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"net/http"
	"net/http/cookiejar"
	"os"
	"testing"
	"time"
)

// Explicit opt-in: normal verification never contacts third-party providers.
func TestLivePublicProviders(t *testing.T) {
	if os.Getenv("CCZJ_DANMAKU_LIVE") != "1" {
		t.Skip("live provider probe disabled")
	}
	transport := &http.Transport{DialContext: proxy.SafeDialContext, MaxConnsPerHost: 2, MaxIdleConns: 4, IdleConnTimeout: 30 * time.Second}
	defer transport.CloseIdleConnections()
	jar, _ := cookiejar.New(nil)
	client := danmaku.NewClient(&http.Client{Transport: transport, Jar: jar, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 || !danmaku.AllowedURL(r.URL) {
			return http.ErrUseLastResponse
		}
		return nil
	}})
	for _, spec := range []struct{ name, query, ref string }{{"bilibili", "凡人修仙传", "BV17x411w7KC"}, {"qq", "兰香如故", "c4102g9a01t"}, {"iqiyi", "狂飙", "3493131456125200"}} {
		t.Run(spec.name, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			database.SetMaxOpenConns(1)
			if _, err = database.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)`); err != nil {
				t.Fatal(err)
			}
			for _, query := range danmaku.Schema {
				if _, err = database.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			service := danmaku.NewService(danmaku.NewStore(func() *sql.DB { return database }), client, context.Background())
			defer service.Close(context.Background())
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			start := time.Now()
			candidates, err := service.Search(ctx, spec.name, spec.query)
			t.Logf("search count=%d code=%s duration_ms=%d", len(candidates), apperror.CodeOf(err), time.Since(start).Milliseconds())
			if err != nil {
				t.Errorf("search diagnostic: %v", err)
			}
			episodes, err := service.Episodes(ctx, spec.name, spec.ref)
			if err != nil {
				t.Fatal("manual episode:", err)
			}
			if len(episodes) == 0 {
				t.Fatal("no episodes")
			}
			binding := danmaku.Binding{LocalKey: "live:episode", Confirmed: true, Episode: episodes[0]}
			if err = service.Bind(ctx, binding); err != nil {
				t.Fatal("bind:", err)
			}
			window, err := service.ReadWindow(ctx, binding.LocalKey, 0, 1)
			if err != nil {
				t.Fatal("window:", err)
			}
			stats := service.Stats()
			second, err := service.ReadWindow(ctx, binding.LocalKey, 0, 1)
			if err != nil || !second.Cached || service.Stats().Fetches != stats.Fetches {
				t.Fatal("cache did not prevent repeat fetch", err)
			}
			t.Logf("episode_id=%s items=%d skipped=%d total_ms=%d stats=%+v", episodes[0].EpisodeID, len(window.Items), window.Skipped, time.Since(start).Milliseconds(), service.Stats())
		})
	}
}
