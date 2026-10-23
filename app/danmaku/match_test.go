package danmaku

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

type matchProviderFixture struct {
	name     string
	searches atomic.Int32
	search   func(context.Context) ([]Candidate, error)
	empty    bool
}

func (p *matchProviderFixture) Search(ctx context.Context, _ string) ([]Candidate, error) {
	p.searches.Add(1)
	if p.search != nil {
		return p.search(ctx)
	}
	return []Candidate{{Provider: p.name, Ref: "series", Title: "sample", Year: "2023", Kind: "电视剧"}}, nil
}
func (p *matchProviderFixture) Episodes(context.Context, string) ([]Episode, error) {
	id := "12345"
	if p.name == "qq" {
		id = "video01"
	}
	return []Episode{{Provider: p.name, SeriesID: "series", EpisodeID: id, Number: "1", Title: "正片", DurationMS: 120000}}, nil
}
func (p *matchProviderFixture) Index(context.Context, Episode) (Index, error) {
	if p.empty {
		return Index{Segments: []Segment{}}, nil
	}
	return Index{Segments: []Segment{{Key: "1", StartMS: 0, EndMS: 120000}}}, nil
}
func (p *matchProviderFixture) Segment(context.Context, Episode, Segment) ([]Item, int, error) {
	return []Item{}, 0, nil
}
func matchRequest() MatchRequest {
	return MatchRequest{LocalKey: "source:video:1", Title: "sample", Year: "2023", Kind: "电视剧", EpisodeLabel: "第01集", DurationMS: 120000, FreshDays: 1}
}
func matchService(t *testing.T, store *Store) *Service {
	t.Helper()
	s := NewService(store, NewClient(nil), context.Background())
	for _, name := range []string{"bilibili", "qq", "iqiyi"} {
		s.providers[name] = &matchProviderFixture{name: name}
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := s.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return s
}
func TestMatchParallelPriorityIsNotCompletionOrder(t *testing.T) {
	s := matchService(t, testStore(t))
	started := make(chan string, 3)
	release := make(chan struct{})
	for name, provider := range s.providers {
		p := provider.(*matchProviderFixture)
		p.search = func(ctx context.Context) ([]Candidate, error) {
			started <- name
			if name == "bilibili" {
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			return []Candidate{{Provider: name, Ref: "series", Title: "sample", Year: "2023", Kind: "电视剧"}}, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	done := make(chan MatchResult, 1)
	fail := make(chan error, 1)
	go func() { value, err := s.Match(ctx, matchRequest()); done <- value; fail <- err }()
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("providers did not start concurrently")
		}
	}
	select {
	case <-done:
		t.Fatal("a faster fallback outranked the default")
	default:
	}
	close(release)
	value := <-done
	if err := <-fail; err != nil || value.Binding == nil || value.Binding.Episode.Provider != "bilibili" {
		t.Fatal(value, err)
	}
}
func TestMatchFallbackFailureCooldownAcrossServiceRestart(t *testing.T) {
	store := testStore(t)
	now := time.Now()
	s := matchService(t, store)
	s.now = func() time.Time { return now }
	defaultSource := s.providers["bilibili"].(*matchProviderFixture)
	defaultSource.search = func(context.Context) ([]Candidate, error) { return []Candidate{}, nil }
	result, err := s.Match(context.Background(), matchRequest())
	if err != nil || result.Binding == nil || result.Binding.Episode.Provider != "qq" {
		t.Fatal(result, err)
	}
	restarted := matchService(t, store)
	restarted.now = func() time.Time { return now }
	restarted.providers["bilibili"] = defaultSource
	result, err = restarted.Match(context.Background(), matchRequest())
	if err != nil || result.Binding == nil || result.Binding.Episode.Provider != "qq" {
		t.Fatal(result, err)
	}
	if defaultSource.searches.Load() != 1 || result.Attempts[0].Status != "cooling_down" || result.Attempts[0].RetryAt != now.Add(24*time.Hour).Unix() {
		t.Fatal(defaultSource.searches.Load(), result.Attempts)
	}
	now = now.Add(24*time.Hour + time.Second)
	_, err = restarted.Match(context.Background(), matchRequest())
	if err != nil {
		t.Fatal(err)
	}
	if defaultSource.searches.Load() != 2 {
		t.Fatal("failure did not expire", defaultSource.searches.Load())
	}
}
func TestMatchAllUnavailableAndCancellationDoNotInventAvailability(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "unavailable", true: "cancelled"}[cancelled], func(t *testing.T) {
			store := testStore(t)
			s := matchService(t, store)
			began := make(chan struct{}, 3)
			for _, provider := range s.providers {
				provider.(*matchProviderFixture).search = func(ctx context.Context) ([]Candidate, error) {
					began <- struct{}{}
					if cancelled {
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return nil, errors.New("offline")
				}
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				value, err := s.Match(ctx, matchRequest())
				if !cancelled && value.Binding != nil {
					err = errors.New("unavailable provider was selected")
				}
				done <- err
			}()
			if cancelled {
				<-began
				cancel()
			}
			err := <-done
			if cancelled && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if !cancelled && err != nil {
				t.Fatal(err)
			}
			if !cancelled {
				value, err := s.Match(context.Background(), matchRequest())
				if err != nil || value.Binding != nil || len(value.Attempts) != 3 {
					t.Fatal(value, err)
				}
				for _, attempt := range value.Attempts {
					if attempt.Status != "cooling_down" || s.providers[attempt.Provider].(*matchProviderFixture).searches.Load() != 1 {
						t.Fatal("unavailable provider was retried during cooldown", attempt)
					}
				}
			}
			if err := s.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
			db, _ := store.db()
			var count int
			if err := db.QueryRow(`SELECT COUNT(*) FROM danmaku_match_failures`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if cancelled && count != 0 || !cancelled && count != 3 {
				t.Fatal("incorrect failure ledger", count)
			}
		})
	}
}
func TestMatchYearAndTypeDisambiguateButEmptyWindowIsNotEmptyPool(t *testing.T) {
	request := matchRequest()
	rows := []Candidate{{Title: "sample", Provider: "qq", Ref: "old", Year: "2020", Kind: "电视剧"}, {Title: "sample", Provider: "qq", Ref: "movie", Year: "2023", Kind: "电影"}, {Title: "sample", Provider: "qq", Ref: "right", Year: "2023", Kind: "电视剧"}}
	if value := matchingWork(rows, request); value == nil || value.Ref != "right" {
		t.Fatal(value)
	}
	request.Year = ""
	request.Kind = ""
	if value := matchingWork(rows, request); value == nil || value.Ref != "movie" {
		t.Fatal("equal work ranking did not select a stable identity", value)
	}
	request.Year = "2023"
	request.Kind = "国产剧"
	if value := matchingWork(rows, request); value == nil || value.Ref != "right" {
		t.Fatal("local category did not normalize to TV", value)
	}
	s := matchService(t, testStore(t))
	result, err := s.Match(context.Background(), matchRequest())
	if err != nil || result.Binding == nil || result.Window == nil || len(result.Window.Items) != 0 {
		t.Fatal("empty fragment should remain a valid pool", result, err)
	}
}

type blockingMatchProvider struct {
	matchProviderFixture
	started chan<- struct{}
	active  *atomic.Int32
	peak    *atomic.Int32
}

func (p *blockingMatchProvider) Search(ctx context.Context, _ string) ([]Candidate, error) {
	n := p.active.Add(1)
	defer p.active.Add(-1)
	for old := p.peak.Load(); n > old; old = p.peak.Load() {
		if p.peak.CompareAndSwap(old, n) {
			break
		}
	}
	p.started <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestMatchGlobalConcurrencyAndCancellationRelease(t *testing.T) {
	s := matchService(t, testStore(t))
	started := make(chan struct{}, 12)
	var active, peak atomic.Int32
	for name := range s.providers {
		s.providers[name] = &blockingMatchProvider{matchProviderFixture: matchProviderFixture{name: name}, started: started, active: &active, peak: &peak}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan error, 4)
	for i := 0; i < 4; i++ {
		request := matchRequest()
		request.LocalKey = fmt.Sprintf("video:%d:1", i)
		request.Title = fmt.Sprintf("distinct work %d", i)
		go func() { _, err := s.Match(ctx, request); done <- err }()
	}
	for i := 0; i < 3; i++ {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("probes did not start")
		}
	}
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for s.matchRuns.Load() < 4 {
		select {
		case <-ticker.C:
		case <-ctx.Done():
			t.Fatal("concurrent match requests did not all start")
		}
	}
	select {
	case <-started:
		t.Fatal("more than three provider pipelines ran concurrently")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	for i := 0; i < 4; i++ {
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("cancelled match did not return")
		}
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if active.Load() != 0 || peak.Load() != 3 || len(s.matchSlots) != 0 {
		t.Fatal("provider work leaked", active.Load(), peak.Load(), len(s.matchSlots))
	}
}

func TestMatchDisabledProvidersAndSavedOffset(t *testing.T) {
	store := testStore(t)
	database, _ := store.db()
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, SourceSettingsKey, `{"defaultProvider":"qq","disabled":["bilibili","iqiyi"]}`); err != nil {
		t.Fatal(err)
	}
	s := matchService(t, store)
	prior := Binding{LocalKey: matchRequest().LocalKey, Confirmed: true, OffsetMS: 3000, Episode: Episode{Provider: "qq", SeriesID: "series", EpisodeID: "video01", Number: "1"}}
	if err := store.SaveBinding(context.Background(), prior); err != nil {
		t.Fatal(err)
	}
	value, err := s.Match(context.Background(), matchRequest())
	if err != nil || value.Binding == nil || value.Binding.OffsetMS != 3000 || len(value.Attempts) != 1 || value.Attempts[0].Provider != "qq" {
		t.Fatal(value, err)
	}
	for _, name := range []string{"bilibili", "iqiyi"} {
		if s.providers[name].(*matchProviderFixture).searches.Load() != 0 {
			t.Fatal("disabled provider was queried", name)
		}
	}
	if _, err := database.Exec(`UPDATE settings SET value=? WHERE key=?`, `{"disabled":["bilibili","qq","iqiyi"]}`, SourceSettingsKey); err != nil {
		t.Fatal(err)
	}
	value, err = s.Match(context.Background(), matchRequest())
	if err != nil || value.Binding != nil || len(value.Attempts) != 0 || s.providers["qq"].(*matchProviderFixture).searches.Load() != 1 {
		t.Fatal("disabled sources did not settle without probing", value, err)
	}
}

func TestMatchEmptyPoolFallsBackAndFailureIdentityIsLocal(t *testing.T) {
	store := testStore(t)
	s := matchService(t, store)
	s.providers["bilibili"].(*matchProviderFixture).empty = true
	value, err := s.Match(context.Background(), matchRequest())
	if err != nil || value.Binding == nil || value.Binding.Episode.Provider != "qq" || value.Attempts[0].Code != "EMPTY_POOL" {
		t.Fatal(value, err)
	}
	request := matchRequest()
	failure, err := store.matchFailure(context.Background(), "other-source:video:1", "bilibili", matchFingerprint(request), time.Now())
	if err != nil || failure != nil {
		t.Fatal("another local video's failure was reused", failure, err)
	}
	failure, err = store.matchFailure(context.Background(), request.LocalKey, "qq", matchFingerprint(request), time.Now())
	if err != nil || failure != nil {
		t.Fatal("another provider's failure was reused", failure, err)
	}
}

func TestMatchWrappedTimeoutKeepsItsClassification(t *testing.T) {
	s := matchService(t, testStore(t))
	s.providers["bilibili"].(*matchProviderFixture).search = func(context.Context) ([]Candidate, error) {
		return nil, apperror.Wrap(apperror.Unavailable, context.DeadlineExceeded, "upstream unavailable")
	}
	value, err := s.Match(context.Background(), matchRequest())
	if err != nil || value.Binding == nil || value.Binding.Episode.Provider != "qq" || value.Attempts[0].Code != string(apperror.Timeout) {
		t.Fatal(value, err)
	}
}

func TestMatchExplainsTitleRejectionAndLogsEvenWithoutRecording(t *testing.T) {
	store := testStore(t)
	database, _ := store.db()
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, SourceSettingsKey, `{"disabled":["qq","iqiyi"]}`); err != nil {
		t.Fatal(err)
	}
	s := matchService(t, store)
	before := applog.Default().LatestSeq()
	request := matchRequest()
	request.Title = "unrelated title"
	value, err := s.Match(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if len(value.Attempts) != 1 || value.Attempts[0].Code != "WORK_TITLE_MISMATCH" {
		t.Error("title mismatch lost its reason", value.Attempts)
	}
	if value.TraceID == "" {
		t.Fatal("missing match correlation ID")
	}
	found := false
	for _, row := range applog.Default().Since(before, 100) {
		if strings.Contains(row.Message, "danmaku match provider") && strings.Contains(row.Message, "WORK_TITLE_MISMATCH") && strings.Contains(row.Message, value.TraceID) {
			found = true
		}
	}
	if !found {
		t.Error("no ordinary log explaining the failed provider")
	}
}

func TestMatchExplainsLegacyCooldownFromCacheWithoutNewRequests(t *testing.T) {
	store := testStore(t)
	database, _ := store.db()
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, SourceSettingsKey, `{"disabled":["qq","iqiyi"]}`); err != nil {
		t.Fatal(err)
	}
	s := matchService(t, store)
	request := matchRequest()
	request.Title = "unrelated title"
	if _, err := s.Search(context.Background(), "bilibili", request.Title); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := store.saveMatchFailure(context.Background(), request.LocalKey, "bilibili", matchFingerprint(request), "NO_WORK_MATCH", now); err != nil {
		t.Fatal(err)
	}
	value, err := s.Match(context.Background(), request)
	if err != nil || len(value.Attempts) != 1 {
		t.Fatal(value, err)
	}
	attempt := value.Attempts[0]
	if attempt.Code != "WORK_TITLE_MISMATCH" || attempt.Status != "cooling_down" || attempt.Stage != "work_cache" || !strings.Contains(attempt.Detail, "cached_evidence=true") || attempt.RetryAt != now.Add(24*time.Hour).Unix() {
		t.Fatal(attempt)
	}
	if s.providers["bilibili"].(*matchProviderFixture).searches.Load() != 1 {
		t.Fatal("cooldown explanation invoked upstream")
	}
	stored, err := store.matchFailure(context.Background(), request.LocalKey, "bilibili", matchFingerprint(request), now)
	if err != nil || stored == nil || stored.Code != "NO_WORK_MATCH" {
		t.Fatal("explanation modified the prior failure", stored, err)
	}
}

