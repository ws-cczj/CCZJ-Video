package netpolicy

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSettingsRejectUnknownOrMalformedValues(t *testing.T) {
	for _, raw := range []string{"null", "[]", `{"frequency":null}`, `{"frequency":"fast"}`, `{"frequency":true}`, `{} {}`} {
		if _, err := DecodeSettings(raw); err == nil {
			t.Fatal(raw)
		}
	}
	for _, raw := range []string{"", `{}`, `{"frequency":"standard"}`, `{"frequency":"steady"}`, `{"frequency":"low"}`} {
		if _, err := DecodeSettings(raw); err != nil {
			t.Fatal(raw, err)
		}
	}
}
func TestJitterIsPositiveAndBoundedForEachTier(t *testing.T) {
	for frequency, p := range profiles {
		manager := New(nil, context.Background(), Settings{Frequency: frequency})
		defer manager.cancel()
		minimum, maximum := p.minimum+p.jitter, p.minimum
		for i := 0; i < 100; i++ {
			got := manager.interval()
			minimum = min(minimum, got)
			maximum = max(maximum, got)
			if got < p.minimum || got > p.minimum+p.jitter {
				t.Fatal(frequency, got)
			}
		}
		if minimum == maximum {
			t.Fatal("non-random interval", frequency)
		}
	}
}
func TestRequestIdentityIgnoresOnlyKnownSignatureFields(t *testing.T) {
	a := request(context.Background(), "https://www.iqiyi.com/prelw/tvg/v2/lw/base_info?entity_id=123&timestamp=1&sign=a")
	b := request(context.Background(), "https://www.iqiyi.com/prelw/tvg/v2/lw/base_info?sign=b&timestamp=2&entity_id=123")
	ka, _, _ := identity(a, "danmaku")
	kb, _, _ := identity(b, "danmaku")
	if ka != kb {
		t.Fatal("changing signatures bypasses cooldown")
	}
	a, _ = http.NewRequest("POST", "https://source.example/search", strings.NewReader(`{"page":1}`))
	b, _ = http.NewRequest("POST", "https://source.example/search", strings.NewReader(`{"page":2}`))
	ka, _, _ = identity(a, "comic")
	kb, _, _ = identity(b, "comic")
	if ka == kb {
		t.Fatal("different bodies conflated")
	}
	a = request(context.Background(), "https://source.example/list?timestamp=1")
	b = request(context.Background(), "https://source.example/list?timestamp=2")
	ka, _, _ = identity(a, "collect")
	kb, _, _ = identity(b, "collect")
	if ka == kb {
		t.Fatal("unknown query parameters removed")
	}
}
func TestRetryAfterParsesBothServerForms(t *testing.T) {
	now := time.Unix(1792380000, 0)
	for _, value := range []string{"600", now.Add(600 * time.Second).UTC().Format(http.TimeFormat)} {
		if got := retryAfter(value, now); got != 600*time.Second {
			t.Fatal(value, got)
		}
	}
}
