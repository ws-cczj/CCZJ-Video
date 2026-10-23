package danmaku

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err = db.Exec(`CREATE TABLE settings(key TEXT PRIMARY KEY,value TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, q := range Schema {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range MatchSchema {
		if _, err = db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return NewStore(func() *sql.DB { return db })
}

type testProvider struct {
	calls   atomic.Int32
	indexes atomic.Int32
	failure atomic.Bool
	wait    bool
	began   chan struct{}
	stopped chan struct{}
}

func (p *testProvider) Search(context.Context, string) ([]Candidate, error) { return nil, nil }
func (p *testProvider) Episodes(context.Context, string) ([]Episode, error) { return nil, nil }
func (p *testProvider) Index(context.Context, Episode) (Index, error) {
	p.indexes.Add(1)
	return Index{Segments: []Segment{{Key: "1", StartMS: 0, EndMS: 60000}, {Key: "2", StartMS: 60000, EndMS: 120000}}}, nil
}
func (p *testProvider) Segment(ctx context.Context, _ Episode, s Segment) ([]Item, int, error) {
	p.calls.Add(1)
	if p.wait {
		close(p.began)
		<-ctx.Done()
		close(p.stopped)
		return nil, 0, ctx.Err()
	}
	if p.failure.Load() {
		return nil, 0, errors.New("offline")
	}
	return []Item{{ID: s.Key, TimeMS: s.StartMS + 1000, Text: "bounded cache", Mode: 1, Color: 0xffffff, FontSize: 25}}, 0, nil
}
func boundService(t *testing.T) (*Service, *testProvider, *time.Time) {
	t.Helper()
	store := testStore(t)
	s := NewService(store, NewClient(nil), context.Background())
	p := &testProvider{}
	s.providers["qq"] = p
	now := time.Unix(1792380000, 0)
	s.now = func() time.Time { return now }
	if err := store.SaveBinding(context.Background(), Binding{LocalKey: "video:source:episode", Confirmed: true, Episode: Episode{Provider: "qq", EpisodeID: "c4102g9a01t"}, OffsetMS: 5000}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s, p, &now
}
func TestMinimumOneDayCacheAndThirtyDayExpiry(t *testing.T) {
	s, p, now := boundService(t)
	ctx := context.Background()
	first, err := s.ReadWindow(ctx, "video:source:episode", 6000, 0)
	if err != nil || first.Cached || len(first.Items) != 2 {
		t.Fatal(first, err)
	}
	if first.Items[0].TimeMS != 1000 {
		t.Fatal("offset was written into cached source timestamp")
	}
	calls := p.calls.Load()
	*now = now.Add(23 * time.Hour)
	second, err := s.ReadWindow(ctx, "video:source:episode", 6000, 0)
	if err != nil || !second.Cached || p.calls.Load() != calls {
		t.Fatal("cache refetched before one day", second, err)
	}
	*now = now.Add(2 * time.Hour)
	p.failure.Store(true)
	stale, err := s.ReadWindow(ctx, "video:source:episode", 6000, 1)
	if err != nil || !stale.Stale || len(stale.Warnings) == 0 {
		t.Fatal("lost usable stale cache", stale, err)
	}
	*now = now.Add(30 * 24 * time.Hour)
	if _, err = s.ReadWindow(ctx, "video:source:episode", 6000, 1); err == nil {
		t.Fatal("retained stale payload after 30 days")
	}
}
func TestModeSwitchCancellationStopsSegmentWork(t *testing.T) {
	s, p, _ := boundService(t)
	p.wait = true
	p.began = make(chan struct{})
	p.stopped = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.ReadWindow(ctx, "video:source:episode", 0, 1); done <- err }()
	<-p.began
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request did not stop")
	}
	select {
	case <-p.stopped:
	case <-time.After(time.Second):
		t.Fatal("upstream work did not stop")
	}
}
func TestBindingAndCacheKeysIsolateSourceAndProvider(t *testing.T) {
	s, _, _ := boundService(t)
	if got, err := s.Binding(context.Background(), "video:another-source:episode"); err != nil || got != nil {
		t.Fatal("binding crossed source identity", got, err)
	}
	e := Episode{Provider: "bilibili", EpisodeID: "123"}
	b := cacheKey(e, "1")
	e.Provider = "iqiyi"
	if b == cacheKey(e, "1") {
		t.Fatal("cross-platform cache collision")
	}
	if err := ValidateBinding(Binding{LocalKey: "same title", Episode: e}); err == nil {
		t.Fatal("unconfirmed binding accepted")
	}
}

type episodeCacheProvider struct{ testProvider }

func (p *episodeCacheProvider) Segment(_ context.Context, episode Episode, segment Segment) ([]Item, int, error) {
	p.calls.Add(1)
	return []Item{{ID: segment.Key, TimeMS: segment.StartMS + 1000, Text: episode.EpisodeID, Mode: 1, Color: 0xffffff, FontSize: 25}}, 0, nil
}

func TestCommentCacheReusesSamePoolButNeverAnotherEpisode(t *testing.T) {
	s, _, _ := boundService(t)
	p := &episodeCacheProvider{}
	s.providers["qq"] = p
	ctx := context.Background()
	for _, entry := range []struct{ local, remote string }{
		{"episode:1", "c4102g9a01t"}, {"other-source:episode:1", "c4102g9a01t"}, {"episode:2", "c4102g9a02t"},
	} {
		if err := s.store.SaveBinding(ctx, Binding{LocalKey: entry.local, Confirmed: true, Episode: Episode{Provider: "qq", EpisodeID: entry.remote}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, step := range []struct {
		local, remote string
		calls         int32
	}{
		{"episode:1", "c4102g9a01t", 2},
		{"other-source:episode:1", "c4102g9a01t", 2},
		{"episode:2", "c4102g9a02t", 4},
		{"episode:1", "c4102g9a01t", 4},
	} {
		value, err := s.ReadWindow(ctx, step.local, 0, 1)
		if err != nil || len(value.Items) != 2 || p.calls.Load() != step.calls {
			t.Fatal("unexpected cache reuse", step, value, err, p.calls.Load())
		}
		for _, item := range value.Items {
			if item.Text != step.remote {
				t.Fatal("comments leaked across episodes", step, item)
			}
		}
	}
}

func TestExpiredCacheIsPrunedOnReadAndFreshCapacityIsProtected(t *testing.T) {
	store := testStore(t)
	database, _ := store.db()
	ctx := context.Background()
	now := time.Now()
	if err := store.Put(ctx, "old", []byte("old"), now.Add(-31*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Cached(ctx, "old", now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*)FROM danmaku_cache`).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
	if err := store.Put(ctx, "fresh", []byte("fresh"), now); err != nil {
		t.Fatal(err)
	}
	// Exercise capacity policy without allocating 128 MiB in this test process.
	if _, err := database.Exec(`UPDATE danmaku_cache SET bytes=? WHERE cache_key='fresh'`, 128<<20); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "new", []byte("new"), now); err == nil {
		t.Fatal("exceeded budget by evicting a fresh item")
	}
	raw, _, err := store.Cached(ctx, "fresh", now)
	if err != nil || string(raw) != "fresh" {
		t.Fatal("fresh cache lost", err)
	}
	if err := database.QueryRow(`SELECT COUNT(*)FROM danmaku_cache WHERE cache_key='new'`).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed insert did not roll back", count, err)
	}
}
