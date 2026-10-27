package netpolicy

import (
	"cczjVideo/app/apperror"
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
func TestProbeIntervalIsFiveMinutesForEveryTier(t *testing.T) {
	for _, frequency := range []string{"standard", "steady", "low"} {
		t.Run(frequency, func(t *testing.T) {
			store := fixtureStore(t)
			now := time.Unix(1792380000, 0)
			manager := fixtureManager(t, store, &now)
			manager.SetSettings(Settings{Frequency: frequency})
			base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, "sample", ""), nil })
			req := probeRequest(context.Background(), "https://source.example/probe")
			response, err := manager.RoundTrip("collect", base, req)
			readResponse(t, response, err)
			now = now.Add(299 * time.Second)
			restarted := fixtureManager(t, store, &now)
			_, err = restarted.RoundTrip("collect", base, req)
			if apperror.Public(err).RetryAfterSeconds != 1 {
				t.Fatalf("probe interval changed with tier: %v", err)
			}
			now = now.Add(time.Second)
			response, err = restarted.RoundTrip("collect", base, req)
			readResponse(t, response, err)
		})
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
