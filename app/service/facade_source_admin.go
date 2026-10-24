package service

import (
	"cczjVideo/app/apperror"
	sourceservice "cczjVideo/app/source"

	"cczjVideo/app/media"
	"context"
	"encoding/json"
	"strings"
)

// ======================== 数据源详情 / 管理操作 ========================

// GetSourceDetail 返回某个 source_key 的字段定义和示例数据
func (a *App) GetSourceDetail(ctx context.Context, sourceKey string) (*sourceservice.SourceDetail, error) {
	return runRequest(a, ctx, "source.GetSourceDetail", func(ctx context.Context) (*sourceservice.SourceDetail, error) {
		return sourceservice.GetSourceDetailContext(ctx, sourceKey)
	})
}

// SourceActionReq 前端传入的数据源操作请求
type SourceActionReq struct {
	SourceKey string `json:"source_key"`
	Action    string `json:"action"` // truncate / recreate / delete_source
	VodId     string `json:"vod_id"` // action=delete_video 时使用
}

func (r *SourceActionReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		Action    string          `json:"action"`
		VodId     json.RawMessage `json:"vod_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.Action = raw.Action
	r.VodId = normalizeId(raw.VodId)
	return nil
}

// RunSourceAction 统一执行数据源管理操作
// action:
//   - truncate / recreate: 退役源目录并重置采集游标，保留源配置
//   - delete_source: 退役源内容并删除源配置
//   - delete_video: 将一条目录放入回收站，保留收藏和历史
func (a *App) RunSourceAction(ctx context.Context, req SourceActionReq) (string, error) {
	return runRequest(a, ctx, "source.RunSourceAction", func(ctx context.Context) (string, error) {
		if req.SourceKey == "" {
			return "", apperror.New(apperror.Validation, "source_key is empty")
		}
		switch strings.ToLower(strings.TrimSpace(req.Action)) {
		case "truncate":
			if err := a.sources.Clear(ctx, req.SourceKey); err != nil {
				return "", err
			}
			return "truncate ok", nil
		case "recreate":
			if err := a.sources.Clear(ctx, req.SourceKey); err != nil {
				return "", err
			}
			return "recreate ok", nil
		case "delete_source":
			if err := a.sources.Delete(ctx, req.SourceKey); err != nil {
				return "", err
			}
			return "delete_source ok", nil
		case "delete_video":
			if req.VodId == "" {
				return "", apperror.New(apperror.Validation, "vod_id is empty")
			}
			if err := a.media.Delete(ctx, media.DeleteVideoReq{SourceKey: req.SourceKey, VodId: req.VodId}); err != nil {
				return "", err
			}
			return "delete_video ok", nil
		default:
			return "", apperror.Newf(apperror.Validation, "unknown action: %s", req.Action)
		}
	})
}
