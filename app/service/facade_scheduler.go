package service

import (
	"cczjVideo/app/handler"
	"time"
)

// ======================== 采集调度器 ========================

// GetCollectSchedule 返回采集调度器配置与运行状态
func (a *App) GetCollectSchedule() *handler.SchedulerStatus {
	return a.schedulers.Status()
}

// SetCollectSchedule 修改采集调度配置（持久化到 settings）
func (a *App) SetCollectSchedule(cfg handler.CollectScheduleConfig) (handler.CollectScheduleConfig, error) {
	return a.schedulers.SetConfig(cfg)
}

// TriggerCollectNow 立即触发一次后台采集（不影响定时）
// sourceKey 非空则仅采集该源；mode 可选 full/incremental/once
type TriggerCollectReq struct {
	SourceKey string `json:"source_key"`
	Mode      string `json:"mode"`  // full / incremental / once
	Hours     int    `json:"hours"` // 增量模式的回溯小时数
}

func (a *App) TriggerCollectNow(req TriggerCollectReq) (bool, error) {
	a.schedulers.Trigger(req.SourceKey, req.Mode, req.Hours)
	return true, nil
}

// StopBackgroundCollect 停止后台循环采集，并等待短时间让状态同步
func (a *App) StopBackgroundCollect() (bool, error) {
	a.schedulers.Stop()
	// 短暂等待确保 Stop 已把 running 置为 false 后返回（前端状态即时刷新）
	time.Sleep(50 * time.Millisecond)
	return true, nil
}

// SetSourceSchedule 设置单个源的调度配置
func (a *App) SetSourceSchedule(req handler.SourceScheduleReq) error {
	return a.schedulers.SetSourceSchedule(req)
}

// SetSourceStrategy 换掉某个采集源用的适配策略文档。扩展包把策略文档原样交给前端，
// 前端再原样交回来，所以这里只转发不解释；空串表示退回内置的 standard_cms。
// handler.SetSourceStrategy 已经让该源的缓存失效。
func (a *App) SetSourceStrategy(sourceKey string, strategyConfig string) error {
	return handler.SetSourceStrategy(sourceKey, strategyConfig)
}
