package db

import (
	"cczjVideo/app/model"
	"strings"
)

// Source IDs are scoped to a source, never global type IDs. Search list APIs
// sometimes omit type_name, so checking the display field alone is insufficient.
func FilterSourceCollectVideos(sourceKey string, videos []*model.Video) ([]*model.Video, error) {
	if len(videos) == 0 {
		return videos, nil
	}
	var types []*GlobalTypeRow
	if err := instance.Select(&types, `SELECT id,type_name,collect_enabled,sort,created_at FROM global_types`); err != nil {
		return nil, err
	}
	denied := map[string]bool{}
	globalNames := map[int64]string{}
	for _, row := range types {
		globalNames[int64(row.Id)] = row.TypeName
		if row.CollectEnabled == 0 {
			denied[normalizeTypeName(row.TypeName)] = true
		}
	}
	sourceTypes, err := ExportSourceTypes(sourceKey)
	if err != nil {
		return nil, err
	}
	byID := map[string]SourceTypeExport{}
	for _, row := range sourceTypes {
		byID[row.SourceTypeID] = row
	}
	out := make([]*model.Video, 0, len(videos))
	for _, video := range videos {
		if video == nil || denied[normalizeTypeName(video.TypeName)] {
			continue
		}
		mapped, ok := byID[strings.TrimSpace(video.TypeId.String())]
		if ok {
			if denied[normalizeTypeName(mapped.TypeName)] || denied[normalizeTypeName(globalNames[mapped.GlobalTypeID])] {
				continue
			}
			if strings.TrimSpace(video.TypeName) == "" {
				video.TypeName = mapped.TypeName
			}
		}
		// An unclassified remote hit cannot be checked against the user's bans.
		// It must not become a visible card or a durable catalog item.
		if strings.TrimSpace(video.TypeName) == "" {
			continue
		}
		out = append(out, video)
	}
	return out, nil
}
