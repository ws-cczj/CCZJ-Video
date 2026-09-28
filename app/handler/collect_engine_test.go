package handler

import (
	"cczjVideo/app/collect"
	"errors"
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
	entry.FinishEngine(winner, RunOutcome{Log: "done"})
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
	entry.FinishEngine(first, RunOutcome{Log: "first completed"})
	if !entry.TryBindEngine(second, "manual") {
		t.Fatal("second run did not bind")
	}
	entry.FinishEngine(first, RunOutcome{Log: "stale completion"})

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.engine != second || !entry.status.Running {
		t.Fatalf("stale completion changed active run: engine=%p running=%v", entry.engine, entry.status.Running)
	}
	if entry.status.Log == "stale completion" {
		t.Fatal("stale completion overwrote current status")
	}
}

// A run that dropped whole pages must leave that visible in the status, and the
// next run must not inherit it.
func TestFinishEngineRecordsFailedPagesAndRebindClearsThem(t *testing.T) {
	entry := &engineEntry{status: &CollectStatus{SourceKey: "source-a"}}
	engine := &collect.Engine{}
	if !entry.TryBindEngine(engine, "incremental") {
		t.Fatal("run did not bind")
	}

	stats := &collect.RunStats{Saved: 120, FetchFailures: []int{4}, SaveFailures: []int{6}, ElapsedSeconds: 2.5}
	entry.FinishEngine(engine, OutcomeFromRun(stats, &collect.CollectError{Stats: stats}))

	status := entry.snapshotStatus()
	if status.ErrorKind != "partial" || status.Saved != 120 || status.ElapsedMs != 2500 {
		t.Fatalf("outcome not surfaced: kind=%q saved=%d elapsed=%d", status.ErrorKind, status.Saved, status.ElapsedMs)
	}
	if len(status.FetchFailures) != 1 || len(status.SaveFailures) != 1 {
		t.Fatalf("failed pages lost: fetch=%v save=%v", status.FetchFailures, status.SaveFailures)
	}
	if status.FinishedAtUnix == 0 {
		t.Fatal("finish timestamp missing")
	}

	if !entry.TryBindEngine(&collect.Engine{}, "incremental") {
		t.Fatal("second run did not bind")
	}
	fresh := entry.snapshotStatus()
	if fresh.ErrorKind != "" || len(fresh.FetchFailures) != 0 || len(fresh.SaveFailures) != 0 || fresh.Saved != 0 {
		t.Fatalf("new run inherited the previous run's failures: %+v", fresh)
	}
}

func TestOutcomeFromRunTreatsFatalErrorAsNoPagesLanded(t *testing.T) {
	outcome := OutcomeFromRun(&collect.RunStats{ElapsedSeconds: 0.2}, errors.New("第 1 页采集失败"))
	if outcome.ErrorKind != "fatal" || outcome.Log == "" {
		t.Fatalf("fatal run mislabelled: kind=%q log=%q", outcome.ErrorKind, outcome.Log)
	}
	if outcome.Saved != 0 || len(outcome.FetchFailures) != 0 {
		t.Fatalf("fatal run must not report landed pages: %+v", outcome)
	}
}

func TestClassifyCollectRunGradesHealthSamples(t *testing.T) {
	cases := []struct {
		name        string
		outcome     RunOutcome
		err         error
		wantOK      bool
		wantProblem string
	}{
		{
			name:    "完整成功",
			outcome: RunOutcome{Saved: 20},
			wantOK:  true,
		},
		{
			name:        "整轮失败带错误原因",
			outcome:     RunOutcome{Saved: 3},
			err:         errors.New("第 4 页取页失败: 502"),
			wantProblem: "第 4 页取页失败: 502",
		},
		{
			name:        "取页缺页也算不健康",
			outcome:     RunOutcome{Saved: 18, FetchFailures: []int{2, 7}},
			wantProblem: "2 页取页失败",
		},
		{
			name:        "入库缺页也算不健康",
			outcome:     RunOutcome{Saved: 19, SaveFailures: []int{5}},
			wantProblem: "1 页入库失败",
		},
		{
			name:    "只有空页不扣健康度",
			outcome: RunOutcome{Saved: 20, EmptyPages: []int{9}},
			wantOK:  true,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			ok, problem := classifyCollectRun(tt.outcome, tt.err)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if problem != tt.wantProblem {
				t.Fatalf("problem = %q, want %q", problem, tt.wantProblem)
			}
		})
	}
}