func TestEpisodeFailureExplainsRejectedPreviewInsteadOfMissingNumber(t *testing.T) {
	request := matchRequest()
	rows := []Episode{{Provider: "bilibili", EpisodeID: "1", Number: "1", Title: "1 凡人风起天南1 预告", DurationMS: 120000}, {Provider: "bilibili", EpisodeID: "2", Number: "2", Title: "正片", DurationMS: 120000}}
	if matchingEpisode(rows, request) != nil {
		t.Fatal("special version was silently matched")
	}
	code, detail := episodeMatchFailure(rows, request)
	if code != "EPISODE_SPECIAL_REJECTED" || !strings.Contains(detail, "special_same_number=1") {
		t.Fatal(code, detail)
	}
}

func TestLegacyEpisodeCooldownExplainsSpecialVersionWithoutUpstream(t *testing.T) {
	store := testStore(t)
	database, _ := store.db()
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, SourceSettingsKey, `{"disabled":["qq","iqiyi"]}`); err != nil {
		t.Fatal(err)
	}
	s := matchService(t, store)
	ctx := context.Background()
	request := matchRequest()
	now := time.Now()
	if _, err := s.Search(ctx, "bilibili", request.Title); err != nil {
		t.Fatal(err)
	}
	raw := []byte(`[{"provider":"bilibili","episodeId":"1","number":"1","title":"1 凡人风起天南1预告","durationMs":120000}]`)
	if err := store.Put(ctx, discoveryCacheKey("bilibili", "episodes", "series"), raw, now); err != nil {
		t.Fatal(err)
	}
	if err := store.saveMatchFailure(ctx, request.LocalKey, "bilibili", matchFingerprint(request), "NO_EPISODE_MATCH", now); err != nil {
		t.Fatal(err)
	}
	value, err := s.Match(ctx, request)
	if err != nil || len(value.Attempts) != 1 {
		t.Fatal(value, err)
	}
	attempt := value.Attempts[0]
	if attempt.Stage != "episode_cache" || attempt.Code != "EPISODE_SPECIAL_REJECTED" || attempt.Status != "cooling_down" || attempt.RetryAt != now.Add(24*time.Hour).Unix() {
		t.Fatal(attempt)
	}
	if s.providers["bilibili"].(*matchProviderFixture).searches.Load() != 1 {
		t.Fatal("explanation bypassed cooldown")
	}
}
