package netpolicy

import (
	"context"
	"net/http"
	"testing"
	"time"

	"cczjVideo/app/apperror"
)

func TestAllQueryCategoriesUseSelectedCooldownAcrossRestart(t *testing.T) {
	for _, category := range []string{"douban", "collect", "reading", "novel", "comic", "update", "danmaku"} {
		for _, tier := range []string{"standard", "steady", "low"} {
			t.Run(category+"/"+tier, func(t *testing.T) {
				store := fixtureStore(t)
				now := time.Unix(1792380000, 0)
				manager := fixtureManager(t, store, &now)
				manager.SetSettings(Settings{Frequency: tier})
				calls := 0
				base := roundTripFunc(func(*http.Request) (*http.Response, error) {
					calls++
					return reply(200, "cached", ""), nil
				})
				req := request(context.Background(), "https://source.example/metadata")
				response, err := manager.RoundTrip(category, base, req)
				readResponse(t, response, err)
				response, err = manager.RoundTrip(category, base, req)
				readResponse(t, response, err)
				if calls != 1 {
					t.Fatalf("%s bypassed tier %s: calls=%d", category, tier, calls)
				}
				restarted := fixtureManager(t, store, &now)
				_, err = restarted.RoundTrip(category, base, req)
				want := int((profiles[tier].minimum + 30*time.Second) / time.Second)
				if !apperror.Is(err, apperror.RateLimited) || apperror.Public(err).RetryAfterSeconds != want {
					t.Fatalf("restart lost tier: err=%v wantSeconds=%d", err, want)
				}
				now = now.Add(time.Duration(want) * time.Second)
				response, err = restarted.RoundTrip(category, base, req)
				readResponse(t, response, err)
				if calls != 2 {
					t.Fatalf("expiry did not admit query: calls=%d", calls)
				}
			})
		}
	}
}
