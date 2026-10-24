package douban

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/netpolicy"
)

// Opt-in, bounded probe: never use the user's database or automatically retry
// a rejected chart. The optional challenge flow shares a three-request ceiling.
func TestLiveChartWithSharedCooldown(t *testing.T) {
	if os.Getenv("CCZJ_DOUBAN_CHART_LIVE") != "1" {
		t.Skip("set CCZJ_DOUBAN_CHART_LIVE=1 for one bounded live chart probe")
	}
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	manager := netpolicy.New(netpolicy.NewStore(func() *sql.DB { return db.DB().DB }), ctx, netpolicy.Settings{Frequency: "standard"})
	if err := manager.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	netpolicy.Install(manager)
	t.Cleanup(func() {
		netpolicy.Install(nil)
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = manager.Close(closeCtx)
	})
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	old := client
	t.Cleanup(func() { client = old })
	base := &http.Transport{MaxIdleConns: 2, MaxIdleConnsPerHost: 1, TLSHandshakeTimeout: 10 * time.Second}
	if proxyAddress := os.Getenv("CCZJ_DOUBAN_LIVE_PROXY"); proxyAddress != "" {
		proxyURL, err := url.Parse(proxyAddress)
		if err != nil || proxyURL.Hostname() == "" || (proxyURL.Scheme != "http" && proxyURL.Scheme != "https" && proxyURL.Scheme != "socks5") {
			t.Fatal("CCZJ_DOUBAN_LIVE_PROXY must be an http, https or socks5 proxy URL")
		}
		base.Proxy = http.ProxyURL(proxyURL)
		t.Logf("route: explicit %s proxy %s (credentials omitted)", proxyURL.Scheme, proxyURL.Host)
	} else {
		t.Log("route: direct")
	}
	t.Cleanup(base.CloseIdleConnections)
	calls := 0
	var first *http.Request
	counted := interactiveTransport(func(r *http.Request) (*http.Response, error) {
		if calls >= 3 {
			return nil, fmt.Errorf("live probe request ceiling reached")
		}
		calls++
		if first == nil {
			first = r.Clone(ctx)
		}
		response, err := base.RoundTrip(r)
		if err != nil {
			t.Logf("outbound #%d %s%s error=%v", calls, r.URL.Host, r.URL.Path, err)
		} else {
			t.Logf("outbound #%d %s%s HTTP=%d", calls, r.URL.Host, r.URL.Path, response.StatusCode)
		}
		return response, err
	})
	client = &http.Client{Timeout: 15 * time.Second, Jar: doubanJar, CheckRedirect: old.CheckRedirect, Transport: netpolicy.WrapTransport("douban", counted)}
	html, liveErr := fetchInteractiveDoubanContext(ctx, chartURL, "https://movie.douban.com/", "live chart")
	items := parseDoubanChart(html)
	t.Logf("live result: parsed_items=%d err=%v", len(items), liveErr)
	if first == nil {
		t.Fatal("probe did not reach the HTTP transport")
	}
	before := calls
	response, cooldownErr := manager.RoundTrip("douban", counted, first)
	if cooldownErr == nil {
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
	} else if apperror.Public(cooldownErr).RetryAfterSeconds == 0 {
		t.Fatalf("repeat did not report remaining cooldown: %v", cooldownErr)
	}
	if calls != before {
		t.Fatal("repeat reached upstream during cooldown")
	}
	t.Logf("repeat blocked/reused without network: outbound=%d retryAfterSeconds=%d", calls, apperror.Public(cooldownErr).RetryAfterSeconds)
	if liveErr != nil || len(items) == 0 {
		t.Fatalf("live chart unavailable: parsed_items=%d err=%v", len(items), liveErr)
	}
}
