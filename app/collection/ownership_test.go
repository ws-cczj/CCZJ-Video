package collection

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/model"
	sourceservice "cczjVideo/app/source"
)

func TestSourceChangeCancelsHTTPAndJoinsCollectionBeforeRetiringRows(t *testing.T) {
	started, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			close(stopped)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	key := "owned_source_change"
	if err := db.AddSource(&model.Source{SourceKey: key, Name: key, ApiUrl: server.URL, Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.DeleteSource(key) })
	owner := lifecycle.NewGroup()
	t.Cleanup(func() { owner.Stop(time.Second) })
	runs := NewService(nil, owner.Go, owner.Context())
	if _, err := runs.Start(context.Background(), CollectReq{SourceKey: key}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("collection did not start HTTP")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	source := sourceservice.NewServiceWithRuntime(runs, nil)
	if err := source.Clear(ctx, key); err != nil {
		t.Fatal("source change failed to stop collection", err)
	}
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal("collection left upstream HTTP running")
	}
	if runs.Status(key).Running {
		t.Fatal("source mutation completed before collection stopped")
	}
	if !runs.Status(key).Stopped {
		t.Fatal("user cancellation was recorded as an unhealthy source")
	}
}

func TestSourceChangeSerializesMutationsAndBlocksNewRuns(t *testing.T) {
	r := newRegistry()
	first, err := r.quiesce(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.bind("source", &collect.Engine{}, "full"); ok {
		t.Fatal("change admitted a concurrent collector")
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() {
		release, err := r.quiesce(ctx, "source")
		if release != nil {
			release()
		}
		waiting <- err
	}()
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatal("queued mutation ignored cancellation", err)
	}
	r.forget("source")
	first()
	r.engineMapMu.Lock()
	defer r.engineMapMu.Unlock()
	if len(r.engineMap) != 0 {
		t.Fatal("forgotten source retained its state")
	}
}

func TestRegistryInstancesAndStatusSlicesAreIndependent(t *testing.T) {
	a, b := newRegistry(), newRegistry()
	entry := a.GetOrCreateEngine("source")
	entry.status.Names = []string{"original"}
	entry.status.FetchFailures = []int{1}
	status := a.GetCollectStatus("source")
	status.Names[0] = "mutated"
	status.FetchFailures[0] = 99
	if a.GetCollectStatus("source").Names[0] != "original" || a.GetCollectStatus("source").FetchFailures[0] != 1 {
		t.Fatal("snapshot exposed mutable slices")
	}
	if len(b.engineMap) != 0 {
		t.Fatal("application instances share collection state")
	}
}
