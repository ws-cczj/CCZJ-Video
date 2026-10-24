package outbound

import (
	"net/url"
	"os"
)

func localProxy(configured string) (*url.URL, error) {
	if configured == "" {
		return nil, nil
	}
	if configured != "system" {
		return parseLocalProxy(configured)
	}
	for _, key := range []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "HTTP_PROXY", "http_proxy"} {
		if raw := os.Getenv(key); raw != "" {
			return parseLocalProxy(raw)
		}
	}
	return systemProxy()
}
