package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/handler"
	"encoding/json"
	"strings"
)

// ======================== 数据源详情 / 管理操作 ========================

// GetSourceDetail 返回某个 source_key 的字段定义和示例数据
func (a *App) GetSourceDetail(sourceKey string) (*handler.SourceDetail, error) {
	return handler.GetSourceDetail(sourceKey)
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
//   - truncate       // 仅清空该源的视频/剧集/分类数据（保留 source 元信息）
//   - recreate       // 删除并重建该源的三张表（数据全部丢失）
//   - delete_source  // 删除该源的所有表 + sources 记录
//   - delete_video   // 删除单条 vod_id（同时清理剧集）
func (a *App) RunSourceAction(req SourceActionReq) (string, error) {
	if req.SourceKey == "" {
		return "", apperror.New(apperror.Validation, "source_key is empty")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "truncate":
		if _, err := handler.TruncateSourceData(req.SourceKey); err != nil {
			return "", err
		}
		return "truncate ok", nil
	case "recreate":
		if _, err := handler.RecreateSourceTables(req.SourceKey); err != nil {
			return "", err
		}
		return "recreate ok", nil
	case "delete_source":
		// 先删表，再删 sources 记录
		if err := handler.DeleteSource(req.SourceKey); err != nil {
			return "", err
		}
		return "delete_source ok", nil
	case "delete_video":
		if req.VodId == "" {
			return "", apperror.New(apperror.Validation, "vod_id is empty")
		}
		if _, err := handler.DeleteSourceVideo(req.SourceKey, req.VodId); err != nil {
			return "", err
		}
		return "delete_video ok", nil
	default:
		return "", apperror.Newf(apperror.Validation, "unknown action: %s", req.Action)
	}
}
