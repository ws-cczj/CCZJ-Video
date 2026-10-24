package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/operation"
	"context"
	"strings"
)

// Wails injects a per-call context; application shutdown is a separate owner.
func (a *App) requestContext(ctx context.Context) (context.Context, context.CancelFunc) {
	group := a.modeWork()
	release, _ := group.Track("requests")
	ctx, cancel := lifecycle.WithShutdown(ctx, group.Context())
	return ctx, func() { cancel(); release() }
}

// runRequest gives mode-owned RPC work one cancellation, recovery and diagnostic
// path. Mode switching and host controls retain their separate ownership.
func runRequest[T any](a *App, ctx context.Context, name string, work func(context.Context) (T, error)) (T, error) {
	a.modeSwitchMu.RLock()
	mode := a.currentMode()
	ctx, release := a.requestContext(ctx)
	a.modeSwitchMu.RUnlock()
	defer release()
	return operation.Run(ctx, operation.Meta{Name: name, Mode: mode, Pipeline: requestPipeline(name)}, a.observeOperation, work)
}

func (a *App) observeOperation(result operation.Result) {
	operation.Log(result)
	if a.operations != nil {
		a.operations.Record(result)
	}
}

// Preserve existing operation names while matching the frontend binding channel.
var requestPipelineAliases = map[string]string{
	"video.detail": "GetVideoDetail", "video.line_probe": "SpeedTestPlayLines",
	"reading.state_load": "ReadingStateLoad", "reading.state_apply": "ReadingStateApply", "douban.comments": "GetDoubanComments",
	"danmaku.search": "DanmakuSearch", "danmaku.match": "DanmakuMatch", "danmaku.episodes": "DanmakuEpisodes",
	"danmaku.binding": "DanmakuBinding", "danmaku.bind": "DanmakuBind", "danmaku.unbind": "DanmakuUnbind", "danmaku.window": "DanmakuWindow",
}

func requestPipeline(name string) string {
	if method, ok := requestPipelineAliases[name]; ok {
		return "rpc." + method
	}
	if index := strings.LastIndexByte(name, '.'); index >= 0 {
		name = name[index+1:]
	}
	return "rpc." + name
}

func runRequestError(a *App, ctx context.Context, name string, work func(context.Context) error) error {
	_, err := runRequest(a, ctx, name, func(ctx context.Context) (struct{}, error) { return struct{}{}, work(ctx) })
	return err
}

// Host work survives a mode switch, but is cancelled and drained before the
// application's resources close. Update download belongs to this lifetime.
func runHostRequest[T any](a *App, ctx context.Context, name string, work func(context.Context) (T, error)) (T, error) {
	release, _ := a.background.Track("host.requests")
	defer release()
	ctx, stop := lifecycle.WithShutdown(ctx, a.background.Context())
	defer stop()
	return operation.Run(ctx, operation.Meta{Name: name, Mode: a.currentMode(), Pipeline: requestPipeline(name)}, a.observeOperation, work)
}

// Admission keeps task acceptance in the same mode epoch as its request owner.
// The lock covers only registration, never the accepted background work.
func videoAdmission[T any](a *App, ctx context.Context, name string, work func(context.Context) (T, error)) (value T, err error) {
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.currentMode() != "video" {
		return value, apperror.New(apperror.Conflict, "请切换到视频模式后开始此任务")
	}
	ctx, release := a.requestContext(ctx)
	defer release()
	return operation.Run(ctx, operation.Meta{Name: name, Mode: "video", Pipeline: requestPipeline(name)}, a.observeOperation, work)
}
