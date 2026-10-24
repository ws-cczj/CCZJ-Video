// Package collection owns manually started collection runs and their events.
package collection

import (
	"cczjVideo/app/apperror"

	"cczjVideo/app/collect"

	"cczjVideo/app/db"
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
	registry   *Registry
	emit       EmitFunc
	background BackgroundFunc
	// appCtx 是应用生命周期上下文（lifecycle.Group 的那一个，创建后不再更换）。
	// 采集引擎要在登记给界面之前挂上它，理由见 Start。
	appCtx          context.Context
	contextProvider func() context.Context
}

// NewService constructs a collection service.
func NewService(emit EmitFunc, background BackgroundFunc, appCtx context.Context) *Service {
	if emit == nil {
		emit = func(string, any) {}
	}
	return &Service{registry: newRegistry(), emit: emit, background: background, appCtx: appCtx}
}

// SetContextProvider is wired before startup; each run captures its mode owner.
func (s *Service) SetContextProvider(provider func() context.Context) { s.contextProvider = provider }

// Start begins a manual collection run for a source.
func (s *Service) Start(ctx context.Context, req CollectReq) (*CollectStatus, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := model.ValidateSourceKey(req.SourceKey); err != nil {
		return nil, apperror.Wrap(apperror.Validation, err, "采集源身份无效")
	}
	source, err := db.GetSourceByKeyContext(ctx, req.SourceKey)
	if err != nil {
		return nil, err
	}
	if source.GetKind() != model.SourceKindVideo {
		return nil, apperror.New(apperror.Unsupported, "采集只支持视频源")
	}
	if req.Mode != "" && req.Mode != "full" && req.Mode != "incremental" && req.Mode != "once" {
		return nil, apperror.New(apperror.Validation, "采集模式无效")
	}

	operationID := fmt.Sprintf("collect-%d", time.Now().UnixNano())
	var entry *engineEntry

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
	owner := s.appCtx
	if s.contextProvider != nil {
		owner = s.contextProvider()
	}
	if err := owner.Err(); err != nil {
		return nil, err
	}
	owner, operationID = runContext(owner, ctx, req.SourceKey)
	engine.SetContext(owner)
	var accepted bool
	entry, accepted = s.registry.bind(req.SourceKey, engine, string(mode))
	if !accepted {
		engine.Stop()
		return nil, apperror.Newf(apperror.Conflict, "采集源 %s 正在采集中", req.SourceKey)
	}

	run := func(context.Context) { s.execute(engine, entry, req.SourceKey, string(mode), operationID) }

	if !s.background("collection:"+req.SourceKey, run) {
		engine.Stop()
		entry.FinishEngine(engine, RunOutcome{Log: "application is shutting down"})
		return nil, apperror.New(apperror.Cancelled, "application is shutting down")
	}

	return s.registry.GetCollectStatus(req.SourceKey), nil
}

// Pause pauses a source collection run.
func (s *Service) Pause(sourceKey string) bool {
	return s.registry.PauseCollect(sourceKey)
}

// Resume resumes a source collection run.
func (s *Service) Resume(sourceKey string) bool {
	return s.registry.ResumeCollect(sourceKey)
}

// Stop requests a source collection run to stop.
func (s *Service) Stop(sourceKey string) bool {
	return s.registry.StopCollect(sourceKey)
}

// Status returns the current state for a source collection run.
func (s *Service) Status(sourceKey string) *CollectStatus {
	return s.registry.GetCollectStatus(sourceKey)
}

func (s *Service) Quiesce(ctx context.Context, key string) (func(), error) {
	return s.registry.quiesce(ctx, key)
}
func (s *Service) Forget(key string) { s.registry.forget(key) }
