package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"context"
	"strings"
)

// Import stores catalog projections of explicitly selected video search hits.
// It revives retired rows, copies caller-owned data and publishes after commit.
func (s *Service) Import(ctx context.Context, sourceKey string, videos []*model.Video) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := model.ValidateSourceKey(sourceKey); err != nil {
		return 0, apperror.Wrap(apperror.Validation, err, "源标识无效")
	}
	if len(videos) == 0 {
		return 0, nil
	}
	source, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return 0, apperror.Wrap(apperror.Storage, err, "获取源失败")
	}
	if source.GetKind() != model.SourceKindVideo {
		return 0, apperror.New(apperror.Unsupported, "视频入库只支持视频采集源")
	}
	filtered, err := db.FilterSourceCollectVideos(sourceKey, videos)
	if err != nil {
		return 0, apperror.Wrap(apperror.Storage, err, "读取禁用类型失败")
	}
	toSave := make([]*model.Video, 0, len(filtered))
	byID := map[string]int{}
	for _, value := range filtered {
		if value == nil || strings.TrimSpace(value.VodName) == "" || strings.TrimSpace(value.VodId.String()) == "" {
			continue
		}
		row := *value
		row.VodId = model.FlexibleString(strings.TrimSpace(row.VodId.String()))
		row.VodContent, row.VodActor, row.VodDirector = "", "", ""
		row.VodPlayUrl, row.VodDownUrl, row.VodPlayFrom = "", "", ""
		if index, ok := byID[row.VodId.String()]; ok {
			toSave[index] = &row
		} else {
			byID[row.VodId.String()] = len(toSave)
			toSave = append(toSave, &row)
		}
	}
	if len(toSave) == 0 {
		return 0, nil
	}
	if err := db.UpsertCatalogItemsWithRevivalContext(ctx, sourceKey, toSave); err != nil {
		return 0, apperror.Wrap(apperror.Storage, err, "写入视频目录失败")
	}
	cache.InvalidateSource(sourceKey, "视频已显式入库")
	operation.Stage(ctx, "video_catalog_imported", applog.Fields{"source_key": sourceKey, "count": len(toSave)})
	return len(toSave), nil
}
