package applog

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

var diagnosticURL = regexp.MustCompile(`https?://[^\s<>"\\]+`)

// SafeURL retains the request shape and public CMS parameters, never credentials
// or arbitrary query values. The source identity still identifies local config.
func SafeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "[invalid URL]"
	}
	u.User, u.Fragment = nil, ""
	q := u.Query()
	for key := range q {
		switch strings.ToLower(key) {
		case "ac", "wd", "pg", "page", "limit", "pagesize", "ids", "mid", "t", "h", "at":
		default:
			q.Set(key, "[redacted]")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// Fingerprint identifies an exact configuration/response without retaining it.
func Fingerprint(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

// DiagnosticText bounds externally supplied diagnostics and removes URL secrets.
func DiagnosticText(value string, maxBytes int) string {
	value = diagnosticURL.ReplaceAllStringFunc(value, SafeURL)
	if len(value) <= maxBytes {
		return value
	}
	suffix := fmt.Sprintf("… [truncated bytes=%d sha256=%s]", len(value), Fingerprint([]byte(value)))
	end := maxBytes - len(suffix)
	if end < 0 {
		return suffix
	}
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix
}

// Health distinguishes records visible in memory from durable log evidence.
type Health struct {
	SessionID      string `json:"session_id"`
	WriteFailures  uint64 `json:"write_failures"`
	Unpersisted    uint64 `json:"unpersisted"`
	LastWriteError string `json:"last_write_error"`
	LastWriteTS    int64  `json:"last_write_ts"`
}

func (l *Logger) Health() Health {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.health
}
