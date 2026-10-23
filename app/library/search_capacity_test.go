package library

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/model"
)

func TestConcurrentQueriesShareFiveSourceSearchSlots(t *testing.T) {
	s := setupLibraryTest(t)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	entered := make(chan struct{}, 24)
	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })
	var active, peak atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old; old = peak.Load() {
			if peak.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		if _, err := fmt.Fprint(w, `<ul class="results"></ul>`); err != nil {
			t.Error(err)
		}
	}))
	defer server.Close()
	// Release handlers before httptest shutdown even if an assertion fails.
	defer once.Do(func() { close(release) })
	for i := 0; i < 12; i++ {
		addTestSource(t, server, fmt.Sprintf("capacity_%055d", i), novelDocument())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results := make(chan error, 2)
	for _, keyword := range []string{"query-a", "query-b"} {
		go func(keyword string) {
			_, err := s.Search(ctx, model.SourceKindNovel, keyword, 1, nil)
			results <- err
		}(keyword)
	}
	for i := 0; i < 5; i++ {
		select {
		case <-entered:
		case <-ctx.Done():
			t.Fatal("five searches did not enter", ctx.Err())
		}
	}
	select {
	case <-entered:
		t.Error("a sixth source request entered before any of the five completed")
	case <-time.After(50 * time.Millisecond):
	}
	once.Do(func() { close(release) })
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if peak.Load() != 5 || active.Load() != 0 {
		t.Fatal("wrong peak or unfinished requests", peak.Load(), active.Load())
	}
}
