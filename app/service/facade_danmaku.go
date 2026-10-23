package service

import (
	"context"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/danmaku"
)

func (a *App) danmakuContext(ctx context.Context) (context.Context, context.CancelFunc, error) {
	a.modeSwitchMu.RLock()
	defer a.modeSwitchMu.RUnlock()
	if a.currentMode() != "video" || a.danmaku == nil {
		return nil, nil, apperror.New(apperror.Unsupported, "弹幕仅在视频模式可用")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	return ctx, cancel, nil
}
func runDanmaku[T any](a *App, ctx context.Context, name string, work func(context.Context) (T, error)) (T, error) {
	return runRequest(a, ctx, "danmaku."+name, func(ctx context.Context) (T, error) {
		var zero T
		ctx, release, err := a.danmakuContext(ctx)
		if err != nil {
			return zero, err
		}
		defer release()
		return work(ctx)
	})
}
func (a *App) DanmakuSearch(ctx context.Context, provider, query string) ([]danmaku.Candidate, error) {
	return runDanmaku(a, ctx, "search", func(ctx context.Context) ([]danmaku.Candidate, error) { return a.danmaku.Search(ctx, provider, query) })
}
func (a *App) DanmakuMatch(ctx context.Context, request danmaku.MatchRequest) (danmaku.MatchResult, error) {
	return runDanmaku(a, ctx, "match", func(ctx context.Context) (danmaku.MatchResult, error) { return a.danmaku.Match(ctx, request) })
}
func (a *App) DanmakuEpisodes(ctx context.Context, provider, ref string) ([]danmaku.Episode, error) {
	return runDanmaku(a, ctx, "episodes", func(ctx context.Context) ([]danmaku.Episode, error) { return a.danmaku.Episodes(ctx, provider, ref) })
}
func (a *App) DanmakuBinding(ctx context.Context, key string) (*danmaku.Binding, error) {
	return runDanmaku(a, ctx, "binding", func(ctx context.Context) (*danmaku.Binding, error) { return a.danmaku.Binding(ctx, key) })
}
func (a *App) DanmakuBind(ctx context.Context, binding danmaku.Binding) error {
	_, err := runDanmaku(a, ctx, "bind", func(ctx context.Context) (bool, error) { return true, a.danmaku.Bind(ctx, binding) })
	return err
}
func (a *App) DanmakuUnbind(ctx context.Context, key string) error {
	_, err := runDanmaku(a, ctx, "unbind", func(ctx context.Context) (bool, error) { return true, a.danmaku.Unbind(ctx, key) })
	return err
}
func (a *App) DanmakuWindow(ctx context.Context, key string, positionMS int64, freshDays int) (*danmaku.Window, error) {
	return runDanmaku(a, ctx, "window", func(ctx context.Context) (*danmaku.Window, error) {
		return a.danmaku.ReadWindow(ctx, key, positionMS, freshDays)
	})
}
