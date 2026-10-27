package netpolicy

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestOrdinaryQueriesIgnoreOldCooldownLedgerAndDoNotCreateNewCooldowns(t *testing.T) {
	for _, category := range []string{"collect", "douban", "reading", "novel", "comic", "update", "danmaku"} {
		for _, tier := range []string{"standard", "steady", "low"} {
			t.Run(category+"/"+tier, func(t *testing.T) {
				store := fixtureStore(t)
				now := time.Unix(1792380000, 0)
				req := request(context.Background(), "https://source.example/metadata")
				key, host, err := identity(req, category)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := store.reserveRequest(req.Context(), key, host, now, now.Add(time.Hour)); err != nil {
					t.Fatal(err)
				}
				manager := fixtureManager(t, store, &now)
				manager.SetSettings(Settings{Frequency: tier})
				calls := 0
				base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return reply(200, "fresh", ""), nil })
				for i := 0; i < 2; i++ {
					response, err := manager.RoundTrip(category, base, req)
					readResponse(t, response, err)
				}
				restarted := fixtureManager(t, store, &now)
				response, err := restarted.RoundTrip(category, base, req)
				readResponse(t, response, err)
				if calls != 3 {
					t.Fatalf("ordinary query was cooled down: calls=%d", calls)
				}
				database, _ := store.db()
				var count int
				if err := database.QueryRow(`SELECT COUNT(*) FROM network_request_limits`).Scan(&count); err != nil || count != 1 {
					t.Fatalf("ordinary query created cooldown records: count=%d err=%v", count, err)
				}
			})
		}
	}
}

func TestOrdinaryFailuresCanBeRetriedImmediately(t *testing.T) {
	now := time.Unix(1792380000, 0)
	manager := fixtureManager(t, fixtureStore(t), &now)
	calls := 0
	failure := errors.New("temporary connection reset")
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, failure })
	for i := 0; i < 2; i++ {
		_, err := manager.RoundTrip("collect", base, request(context.Background(), "https://source.example/search?wd=test"))
		if !errors.Is(err, failure) {
			t.Fatal(err)
		}
	}
	if calls != 2 {
		t.Fatalf("retry blocked after failure: %d", calls)
	}
}

func TestProbeNeverCoolsDownSearchOrDetailWithSameURL(t *testing.T) {
	now := time.Unix(1792380000, 0)
	store := fixtureStore(t)
	manager := fixtureManager(t, store, &now)
	base := roundTripFunc(func(*http.Request) (*http.Response, error) { return reply(200, "fresh", ""), nil })
	req := request(context.Background(), "https://source.example/list")
	response, err := manager.RoundTrip("collect", base, req.WithContext(WithProbeCooldown(req.Context())))
	readResponse(t, response, err)
	for _, m := range []*Manager{manager, fixtureManager(t, store, &now)} {
		response, err = m.RoundTrip("collect", base, req)
		readResponse(t, response, err)
	}
}
