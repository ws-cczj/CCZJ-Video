package db

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"cczjVideo/app/model"
)

func TestShortDramaRecommendationsRespectCatalogBoundaries(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const source = "short_drama_recommend_test"
	const category = "推荐测试短剧"
	cleanup := func() {
		DB().Exec(`DELETE FROM source_videos WHERE source_key IN (?,?)`, source, source+"_other")
		DB().Exec(`DELETE FROM source_types WHERE source_key IN (?,?)`, source, source+"_other")
		DB().Exec(`DELETE FROM global_types WHERE type_name IN (?,?)`, category, "繁体推荐测试短劇")
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := DB().Exec(`INSERT INTO global_types(type_name, collect_enabled) VALUES (?,1),(?,1)`, category, "繁体推荐测试短劇"); err != nil {
		t.Fatal(err)
	}
	var videos []*model.Video
	for i := 0; i < 8; i++ {
		videos = append(videos, &model.Video{VodId: model.FlexibleString(fmt.Sprint(i)), VodName: fmt.Sprintf("推荐测试 %d", i), TypeId: "short", TypeName: category})
	}
	videos = append(videos,
		&model.Video{VodId: "traditional", VodName: "推荐测试繁体", TypeId: "traditional", TypeName: "繁体推荐测试短劇"},
		&model.Video{VodId: "film", VodName: "名字带短剧的电影", TypeId: "film", TypeName: "电影"},
	)
	if err := UpsertCatalogItems(source, videos); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(source+"_other", []*model.Video{{VodId: "foreign", VodName: "推荐测试其他源", TypeId: "short", TypeName: category}}); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`UPDATE source_videos SET lifecycle_state='recycled' WHERE source_key=? AND source_vod_id='0'`, source); err != nil {
		t.Fatal(err)
	}
	t.Run("catalog pagination filters before counting and cursoring", func(t *testing.T) {
		filter := FilterParams{ShortDrama: true, PageSize: 3}
		seen := map[string]bool{}
		for {
			page, err := GetCatalogVideoPageContext(context.Background(), source, filter)
			if err != nil {
				t.Fatal(err)
			}
			if page.Total != 8 {
				t.Fatalf("short drama total = %d, want 8", page.Total)
			}
			for _, row := range page.Videos {
				id := row.VodId.String()
				if seen[id] || id == "film" || id == "foreign" || id == "0" {
					t.Fatalf("invalid catalog row: %+v", row)
				}
				seen[id] = true
			}
			if page.NextCursor == "" {
				break
			}
			filter.Cursor = page.NextCursor
		}
		if len(seen) != 8 {
			t.Fatalf("catalog rows = %d, want 8", len(seen))
		}
		filter.Cursor = ""
		filter.Keyword = "繁体"
		page, err := GetCatalogVideoPageContext(context.Background(), source, filter)
		if err != nil || page.Total != 1 || page.Videos[0].VodId.String() != "traditional" {
			t.Fatalf("filtered catalog = %+v, %v", page, err)
		}
	})
	for n := 0; n < 12; n++ {
		rows, err := GetShortDramaRecommendContext(context.Background(), source, 5, []string{"1"})
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != 5 {
			t.Fatalf("batch size = %d, want 5", len(rows))
		}
		seen := map[string]bool{}
		for _, row := range rows {
			id := row.VodId.String()
			if seen[id] || id == "0" || id == "1" || id == "film" || id == "foreign" {
				t.Fatalf("invalid recommendation: %+v", row)
			}
			seen[id] = true
		}
	}
	if err := SetGlobalTypeCollectEnabled(category, false); err != nil {
		t.Fatal(err)
	}
	rows, err := GetShortDramaRecommendContext(context.Background(), source, 5, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].VodId.String() != "traditional" {
		t.Fatalf("disabled types leaked: %+v", rows)
	}
	rows, err = GetShortDramaRecommendContext(context.Background(), source, 5, []string{"traditional"})
	if err != nil || len(rows) != 0 {
		t.Fatalf("empty feed = %v, %v", rows, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := GetShortDramaRecommendContext(ctx, source, 5, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel error = %v", err)
	}
}
