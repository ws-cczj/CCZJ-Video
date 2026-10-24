//go:build !windows

package outbound

import "net/url"

func systemProxy() (*url.URL, error) { return nil, nil }
