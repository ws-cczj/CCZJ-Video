package outbound

import (
	"context"
	"net/netip"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSourceChallengeIsNotSuccessfulEmptyList(t *testing.T) {
	if _, err := decodeCandidates([]byte(`<html><script>aliyun_waf();</script>Please verify</html>`)); err == nil {
		t.Fatal("challenge page was accepted as a successful empty proxy list")
	}
}

func TestSourceProtocolAndJSONSchemas(t *testing.T) {
	for _, tc := range []struct {
		name, body, protocol, country string
		want                          []string
	}{
		{"socks text", "8.8.8.8:1080\n127.0.0.1:80\n", "socks5", "", []string{"socks5://8.8.8.8:1080"}},
		{"geonode", `{"data":[{"ip":"8.8.8.8","port":"1080","protocols":["https","socks5","socks4"],"country":"CN"},{"ip":"1.1.1.1","port":"80","protocols":["https"],"country":"US"}]}`, "", "CN", []string{"http://8.8.8.8:1080", "socks5://8.8.8.8:1080"}},
		{"scdn", `{"code":200,"data":{"proxies":["8.8.8.8:1080"]}}`, "socks5", "", []string{"socks5://8.8.8.8:1080"}},
		{"proxora", `{"items":[{"host":"8.8.8.8","port":8080,"protocol":"http","https":true,"country_code":"CN"}]}`, "", "CN", []string{"http://8.8.8.8:8080"}},
		{"monosans", `[{"host":"8.8.8.8","port":1080,"protocol":"socks5","geolocation":{"country":{"iso_code":"CN"}}},{"host":"1.1.1.1","port":80,"protocol":"http","username":"private","password":"secret"}]`, "", "CN", []string{"socks5://8.8.8.8:1080"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := decodeSource(SourceConfig{Format: "auto", Protocol: tc.protocol, Country: tc.country}, []byte(tc.body))
			if err != nil || len(rows) != len(tc.want) {
				t.Fatalf("%v %v", rows, err)
			}
			for i, want := range tc.want {
				if rows[i].URL != want {
					t.Fatal(rows)
				}
			}
		})
	}
	for _, body := range []string{`{"code":403,"data":{"proxies":["8.8.8.8:80"]}}`, `{"message":"8.8.8.8:80"}`, `[{"unknown":"8.8.8.8:80"}]`, `null`, `{"data":[]} trailing`} {
		if _, err := decodeSource(SourceConfig{Format: "json"}, []byte(body)); err == nil {
			t.Fatal("invalid envelope accepted", body)
		}
	}
}

func TestHTMLSourceSchemasAndCredentials(t *testing.T) {
	for _, body := range []string{
		`<table id="proxyTable"><tr><th>IP</th><th>PORT</th><th>类型</th><th>属地位置</th></tr><tr><th>8.8.8.8</th><th>8080</th><th>https</th><th>中国 安徽</th></tr></table>`,
		`<table><thead><tr><th>IP地址</th><th>端口号</th><th>用户名</th><th>密码</th><th>协议</th></tr></thead><tbody><tr><td>8.8.8.8</td><td>8080</td><td>****<img data-text="no need"></td><td>****<img data-text="no need"></td><td>http</td></tr><tr><td>1.1.1.1</td><td>8080</td><td><img data-text="private"></td><td><img data-text="secret"></td><td>http</td></tr></tbody></table>`,
		`<table><tr><th>评级</th><th>代理</th></tr><tr><td>A</td><td><span>http</span><span>8.8.8.8<span>:<!-- -->8080</span></span></td></tr></table>`,
	} {
		rows, err := decodeSource(SourceConfig{Format: "html"}, []byte(body))
		if err != nil || len(rows) != 1 || rows[0].URL != "http://8.8.8.8:8080" {
			t.Fatalf("%v %v", rows, err)
		}
	}
	for _, body := range []string{`<html>Login to view list</html>`, `<table><tr><td>Advertise 8.8.8.8:80</td></tr></table>`} {
		if _, err := decodeSource(SourceConfig{Format: "html"}, []byte(body)); err == nil {
			t.Fatal("unrecognized HTML accepted")
		}
	}
}

func TestSourceCadenceBackoffAndFairRotation(t *testing.T) {
	c := DefaultConfig()
	c.Sources = nil
	c.Feeds = []SourceConfig{{ID: "offline", URL: "https://example.com/list", Format: "text", Protocol: "socks5", Enabled: true, RefreshMinutes: 30}}
	c.LocalProxy = ""
	p, err := New(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	p.lookup = func(context.Context, string) ([]netip.Addr, error) {
		return []netip.Addr{netip.MustParseAddr("8.8.8.8")}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, failures := p.updateSources(ctx, nil)
	if len(failures) != 1 {
		t.Fatal(failures)
	}
	status := p.Snapshot().Sources[0]
	if status.Failures != 1 || status.State != "error" || status.NextAttempt < time.Now().Add(29*time.Minute).Unix() {
		t.Fatal(status)
	}
	_, failures = p.updateSources(ctx, nil)
	if len(failures) != 0 || p.Snapshot().Sources[0].Failures != 1 {
		t.Fatal("source backoff was bypassed")
	}
	if sourceBackoff(30*time.Minute, 4) != 4*time.Hour || sourceBackoff(time.Hour, 16) != 6*time.Hour {
		t.Fatal("backoff bounds")
	}
	a := &sourceState{rows: []candidate{{URL: "a"}, {URL: "b"}, {URL: "c"}}}
	b := &sourceState{rows: []candidate{{URL: "a"}, {URL: "d"}}}
	got := selectCandidates([]candidateBatch{{rows: a.rows, source: a}, {rows: b.rows, source: b}}, 3)
	if len(got) != 3 || got[0].URL != "a" || got[1].URL != "b" || got[2].URL != "d" || a.cursor != 2 || b.cursor != 0 {
		t.Fatal(got, a.cursor, b.cursor)
	}
	p.config.MaxCandidates = 1
	batches := []candidateBatch{{rows: []candidate{{URL: "a"}}}, {rows: []candidate{{URL: "b"}}}, {rows: []candidate{{URL: "c"}}}}
	for _, want := range []string{"a", "b", "c", "a"} {
		if batch := p.validationBatch(batches); len(batch) != 1 || batch[0].URL != want {
			t.Fatal("small budget starved a source", batch)
		}
	}
	// A diagnostic consumer cannot mutate the live source state.
	snapshot := p.Snapshot()
	snapshot.Sources[0].State = "changed"
	if p.Snapshot().Sources[0].State == "changed" {
		t.Fatal("snapshot aliases live state")
	}
}

func TestLegacySourceConfigurationRemainsExplicit(t *testing.T) {
	c, err := DecodeConfig(`{"sources":["https://example.com/custom.txt"]}`)
	if err != nil || len(c.Feeds) != 0 || len(c.sourceConfigs()) != 1 {
		t.Fatal(c, err)
	}
	for _, raw := range []string{`{"feeds":[{"id":"bad","url":"https://example.com/list","format":"xml","refreshMinutes":15}]}`, `{"feeds":[{"id":"bad","url":"https://example.com/list","format":"text","protocol":"socks4","refreshMinutes":15}]}`} {
		if _, err := DecodeConfig(raw); err == nil {
			t.Fatal(raw)
		}
	}
}

// Explicit opt-in: verifies current source availability and parsing, not proxies.
func TestLiveSourceDownloadAndParse(t *testing.T) {
	if os.Getenv("CCZJ_OUTBOUND_SOURCES_LIVE") != "1" {
		t.Skip("set CCZJ_OUTBOUND_SOURCES_LIVE=1 for source download verification")
	}
	c := DefaultConfig()
	c.LocalProxy = "http://127.0.0.1:7890"
	p, err := New(context.Background(), c, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	lists, failures := p.updateSources(ctx, nil)
	for _, s := range p.Snapshot().Sources {
		t.Logf("source=%s state=%s candidates=%d error=%s", s.ID, s.State, s.Candidates, s.LastError)
	}
	t.Logf("parsed source batches=%d failures=%d; no candidate proxy connected", len(lists), len(failures))
	for _, s := range p.Snapshot().Sources {
		if s.Enabled && (s.State != "ready" && s.State != "empty") {
			t.Errorf("%s: %s", s.ID, s.LastError)
		}
	}
	// No bare SOCKS source may silently become an HTTP proxy.
	for _, s := range p.sources {
		if s.config.Protocol == "socks5" {
			for _, row := range s.rows {
				if !strings.HasPrefix(row.URL, "socks5://") {
					t.Fatal(s.config.ID, row.URL)
				}
			}
		}
	}
}
