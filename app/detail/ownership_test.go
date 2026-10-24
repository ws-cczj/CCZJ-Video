package detail

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestLastDetailWaiterCancelsUpstreamHTTP(t *testing.T) {
	if err := db.InitDB(testDBDir); err != nil {
		t.Fatal(err)
	}
	started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-release:
			w.Write([]byte(`{"code":1,"list":[{"vod_id":"owned","vod_name":"owned"}]}`))
		}
	}))
	defer server.Close()
	defer close(release)
	key := "detail_cancel_owned"
	if err := db.AddSource(&model.Source{SourceKey: key, Name: key, ApiUrl: server.URL, Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteSource(key) })
	s := New(time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := s.getCatalogContext(ctx, &db.CatalogItem{SourceKey: key, SourceVodID: "owned", GlobalID: 99001})
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("HTTP never started")
	}
	cancel()
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("last waiter left an unowned HTTP request running")
	}
}

func waitJoined(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		_, joined := s.flights.Stats()
		if joined > 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("follower did not join shared fetch")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestDetailFollowerSurvivesFirstCancellationAndOwnsItsSnapshot(t *testing.T) {
	s := New(time.Minute)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	started, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	s.fetchFn = func(c *db.CatalogItem) (*Result, error) {
		count.Add(1)
		close(started)
		<-release
		return &Result{Video: &model.Video{VodName: "original"}, Lines: []*model.PlayLine{{Episodes: []*model.Episode{{}}}}}, nil
	}
	catalog := &db.CatalogItem{SourceKey: "joined", GlobalID: 41}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := s.getCatalogContext(ctx, catalog); first <- err }()
	<-started
	second := make(chan *Result, 1)
	go func() { value, _ := s.getCatalogContext(context.Background(), catalog); second <- value }()
	waitJoined(t, s)
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	value := <-second
	if value == nil || count.Load() != 1 {
		t.Fatal("follower lost shared work")
	}
	value.Video.VodName = "mutated"
	value.Lines[0].Episodes = nil
	cached, err := s.getCatalog(catalog)
	if err != nil || cached.Video.VodName != "original" || len(cached.Lines[0].Episodes) != 1 {
		t.Fatal("caller mutated cached detail", err)
	}
}

func TestDetailInvalidationDoesNotRepublishOlderFlight(t *testing.T) {
	s := New(time.Minute)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	started, release := make(chan struct{}), make(chan struct{})
	var count atomic.Int32
	s.fetchFn = func(c *db.CatalogItem) (*Result, error) {
		n := count.Add(1)
		if n == 1 {
			close(started)
			<-release
		}
		return &Result{Video: &model.Video{GlobalId: int64(n)}}, nil
	}
	catalog := &db.CatalogItem{SourceKey: "revision", GlobalID: 42}
	old := make(chan struct{})
	go func() { _, _ = s.getCatalog(catalog); close(old) }()
	<-started
	s.InvalidateSource(catalog.SourceKey)
	current, err := s.getCatalog(catalog)
	if err != nil || current.Video.GlobalId != 2 {
		t.Fatal("new generation joined old work", err)
	}
	close(release)
	<-old
	cached, err := s.getCatalog(catalog)
	if err != nil || cached.Video.GlobalId != 2 {
		t.Fatal("old flight repopulated current cache", err)
	}
}

func TestDetailCloseWaitsForWorkAndRejectsCachedReads(t *testing.T) {
	s := New(time.Minute)
	started, release := make(chan struct{}), make(chan struct{})
	s.fetchFn = func(c *db.CatalogItem) (*Result, error) {
		close(started)
		<-release
		return &Result{Video: &model.Video{}}, nil
	}
	catalog := &db.CatalogItem{SourceKey: "close", GlobalID: 43}
	result := make(chan error, 1)
	go func() { _, err := s.getCatalog(catalog); result <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := s.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("close declared success before work drained", err)
	}
	close(release)
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := <-result; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.getCatalog(catalog); !errors.Is(err, context.Canceled) {
		t.Fatal("closed detail owner accepted work", err)
	}
}
