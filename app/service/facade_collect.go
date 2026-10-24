package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	collectionservice "cczjVideo/app/collection"
	sourceservice "cczjVideo/app/source"

	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"context"
)

// ======================== Collect ========================

func (a *App) StartCollect(ctx context.Context, req collectionservice.CollectReq) (*collectionservice.CollectStatus, error) {
	return videoAdmission(a, ctx, "video.StartCollect", func(ctx context.Context) (*collectionservice.CollectStatus, error) {
		return a.collection.Start(ctx, req)
	})
}

// PauseCollect 暂停指定 source_key 的采集
func (a *App) PauseCollect(req collectionservice.CollectReq) (bool, error) {
	return a.collection.Pause(req.SourceKey), nil
}

// ResumeCollect 恢复指定 source_key 的采集
func (a *App) ResumeCollect(req collectionservice.CollectReq) (bool, error) {
	return a.collection.Resume(req.SourceKey), nil
}

// StopCollect 停止指定 source_key 的采集
func (a *App) StopCollect(req collectionservice.CollectReq) (bool, error) {
	return a.collection.Stop(req.SourceKey), nil
}

// GetCollectStatus 返回指定 source 的采集状态
func (a *App) GetCollectStatus(sourceKey string) *collectionservice.CollectStatus {
	if a.collection == nil {
		return &collectionservice.CollectStatus{SourceKey: sourceKey}
	}
	return a.collection.Status(sourceKey)
}

// GetCollectStatuses bounds one IPC snapshot and deduplicates identities before
// reading memory. These short lock-protected reads do not benefit from workers.
func (a *App) GetCollectStatuses(ctx context.Context, keys []string) ([]*collectionservice.CollectStatus, error) {
	return runRequest(a, ctx, "video.GetCollectStatuses", func(ctx context.Context) ([]*collectionservice.CollectStatus, error) {
		if len(keys) > 256 {
			return nil, apperror.New(apperror.Validation, "采集状态批次不能超过 256 个源")
		}
		for _, key := range keys {
			if err := model.ValidateSourceKey(key); err != nil {
				return nil, apperror.Wrap(apperror.Validation, err, "采集源身份无效")
			}
		}
		seen := make(map[string]bool, len(keys))
		rows := make([]*collectionservice.CollectStatus, 0, len(keys))
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
func (a *App) SearchSource(ctx context.Context, sourceKey string, keyword string, page int, pageSize int) (*sourceservice.SearchSourceResult, error) {
	return runRequest(a, ctx, "video.SearchSource", func(ctx context.Context) (*sourceservice.SearchSourceResult, error) {
		return a.sources.SearchContext(ctx, sourceKey, keyword, page, pageSize, "")
	})
}

// SearchSourceRequest keeps progress owned by one UI search, including retries
// of the same source/keyword/page. The older binding remains compatible.
func (a *App) SearchSourceRequest(ctx context.Context, sourceKey, keyword string, page, pageSize int, requestID string) (*sourceservice.SearchSourceResult, error) {
	ctx = operation.WithFields(ctx, applog.Fields{"source_key": sourceKey, "keyword": keyword, "page": page, "page_size": pageSize, "ui_request_id": requestID})
	return runRequest(a, ctx, "video.SearchSourceRequest", func(ctx context.Context) (*sourceservice.SearchSourceResult, error) {
		if requestID == "" || len(requestID) > 128 {
			return nil, apperror.New(apperror.Validation, "搜索请求标识无效")
		}
		return a.sources.SearchContext(ctx, sourceKey, keyword, page, pageSize, requestID)
	})
}

// ImportSourceVideos stores catalog projections of explicitly selected videos.
// The domain validates video-source ownership and returns the accepted count.
func (a *App) ImportSourceVideos(ctx context.Context, sourceKey string, videos []*model.Video) (int, error) {
	return runRequest(a, ctx, "video.ImportSourceVideos", func(ctx context.Context) (int, error) { return a.media.Import(ctx, sourceKey, videos) })
}

// GetSourceParamsDoc 返回采集接口参数规范，供前端展示规则指南
func (a *App) GetSourceParamsDoc(sourceKey string) (*sourceservice.SourceParamsDoc, error) {
	return sourceservice.GetSourceParamsDoc(sourceKey)
}
