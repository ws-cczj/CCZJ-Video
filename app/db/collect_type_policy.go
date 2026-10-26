package db

import (
	"cczjVideo/app/model"
	"context"
	"github.com/jmoiron/sqlx"
	"strings"
)

// Source IDs are scoped to a source, never global type IDs. Search list APIs
// sometimes omit type_name, so checking the display field alone is insufficient.
func FilterSourceCollectVideos(sourceKey string, videos []*model.Video) ([]*model.Video, error) {
	selection, err := SelectSourceCollectVideos(sourceKey, videos)
	return selection.Videos, err
}

type SourceVideoSelection struct {
	Videos       []*model.Video
	Disabled     int
	Unclassified int
}

// SelectSourceCollectVideos retains rejection counts for search presentation.
// Collection callers only need the accepted rows through FilterSourceCollectVideos.
func SelectSourceCollectVideos(sourceKey string, videos []*model.Video) (SourceVideoSelection, error) {
	return selectSourceCollectVideos(context.Background(), instance, sourceKey, videos)
}
func selectSourceCollectVideos(ctx context.Context, executor sqlx.ExtContext, sourceKey string, videos []*model.Video) (SourceVideoSelection, error) {
	selection := SourceVideoSelection{Videos: make([]*model.Video, 0, len(videos))}
	if len(videos) == 0 {
		return selection, nil
	}
	var types []*GlobalTypeRow
	if err := sqlx.SelectContext(ctx, executor, &types, `SELECT id,type_name,collect_enabled,sort,created_at FROM global_types`); err != nil {
		return selection, err
	}
	denied := map[string]bool{}
	known := map[string]bool{}
	globalNames := map[int64]string{}
	for _, row := range types {
		known[normalizeTypeName(row.TypeName)] = true
		globalNames[int64(row.Id)] = row.TypeName
		if row.CollectEnabled == 0 {
			denied[normalizeTypeName(row.TypeName)] = true
		}
	}
	for _, key := range defaultBlockedTypeKeys {
		if !known[key] {
			denied[key] = true
		}
	}
	sourceTypes := []SourceTypeExport{}
	err := sqlx.SelectContext(ctx, executor, &sourceTypes, `SELECT source_type_id,COALESCE(global_type_id,0) global_type_id,type_name FROM source_types WHERE source_key=?`, sourceKey)
	if err != nil {
		return selection, err
	}
	byID := map[string]SourceTypeExport{}
	for _, row := range sourceTypes {
		byID[row.SourceTypeID] = row
	}
	for _, video := range videos {
		if video == nil {
			selection.Unclassified++
			continue
		}
		if denied[normalizeTypeName(video.TypeName)] {
			selection.Disabled++
			continue
		}
		mapped, ok := byID[strings.TrimSpace(video.TypeId.String())]
		if ok {
			if denied[normalizeTypeName(mapped.TypeName)] || denied[normalizeTypeName(globalNames[mapped.GlobalTypeID])] {
				selection.Disabled++
				continue
			}
			if strings.TrimSpace(video.TypeName) == "" {
				copy := *video
				copy.TypeName = mapped.TypeName
				video = &copy
			}
		}
		// An unclassified remote hit cannot be checked against the user's bans.
		// It must not become a visible card or a durable catalog item.
		if strings.TrimSpace(video.TypeName) == "" {
			selection.Unclassified++
			continue
		}
		selection.Videos = append(selection.Videos, video)
	}
	return selection, nil
}
