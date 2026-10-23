package service

import (
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
	return operation.Run(ctx, operation.Meta{Name: name, Mode: mode, Pipeline: requestPipeline(name)}, func(result operation.Result) {
		operation.Log(result)
		if a.operations != nil {
			a.operations.Record(result)
		}
	}, work)
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
