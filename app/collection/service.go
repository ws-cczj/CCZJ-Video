// Package collection owns manually started collection runs and their events.
package collection

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"context"
	"fmt"
	"time"
)

// EmitFunc publishes collection events to the UI layer.
type EmitFunc func(name string, data any)

// BackgroundFunc registers application-owned work with a lifecycle manager.
type BackgroundFunc func(name string, fn func(context.Context)) bool

// Service starts and controls collection runs without depending on Wails.
type Service struct {
	emit       EmitFunc
	background BackgroundFunc
}

// NewService constructs a collection service.
func NewService(emit EmitFunc, background BackgroundFunc) *Service {
	return &Service{emit: emit, background: background}
}

// Start begins a manual collection run for a source.
func (s *Service) Start(req handler.CollectReq) (*handler.CollectStatus, error) {
	operationID := fmt.Sprintf("collect-%d", time.Now().UnixNano())
	entry := handler.GetOrCreateEngine(req.SourceKey)

	mode := model.CollectMode(req.Mode)
	if mode == "" {
		mode = model.CollectModeFull
	}

	options := []collect.EngineOption{collect.WithCollectMode(mode)}
	if mode == model.CollectModeIncremental && req.Hours > 0 {
		options = append(options, collect.WithTimeHours(req.Hours))
	}

	engine := collect.NewEngineV2(
		req.SourceKey,
		func(message string) {
			s.emit("collect:log", map[string]any{
				"operation_id": operationID,
				"source_key":   req.SourceKey,
				"message":      message,
			})
		},
		func(current, total int) {
			entry.UpdateProgress(current, total)
			s.emit("collect:progress", map[string]any{
				"operation_id": operationID,
				"source_key":   req.SourceKey,
				"current":      current,
				"total":        total,
			})
		},
		func(page int, names []string) {
			entry.UpdatePageNames(page, names)
			s.emit("collect:page", map[string]any{
				"operation_id": operationID,
				"source_key":   req.SourceKey,
				"page":         page,
				"names":        names,
			})
		},
		options...,
	)
	if !entry.TryBindEngine(engine, string(mode)) {
		return nil, fmt.Errorf("采集源 %s 正在采集中", req.SourceKey)
	}

	run := func(ctx context.Context) {
		engine.SetContext(ctx)
		_, err := engine.Run()
		applog.InfoFields("collection finished", applog.Fields{
			"operation_id": operationID,
			"source_key":   req.SourceKey,
			"mode":         string(mode),
			"error":        errorString(err),
		})
		entry.FinishEngine(engine, errorString(err))
		s.emit("collect:done", map[string]any{
			"operation_id": operationID,
			"source_key":   req.SourceKey,
			"error":        errorString(err),
			"mode":         string(mode),
		})
	}
	if s.background != nil {
		if !s.background("collection:"+req.SourceKey, run) {
			entry.FinishEngine(engine, "application is shutting down")
			return nil, fmt.Errorf("application is shutting down")
		}
	} else {
		go run(context.Background())
	}

	return handler.GetCollectStatus(req.SourceKey), nil
}

// Pause pauses a source collection run.
func (s *Service) Pause(sourceKey string) bool {
	return handler.PauseCollect(sourceKey)
}

// Resume resumes a source collection run.
func (s *Service) Resume(sourceKey string) bool {
	return handler.ResumeCollect(sourceKey)
}

// Stop requests a source collection run to stop.
func (s *Service) Stop(sourceKey string) bool {
	return handler.StopCollect(sourceKey)
}

// Status returns the current state for a source collection run.
func (s *Service) Status(sourceKey string) *handler.CollectStatus {
	return handler.GetCollectStatus(sourceKey)
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
