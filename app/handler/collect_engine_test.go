package handler

import (
	"cczjVideo/app/collect"
	"sync"
	"sync/atomic"
	"testing"
)

func TestEngineEntryTryBindEngineHasOneWinner(t *testing.T) {
	entry := &engineEntry{status: &CollectStatus{SourceKey: "source-a"}}
	const contenders = 32
	start := make(chan struct{})
	results := make(chan *collect.Engine, contenders)
	var wait sync.WaitGroup
	var wins atomic.Int32
	for i := 0; i < contenders; i++ {
		engine := &collect.Engine{}
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			if entry.TryBindEngine(engine, "manual") {
				wins.Add(1)
				results <- engine
			}
		}()
	}
	close(start)
	wait.Wait()
	close(results)

	if got := wins.Load(); got != 1 {
		t.Fatalf("successful bindings = %d, want 1", got)
	}
	winner := <-results
	if winner == nil || !entry.IsRunning() {
		t.Fatal("winner was not retained as the running engine")
	}
	entry.FinishEngine(winner, "done")
	if entry.IsRunning() {
		t.Fatal("current completion did not end the run")
	}
}

func TestEngineEntryIgnoresStaleFinishCallback(t *testing.T) {
	entry := &engineEntry{status: &CollectStatus{SourceKey: "source-a"}}
	first := &collect.Engine{}
	second := &collect.Engine{}
	if !entry.TryBindEngine(first, "scheduled") {
		t.Fatal("first run did not bind")
	}
	entry.FinishEngine(first, "first completed")
	if !entry.TryBindEngine(second, "manual") {
		t.Fatal("second run did not bind")
	}
	entry.FinishEngine(first, "stale completion")

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.engine != second || !entry.status.Running {
		t.Fatalf("stale completion changed active run: engine=%p running=%v", entry.engine, entry.status.Running)
	}
	if entry.status.Log == "stale completion" {
		t.Fatal("stale completion overwrote current status")
	}
}
