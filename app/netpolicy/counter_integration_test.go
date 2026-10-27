package netpolicy_test

import (
	"context"
	"database/sql"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/netpolicy"
	"cczjVideo/app/netstats"
	_ "modernc.org/sqlite"
)

type countedTransport func(*http.Request) (*http.Response, error)

func (f countedTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }
func TestResponseReuseDoesNotDoubleCountNetworkTraffic(t *testing.T) {
	for _, category := range []string{"danmaku", "douban", "collect", "reading", "novel", "comic", "update"} {
		t.Run(category, func(t *testing.T) {
			database, err := sql.Open("sqlite", ":memory:")
			if err != nil {
				t.Fatal(err)
			}
			defer database.Close()
			database.SetMaxOpenConns(1)
			for _, query := range netpolicy.Schema {
				if _, err = database.Exec(query); err != nil {
					t.Fatal(err)
				}
			}
			manager := netpolicy.New(netpolicy.NewStore(func() *sql.DB { return database }), context.Background(), netpolicy.Settings{Frequency: "standard"})
			if err = manager.Initialize(context.Background()); err != nil {
				t.Fatal(err)
			}
			netpolicy.Install(manager)
			defer netpolicy.Install(nil)
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Second)
				defer cancel()
				if err := manager.Close(ctx); err != nil {
					t.Error(err)
				}
			}()
			before := netstats.TotalBytes()
			client := &http.Client{Transport: netpolicy.WrapTransport(category, netstats.WrapTransport(netstats.Category(category), countedTransport(func(req *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("abc"))}, nil
			})))}
			for i := 0; i < 2; i++ {
				req, err := http.NewRequestWithContext(netpolicy.WithProbeCooldown(context.Background()), http.MethodGet, "https://dm.video.qq.com/barrage/base/sample", nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, err = io.Copy(io.Discard, response.Body)
				response.Body.Close()
				if err != nil {
					t.Fatal(err)
				}
			}
			if bytes := netstats.TotalBytes() - before; bytes != 3 {
				t.Fatal("response reuse counted as extra source traffic", bytes)
			}
			stats := manager.Snapshot(map[string]bool{category: true})
			if len(stats) != 1 || stats[0].Requests != 1 || stats[0].Reused != 1 {
				t.Fatal(stats)
			}
		})
	}
}
