package outbound

import (
	"fmt"
	"regexp"
	"time"
)

// SourceConfig describes a list, not a trusted proxy. Every result still needs
// common endpoint validation and an independent HTTPS health check.
type SourceConfig struct {
	ID             string `json:"id"`
	URL            string `json:"url"`
	Format         string `json:"format"`            // auto, text, json, html
	Protocol       string `json:"protocol"`          // protocol hint for bare addresses
	Country        string `json:"country,omitempty"` // strict row metadata filter, not inferred geolocation
	Enabled        bool   `json:"enabled"`
	RefreshMinutes int    `json:"refreshMinutes"`
}

func defaultFeeds() []SourceConfig {
	return []SourceConfig{
		{ID: "kuaidaili", URL: "https://free.kuaidaili.com/free/inha/", Format: "html", Protocol: "http", Enabled: true, RefreshMinutes: 30},
		{ID: "qiyunip", URL: "https://www.qiyunip.com/freeProxy/", Format: "html", Protocol: "http", Enabled: true, RefreshMinutes: 30},
		{ID: "francevpn", URL: "https://francevpn.github.io/free-proxy/", Format: "html", Enabled: true, RefreshMinutes: 30},
		{ID: "geonode-cn", URL: "https://proxylist.geonode.com/api/proxy-list?limit=100&page=1&sort_by=lastChecked&sort_type=desc&country=CN&protocols=https%2Csocks5", Format: "json", Country: "CN", Enabled: true, RefreshMinutes: 15},
		{ID: "scdn-https-cn", URL: "https://proxy.scdn.io/api/get_proxy.php?protocol=https&count=20&country_code=CN", Format: "json", Protocol: "http", Enabled: true, RefreshMinutes: 15},
		{ID: "scdn-socks5-cn", URL: "https://proxy.scdn.io/api/get_proxy.php?protocol=socks5&count=20&country_code=CN", Format: "json", Protocol: "socks5", Enabled: true, RefreshMinutes: 15},
		{ID: "speedx-socks5", URL: "https://raw.githubusercontent.com/TheSpeedX/PROXY-List/master/socks5.txt", Format: "text", Protocol: "socks5", Enabled: true, RefreshMinutes: 60},
		{ID: "monosans-socks5", URL: "https://raw.githubusercontent.com/monosans/proxy-list/main/proxies/socks5.txt", Format: "text", Protocol: "socks5", Enabled: true, RefreshMinutes: 60},
		{ID: "proxora", URL: "https://api.proxora.io/api/tools/free-proxies?https=true&clean=true&per_page=100", Format: "json", Enabled: true, RefreshMinutes: 15},
		// Current endpoints return a verification page or HTTP 403/530. Keep them
		// explicit and disabled until their public list is available again.
		{ID: "zdaye", URL: "https://www.zdaye.com/free/", Format: "html", Protocol: "http", RefreshMinutes: 60},
		{ID: "seofangfa", URL: "https://seofangfa.com/proxy/", Format: "html", Protocol: "http", RefreshMinutes: 60},
		{ID: "rola", URL: "https://rola-ip.co/zh/tools/free-proxy-list/", Format: "html", Protocol: "http", RefreshMinutes: 60},
	}
}

var sourceID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}$`)
var countryCode = regexp.MustCompile(`^[A-Z]{2}$`)

func (s SourceConfig) validate() error {
	if !sourceID.MatchString(s.ID) || s.RefreshMinutes < 5 || s.RefreshMinutes > 1440 {
		return fmt.Errorf("invalid proxy source identity or interval")
	}
	if s.Format != "auto" && s.Format != "text" && s.Format != "json" && s.Format != "html" {
		return fmt.Errorf("invalid proxy source format")
	}
	if s.Protocol != "" && s.Protocol != "http" && s.Protocol != "socks5" {
		return fmt.Errorf("invalid proxy source protocol hint")
	}
	if s.Country != "" && !countryCode.MatchString(s.Country) {
		return fmt.Errorf("invalid proxy source country filter")
	}
	return nil
}

func (c Config) sourceConfigs() []SourceConfig {
	rows := append([]SourceConfig(nil), c.Feeds...)
	for i, raw := range c.Sources {
		rows = append(rows, SourceConfig{ID: fmt.Sprintf("legacy-%d", i+1), URL: raw, Format: "auto", Protocol: "http", Enabled: true, RefreshMinutes: c.RefreshMinutes})
	}
	return rows
}

// SourceStatus contains no fetched document, proxy credentials or request URL.
type SourceStatus struct {
	ID          string `json:"id"`
	Enabled     bool   `json:"enabled"`
	State       string `json:"state"`
	Candidates  int    `json:"candidates"`
	LastSuccess int64  `json:"lastSuccess"`
	NextAttempt int64  `json:"nextAttempt"`
	Failures    int    `json:"failures"`
	LastError   string `json:"lastError"`
}

type sourceState struct {
	config  SourceConfig
	status  SourceStatus
	rows    []candidate
	updated time.Time
	cursor  int
}
