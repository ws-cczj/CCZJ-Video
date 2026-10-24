//go:build windows

package outbound

import (
	"net/url"
	"strings"

	"golang.org/x/sys/windows/registry"
)

func systemProxy() (*url.URL, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, nil
	}
	defer key.Close()
	enabled, _, err := key.GetIntegerValue("ProxyEnable")
	if err != nil || enabled == 0 {
		return nil, nil
	}
	raw, _, err := key.GetStringValue("ProxyServer")
	if err != nil || raw == "" {
		return nil, nil
	}
	if strings.Contains(raw, "=") {
		values := map[string]string{}
		for _, part := range strings.Split(raw, ";") {
			k, v, ok := strings.Cut(part, "=")
			if ok {
				values[strings.TrimSpace(k)] = strings.TrimSpace(v)
			}
		}
		raw = values["https"]
		if raw == "" {
			raw = values["http"]
		}
		if raw == "" {
			return nil, nil
		}
	}
	return parseLocalProxy(raw)
}
