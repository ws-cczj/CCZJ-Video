package db

import (
	"cczjVideo/app/model"
	"context"
)

// SourceInspectionCounts describes only rows belonging or mapped to one source.
type SourceInspectionCounts struct {
	Catalog int `db:"catalog"`
	Types   int `db:"types"`
}

// ReadSourceInspectionCounts reads catalog and type counts in one SQL snapshot.
func ReadSourceInspectionCounts(ctx context.Context, key string) (counts SourceInspectionCounts, err error) {
	err = instance.GetContext(ctx, &counts, `SELECT (SELECT COUNT(*) FROM source_videos WHERE source_key=?) catalog,
	 (SELECT COUNT(DISTINCT gt.id) FROM global_types gt JOIN source_types st ON st.global_type_id=gt.id WHERE st.source_key=?) types`, key, key)
	return counts, err
}

func GetSourceStats() ([]model.SourceStat, error) { return GetSourceStatsContext(context.Background()) }
func GetSourceStatsContext(ctx context.Context) ([]model.SourceStat, error) {
	rows := []model.SourceStat{}
	err := instance.SelectContext(ctx, &rows, `SELECT s.source_key,s.name,
	 CASE WHEN s.kind='video' THEN (SELECT COUNT(*) FROM source_videos sv WHERE sv.source_key=s.source_key AND sv.lifecycle_state='active')
	 ELSE (SELECT COUNT(*) FROM library_items li WHERE li.source_key=s.source_key AND li.kind=s.kind AND li.lifecycle_state='active') END video_count,
	 0 episode_count FROM sources s ORDER BY s.id`)
	return rows, err
}
