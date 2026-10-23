package danmaku

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type discoveryProvider struct {
	testProvider
	searches atomic.Int32
	catalogs atomic.Int32
}

func (p *discoveryProvider) Search(context.Context, string) ([]Candidate, error) {
	p.searches.Add(1)
	return []Candidate{{Provider: "qq", Ref: "cid:series", Title: "凡人修仙传"}}, nil
}
func (p *discoveryProvider) Episodes(context.Context, string) ([]Episode, error) {
	p.catalogs.Add(1)
	return []Episode{{Provider: "qq", EpisodeID: "video01", Number: "1"}}, nil
}
func TestDiscoveryReusesDurableCacheAcrossEpisodesAndSessions(t *testing.T) {
	store := testStore(t)
	p := &discoveryProvider{}
	now := time.Now()
	for session := 0; session < 2; session++ {
		s := NewService(store, NewClient(nil), context.Background())
		s.providers["qq"] = p
		s.now = func() time.Time { return now }
		for episode := 0; episode < 2; episode++ {
			rows, err := s.Search(context.Background(), "qq", "凡人修仙传")
			if err != nil || len(rows) != 1 {
				t.Fatal(rows, err)
			}
			list, err := s.Episodes(context.Background(), "qq", rows[0].Ref)
			if err != nil || len(list) != 1 {
				t.Fatal(list, err)
			}
		}
		if err := s.Close(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if p.searches.Load() != 1 || p.catalogs.Load() != 1 {
		t.Fatalf("repeated upstream discovery: search=%d catalog=%d", p.searches.Load(), p.catalogs.Load())
	}
}
