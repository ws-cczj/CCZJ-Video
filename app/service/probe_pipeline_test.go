package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/model"
)

func setupProbePipeline(t *testing.T) {
	t.Helper()
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sourceProbeMu.Lock()
	sourceProbeStartedAt = map[string]time.Time{}
	sourceProbeMu.Unlock()
	for _, kind := range []string{"video", "novel", "comic"} {
		if err := db.AddSource(&model.Source{SourceKey: "probe_pipeline_" + kind, Name: kind, Kind: kind, ApiUrl: "https://example.invalid/", Enabled: 1}); err != nil {
			t.Fatal(err)
		}
	}
}

func TestProbeTimelineExcludesReadingSources(t *testing.T) {
	setupProbePipeline(t)
	rows, err := (&App{background: lifecycle.NewGroup()}).SourceProbeTimeline(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].SourceKey != "probe_pipeline_video" {
		t.Fatalf("video timeline leaked reading sources: %+v", rows)
	}
}

func TestCancelledProbeDoesNotConsumeCooldown(t *testing.T) {
	setupProbePipeline(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&App{}).probeAllSources(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	sourceProbeMu.Lock()
	defer sourceProbeMu.Unlock()
	if len(sourceProbeStartedAt) != 0 {
		t.Fatalf("cancelled jobs consumed cooldown: %+v", sourceProbeStartedAt)
	}
}

func TestSingleVideoProbeRejectsReadingSourceBeforeCooldown(t *testing.T) {
	setupProbePipeline(t)
	app := &App{background: lifecycle.NewGroup()}
	_, err := app.ProbeSource(context.Background(), "probe_pipeline_novel")
	if !apperror.Is(err, apperror.Validation) {
		t.Fatal(err)
	}
	sourceProbeMu.Lock()
	defer sourceProbeMu.Unlock()
	if len(sourceProbeStartedAt) != 0 {
		t.Fatal("reading source consumed video probe admission")
	}
}
