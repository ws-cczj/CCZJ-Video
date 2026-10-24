package service

import (
	collectionservice "cczjVideo/app/collection"

	"context"
)

// ======================== 采集调度器 ========================

// GetCollectSchedule 返回采集调度器配置与运行状态
func (a *App) GetCollectSchedule() *collectionservice.SchedulerStatus {
	return a.schedulers.Status()
}

// SetCollectSchedule 修改采集调度配置（持久化到 settings）
func (a *App) SetCollectSchedule(cfg collectionservice.CollectScheduleConfig) (collectionservice.CollectScheduleConfig, error) {
	return a.schedulers.SetConfig(cfg)
}

// TriggerCollectNow 立即触发一次后台采集（不影响定时）
// sourceKey 非空则仅采集该源；mode 可选 full/incremental/once
type TriggerCollectReq struct {
	SourceKey string `json:"source_key"`
	Mode      string `json:"mode"`  // full / incremental / once
	Hours     int    `json:"hours"` // 增量模式的回溯小时数
}

func (a *App) TriggerCollectNow(ctx context.Context, req TriggerCollectReq) (bool, error) {
	return videoAdmission(a, ctx, "video.TriggerCollectNow", func(ctx context.Context) (bool, error) {
		return a.schedulers.Trigger(ctx, req.SourceKey, req.Mode, req.Hours)
	})
}

// StopBackgroundCollect 停止后台循环采集，并等待短时间让状态同步
func (a *App) StopBackgroundCollect() (bool, error) {
	a.schedulers.StopCollection()
	// 短暂等待确保 Stop 已把 running 置为 false 后返回（前端状态即时刷新）
	return true, nil
}

// SetSourceSchedule 设置单个源的调度配置
func (a *App) SetSourceSchedule(ctx context.Context, req collectionservice.SourceScheduleReq) error {
	return runRequestError(a, ctx, "video.SetSourceSchedule", func(ctx context.Context) error { return a.schedulers.SetSourceSchedule(ctx, req) })
}

// SetSourceStrategy 换掉某个采集源用的适配策略文档。扩展包把策略文档原样交给前端，
// 前端再原样交回来，所以这里只转发不解释；空串表示退回内置的 standard_cms。
// sourceservice.SetSourceStrategy 已经让该源的缓存失效。
func (a *App) SetSourceStrategy(ctx context.Context, sourceKey, config string) error {
	return runRequestError(a, ctx, "source.SetSourceStrategy", func(ctx context.Context) error { return a.sources.Strategy(ctx, sourceKey, config) })
}
