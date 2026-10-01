package service

import (
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
)

// ======================== Collect ========================

func (a *App) StartCollect(req handler.CollectReq) (*handler.CollectStatus, error) {
	return a.collection.Start(req)
}

// PauseCollect 暂停指定 source_key 的采集
func (a *App) PauseCollect(req handler.CollectReq) (bool, error) {
	return a.collection.Pause(req.SourceKey), nil
}

// ResumeCollect 恢复指定 source_key 的采集
func (a *App) ResumeCollect(req handler.CollectReq) (bool, error) {
	return a.collection.Resume(req.SourceKey), nil
}

// StopCollect 停止指定 source_key 的采集
func (a *App) StopCollect(req handler.CollectReq) (bool, error) {
	return a.collection.Stop(req.SourceKey), nil
}

// GetCollectStatus 返回指定 source 的采集状态
func (a *App) GetCollectStatus(sourceKey string) *handler.CollectStatus {
	return a.collection.Status(sourceKey)
}

// SearchSource 用 wd=keyword 去指定源站搜索指定页，返回富字段结果（不入库）
// page=1 开始；pageSize<=0 时使用源的默认条数
// 入库请调用 ImportSourceVideos
func (a *App) SearchSource(sourceKey string, keyword string, page int, pageSize int) (*handler.SearchSourceResult, error) {
	return handler.SearchSource(sourceKey, keyword, page, pageSize)
}

// ImportSourceVideos 将用户挑选的源站搜索视频入库（压缩字段 + 合并写入）
// sourceKey 用于确定入库的目标源表；videos 中携带的豆瓣信息也会写入全局 douban_info 表
// 返回成功入库的条数
func (a *App) ImportSourceVideos(sourceKey string, videos []*model.Video) (int, error) {
	return handler.ImportSourceVideos(sourceKey, videos)
}

// GetSourceParamsDoc 返回采集接口参数规范，供前端展示规则指南
func (a *App) GetSourceParamsDoc(sourceKey string) (*handler.SourceParamsDoc, error) {
	return handler.GetSourceParamsDoc(sourceKey)
}
