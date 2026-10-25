// Package outbound routes public HTTP traffic through an application-owned proxy pool.
package outbound

import (
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strconv"
	"strings"

	"cczjVideo/app/netaddress"
)

// SettingsKey stores pool configuration in the existing SQLite settings table.
const SettingsKey = "outbound_proxy_pool_v1"
const snapshotKey = "outbound_proxy_pool_snapshot_v1"

// Config applies to public backend information requests. Media playback,
// probing and downloads retain their caller-owned transports.
type Config struct {
	Enabled        bool           `json:"enabled"`
	Sources        []string       `json:"sources"`
	Feeds          []SourceConfig `json:"feeds"`
	Seeds          []string       `json:"seeds"`
	LocalProxy     string         `json:"localProxy"`
	RefreshMinutes int            `json:"refreshMinutes"`
	MaxCandidates  int            `json:"maxCandidates"`
	MaxHealthy     int            `json:"maxHealthy"`
}

// DefaultConfig uses CONNECT-capable lists and the configured local proxy as fallback.
func DefaultConfig() Config {
	return Config{Enabled: true, Sources: []string{
		"https://raw.githubusercontent.com/proxyscrape/free-proxy-list/main/proxies/protocols/https/data.json",
		"https://raw.githubusercontent.com/databay-labs/free-proxy-list/master/http.txt",
	}, Feeds: defaultFeeds(), Seeds: []string{}, LocalProxy: "system", RefreshMinutes: 15, MaxCandidates: 24, MaxHealthy: 8}
}

// DecodeConfig validates persisted configuration before publishing it.
func DecodeConfig(raw string) (Config, error) {
	c := DefaultConfig()
	if raw != "" {
		if len(raw) > 32768 || !strings.HasPrefix(strings.TrimSpace(raw), "{") {
			return c, fmt.Errorf("invalid proxy pool configuration")
		}
		if err := json.Unmarshal([]byte(raw), &c); err != nil {
			return c, fmt.Errorf("invalid proxy pool configuration: %w", err)
		}
		var fields map[string]json.RawMessage
		_ = json.Unmarshal([]byte(raw), &fields)
		if _, legacy := fields["sources"]; legacy {
			if _, explicit := fields["feeds"]; !explicit {
				c.Feeds = nil
			}
		}
	}
	if len(c.Sources)+len(c.Feeds) > 16 || len(c.Seeds) > 64 || c.RefreshMinutes < 5 || c.RefreshMinutes > 120 || c.MaxCandidates < 1 || c.MaxCandidates > 64 || c.MaxHealthy < 1 || c.MaxHealthy > 16 {
		return c, fmt.Errorf("proxy pool bounds exceeded")
	}
	ids := map[string]bool{}
	for _, feed := range c.sourceConfigs() {
		if err := feed.validate(); err != nil {
			return c, err
		}
		if ids[feed.ID] {
			return c, fmt.Errorf("duplicate proxy source ID")
		}
		ids[feed.ID] = true
		rawURL := feed.URL
		u, err := url.Parse(rawURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || len(rawURL) > 2048 {
			return c, fmt.Errorf("proxy source must be an HTTPS URL without credentials")
		}
	}
	for _, seed := range c.Seeds {
		if _, err := parseEndpoint(seed); err != nil {
			return c, err
		}
	}
	if c.LocalProxy != "" && c.LocalProxy != "system" {
		if _, err := parseLocalProxy(c.LocalProxy); err != nil {
			return c, err
		}
	}
	return c, nil
}

func parseEndpoint(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid public proxy endpoint")
	}
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil || !netaddress.IsPublic(addr) {
		return nil, fmt.Errorf("public proxy must have a public literal IP")
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil || port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid proxy port")
	}
	u.Path = ""
	u.Host = net.JoinHostPort(addr.Unmap().String(), strconv.Itoa(port))
	return u, nil
}

func parseLocalProxy(raw string) (*url.URL, error) {
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("invalid local proxy URL")
	}
	u.Path = ""
	return u, nil
}
