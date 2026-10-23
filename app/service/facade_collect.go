package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"context"
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

// GetCollectStatuses bounds one IPC snapshot and deduplicates identities before
// reading memory. These short lock-protected reads do not benefit from workers.
func (a *App) GetCollectStatuses(ctx context.Context, keys []string) ([]*handler.CollectStatus, error) {
	return runRequest(a, ctx, "video.GetCollectStatuses", func(ctx context.Context) ([]*handler.CollectStatus, error) {
		if len(keys) > 256 {
			return nil, apperror.New(apperror.Validation, "采集状态批次不能超过 256 个源")
		}
		for _, key := range keys {
			if err := model.ValidateSourceKey(key); err != nil {
				return nil, apperror.Wrap(apperror.Validation, err, "采集源身份无效")
			}
		}
		seen := make(map[string]bool, len(keys))
		rows := make([]*handler.CollectStatus, 0, len(keys))
		for _, key := range keys {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !seen[key] {
				seen[key] = true
				rows = append(rows, a.collection.Status(key))
			}
		}
		return rows, nil
	})
}

// SearchSource 用 wd=keyword 去指定源站搜索指定页，返回富字段结果（不入库）
// page=1 开始；pageSize<=0 时使用源的默认条数
// 入库请调用 ImportSourceVideos
func (a *App) SearchSource(ctx context.Context, sourceKey string, keyword string, page int, pageSize int) (*handler.SearchSourceResult, error) {
	return runRequest(a, ctx, "video.SearchSource", func(ctx context.Context) (*handler.SearchSourceResult, error) {
		return handler.SearchSourceContext(ctx, sourceKey, keyword, page, pageSize, "")
	})
}

// SearchSourceRequest keeps progress owned by one UI search, including retries
// of the same source/keyword/page. The older binding remains compatible.
func (a *App) SearchSourceRequest(ctx context.Context, sourceKey, keyword string, page, pageSize int, requestID string) (*handler.SearchSourceResult, error) {
	return runRequest(a, ctx, "video.SearchSourceRequest", func(ctx context.Context) (*handler.SearchSourceResult, error) {
		if requestID == "" || len(requestID) > 128 {
			return nil, apperror.New(apperror.Validation, "搜索请求标识无效")
		}
		return handler.SearchSourceContext(ctx, sourceKey, keyword, page, pageSize, requestID)
	})
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
