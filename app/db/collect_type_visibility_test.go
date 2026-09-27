package db

import (
	"cczjVideo/app/model"
	"testing"
)

func TestDisabledCollectTypesAreHiddenEverywhere(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}

	const (
		sourceKey    = "visibility_test"
		disabledType = "Disabled visibility type"
		enabledType  = "Enabled visibility type"
	)
	if _, err := DB().Exec(`DELETE FROM source_types WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM global_types WHERE type_name IN (?, ?)`, disabledType, enabledType); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`INSERT INTO global_types(type_name, collect_enabled) VALUES (?, 1), (?, 1)`, disabledType, enabledType); err != nil {
		t.Fatal(err)
	}

	if err := UpsertCatalogItems(sourceKey, []*model.Video{
		{VodId: "disabled-1", TypeId: "disabled", TypeName: disabledType, VodName: "Hidden title", VodYear: "1999", VodArea: "Hidden area", VodTime: "2026-01-01"},
		{VodId: "enabled-1", TypeId: "enabled", TypeName: enabledType, VodName: "Visible title", VodYear: "2026", VodArea: "Visible area", VodTime: "2026-01-02"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := SetGlobalTypeCollectEnabled(disabledType, false); err != nil {
		t.Fatal(err)
	}

	page, err := GetCatalogVideoPage(sourceKey, FilterParams{PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Videos) != 1 || page.Videos[0].TypeName != enabledType {
		t.Fatalf("catalog page = total %d, videos %v; want only enabled type", page.Total, page.Videos)
	}

	types, err := GetTypes(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(types) != 1 || types[0].Name != enabledType {
		t.Fatalf("types = %v; want only enabled type", types)
	}

	years, areas, err := GetCatalogYearsAndAreas(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	if len(years) != 1 || years[0] != "2026" || len(areas) != 1 || areas[0] != "Visible area" {
		t.Fatalf("years=%v areas=%v; want only enabled values", years, areas)
	}

	recommendations, err := GetCatalogRecommend(sourceKey, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, video := range recommendations {
		if video.TypeName == disabledType {
			t.Fatalf("recommendations contain disabled type: %#v", video)
		}
	}
	if _, err := GetCatalogItem(sourceKey, "disabled-1"); err == nil {
		t.Fatal("disabled catalog item is still addressable")
	}

	if err := UpsertCatalogItems(sourceKey, []*model.Video{{
		VodId: "disabled-2", TypeId: "disabled", TypeName: disabledType, VodName: "Should not be written",
	}}); err != nil {
		t.Fatal(err)
	}
	var disabledCount int
	if err := DB().Get(&disabledCount, `SELECT COUNT(*) FROM source_videos WHERE source_key=? AND type_name=?`, sourceKey, disabledType); err != nil {
		t.Fatal(err)
	}
	if disabledCount != 1 {
		t.Fatalf("disabled catalog rows = %d; want original stale row only", disabledCount)
	}
}
