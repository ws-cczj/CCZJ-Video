// Catalog deletion hides preserved user records. Restore reveals them again;
// purge removes the projection and keeps a retirement marker until reimport.
package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"context"
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
func (s *Service) ListRecycleBin(ctx context.Context, req RecycleListReq) (*RecycleListResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 || req.PageSize > 200 {
		req.PageSize = 50
	}
	items, total, err := s.store.ListRecycleBin(ctx, req.SourceKey, req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []db.RecycleItem{}
	}
	return &RecycleListResp{Items: items, Total: total, Page: req.Page, PageSize: req.PageSize}, nil
}

// RestoreVideo 把一条回收站条目放回视频库。
func (s *Service) RestoreVideo(ctx context.Context, req RecycleReq) error {
	if err := req.validate(); err != nil {
		return err
	}
	change, err := s.store.RestoreCatalog(ctx, req.SourceKey, req.VodId)
	if err != nil {
		return err
	}
	cache.PublishCatalogChange(change, "视频已从回收站恢复")
	return nil
}

// PurgeVideo 彻底删除一条回收站条目。
func (s *Service) PurgeVideo(ctx context.Context, req RecycleReq) (*RecycleResult, error) {
	if err := req.validate(); err != nil {
		return nil, err
	}
	change, err := s.store.PurgeCatalog(ctx, req.SourceKey, req.VodId)
	if err != nil {
		return nil, err
	}
	cache.PublishCatalogChange(change, "视频已彻底删除")
	return &RecycleResult{Affected: change.Affected}, nil
}

// ClearRecycleBin 清空回收站（可按源过滤）。
func (s *Service) ClearRecycleBin(ctx context.Context, req RecycleListReq) (*RecycleResult, error) {
	change, err := s.store.ClearCatalogRecycleBin(ctx, req.SourceKey)
	if err != nil {
		return nil, err
	}
	cache.PublishCatalogChange(change, "回收站已清空")
	return &RecycleResult{Affected: change.Affected}, nil
}
