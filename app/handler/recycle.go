// Package handler 回收站入口。
//
// 删除视频一直是软删除（source_videos.lifecycle_state='deleted'），但界面上没有任何地方
// 能看到或撤销这些行，文案还写着"永久删除并清理收藏和历史"。这一层把真实的生命周期暴露出来：
// 列出、恢复、彻底删除、清空。
//
// 收藏和观看历史始终不动：它们靠 catalogProjectionFilter 跟着目录行一起隐藏，所以删除期间
// 看不见、恢复后自动回来；只有彻底删除才让这些行永远失去归属。
package handler

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
)

// RecycleListReq 是回收站分页请求。SourceKey 为空表示跨源列出全部。
type RecycleListReq struct {
	SourceKey string `json:"source_key"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
}

// RecycleListResp 是回收站一页加总数。
type RecycleListResp struct {
	Items    []db.RecycleItem `json:"items"`
	Total    int              `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}

// RecycleReq 定位一条回收站条目。回收站跨源，所以源坐标是必填的。
type RecycleReq struct {
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
}

// RecycleResult 是一次彻底删除/清空的结果。
type RecycleResult struct {
	Affected int `json:"affected"`
}

func (req RecycleReq) validate() error {
	if req.SourceKey == "" || req.VodId == "" {
		return apperror.New(apperror.Validation, "参数不完整")
	}
	return nil
}

// ListRecycleBin 返回回收站的一页。
func ListRecycleBin(req RecycleListReq) (*RecycleListResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 50
	}
	items, total, err := db.ListRecycleBin(req.SourceKey, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []db.RecycleItem{}
	}
	return &RecycleListResp{Items: items, Total: total, Page: req.Page, PageSize: req.PageSize}, nil
}

// RestoreVideo 把一条回收站条目放回视频库。
func RestoreVideo(req RecycleReq) error {
	if err := req.validate(); err != nil {
		return err
	}
	return cache.RestoreCatalogVideo(req.SourceKey, req.VodId, "视频已从回收站恢复")
}

// PurgeVideo 彻底删除一条回收站条目。
func PurgeVideo(req RecycleReq) (*RecycleResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	deleted, err := cache.PurgeCatalogVideo(req.SourceKey, req.VodId, "视频已彻底删除")
	if err != nil {
		return nil, err
	}
	return &RecycleResult{Affected: deleted}, nil
}

// ClearRecycleBin 清空回收站（可按源过滤）。
func ClearRecycleBin(req RecycleListReq) (*RecycleResult, error) {
	deleted, err := cache.ClearRecycleBin(req.SourceKey, "回收站已清空")
	if err != nil {
		return nil, err
	}
	return &RecycleResult{Affected: deleted}, nil
}
