package db

import (
	"cczjVideo/app/model"
	"fmt"
	"testing"
)

func TestCatalogCursorPageWalksWithoutDuplicates(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "cursor_test"
	if _, err := DB().Exec(`DELETE FROM source_types WHERE source_key=?; DELETE FROM source_videos WHERE source_key=?`, sourceKey, sourceKey); err != nil {
		t.Fatal(err)
	}
	videos := make([]*model.Video, 0, 5)
	for i := 1; i <= 5; i++ {
		videos = append(videos, &model.Video{
			VodId:    model.FlexibleString(fmt.Sprintf("cursor-%d", i)),
			TypeId:   "cursor-type",
			TypeName: "Cursor test",
			VodName:  fmt.Sprintf("Cursor title %d", i),
			VodTime:  fmt.Sprintf("2026-01-0%d", i),
		})
	}
	if err := UpsertCatalogItems(sourceKey, videos); err != nil {
		t.Fatal(err)
	}

	seen := make(map[string]bool)
	filter := FilterParams{PageSize: 2}
	for pageNo := 0; pageNo < 3; pageNo++ {
		page, err := GetCatalogVideoPage(sourceKey, filter)
		if err != nil {
			t.Fatal(err)
		}
		if pageNo == 0 && page.Total != 5 {
			t.Fatalf("total=%d, want 5", page.Total)
		}
		for _, video := range page.Videos {
			id := video.VodId.String()
			if seen[id] {
				t.Fatalf("duplicate cursor result %q", id)
			}
			seen[id] = true
		}
		filter.Cursor = page.NextCursor
		if pageNo < 2 && filter.Cursor == "" {
			t.Fatalf("page %d unexpectedly had no next cursor", pageNo)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("seen=%d, want 5", len(seen))
	}
}
