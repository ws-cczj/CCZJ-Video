// Package collection owns manually started collection runs and their events.
package collection

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
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
	// appCtx 是应用生命周期上下文（lifecycle.Group 的那一个，创建后不再更换）。
	// 采集引擎要在登记给界面之前挂上它，理由见 Start。
	appCtx context.Context
}

// NewService constructs a collection service.
func NewService(emit EmitFunc, background BackgroundFunc, appCtx context.Context) *Service {
	return &Service{emit: emit, background: background, appCtx: appCtx}
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
	// 上下文先挂再登记：TryBindEngine 一发布，"停止采集"和退出清理就都能立刻拿到
	// 这个引擎。等后台任务真正开跑才 SetContext 的话，中间那段窗口里取消信号
	// 无处可去，退出时这一趟采集会继续往已经关掉的库里写。
	engine.SetContext(s.appCtx)
	if !entry.TryBindEngine(engine, string(mode)) {
		return nil, apperror.Newf(apperror.Conflict, "采集源 %s 正在采集中", req.SourceKey)
	}

	run := func(context.Context) {
		stats, err := engine.Run()
		outcome := handler.OutcomeFromRun(stats, err)
		applog.InfoFields("collection finished", applog.Fields{
			"operation_id": operationID,
			"source_key":   req.SourceKey,
			"mode":         string(mode),
			"saved":        outcome.Saved,
			"error_kind":   outcome.ErrorKind,
			"error":        outcome.Log,
		})
		entry.FinishEngine(engine, outcome)
		handler.RecordCollectHealth(req.SourceKey, outcome, err)
		// 只有真的写进库才需要失效缓存：定时任务每轮都会跑，空跑一次就把整源缓存清光
		// 只会让下一次点开详情多打一趟采集接口。
		if outcome.Saved > 0 {
			cache.InvalidateSource(req.SourceKey, fmt.Sprintf("采集写入 %d 条", outcome.Saved))
		}
		payload := handler.CollectDonePayload(req.SourceKey, string(mode), outcome)
		payload["operation_id"] = operationID
		s.emit("collect:done", payload)
	}
	if !s.background("collection:"+req.SourceKey, run) {
		entry.FinishEngine(engine, handler.RunOutcome{Log: "application is shutting down"})
		return nil, apperror.New(apperror.Cancelled, "application is shutting down")
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
