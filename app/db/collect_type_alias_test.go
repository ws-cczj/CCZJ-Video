package db

import (
	"cczjVideo/app/model"
	"testing"
)

func setupTypeAliasPolicy(t *testing.T) {
	t.Helper()
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous := instance
	instance = database
	t.Cleanup(func() { instance = previous; database.Close() })
	_, err = database.Exec(`INSERT INTO global_types(id,type_name,collect_enabled) VALUES
	 (901,'福利',0),(902,'伦理片',0),(903,'倫理片',1),(904,'福利片',1),(905,'剧情片',1)`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDisabledTypeAliasesCannotEnterCatalog(t *testing.T) {
	setupTypeAliasPolicy(t)
	rows := []*model.Video{
		{VodId: "1", TypeName: "倫理片", VodName: "blocked traditional"},
		{VodId: "2", TypeName: "福利片", VodName: "blocked alias"},
		{VodId: "3", TypeName: "伦 理 片", VodName: "blocked spacing"},
		{VodId: "4", TypeName: "剧情片", VodName: "allowed"},
	}
	if got, err := FilterSourceCollectVideos("alias_policy", rows); err != nil || len(got) != 1 || got[0].VodName != "allowed" {
		t.Errorf("remote search filter retained disabled aliases: %+v", got)
	}
	if err := UpsertCatalogItemsBatch(NewCatalogBatch(), "alias_policy", rows); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := DB().Get(&count, `SELECT COUNT(*) FROM source_videos WHERE source_key='alias_policy'`); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("collection wrote %d rows; want only allowed item", count)
	}
}

func TestDisabledTypeAliasesHideLegacyRowsRegardlessOfMappedID(t *testing.T) {
	setupTypeAliasPolicy(t)
	_, err := DB().Exec(`INSERT INTO source_videos(source_key,source_vod_id,global_id,type_name,global_type_id,vod_name,lifecycle_state) VALUES
	 ('alias_legacy','1',0,'倫理片',903,'legacy blocked','active'),
	 ('alias_legacy','2',0,'福利片',904,'legacy blocked','active'),
	 ('alias_legacy','3',0,'伦理片',905,'legacy blocked wrong mapping','active'),
	 ('alias_legacy','4',0,'剧情片',905,'legacy allowed','active')`)
	if err != nil {
		t.Fatal(err)
	}
	page, err := GetCatalogVideoPage("alias_legacy", FilterParams{PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Videos) != 1 || page.Videos[0].VodName != "legacy allowed" {
		t.Errorf("browse leaked disabled legacy rows: %+v", page)
	}
	search, err := SearchCatalogByTitle("alias_legacy", "legacy", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 1 {
		t.Errorf("local search returned %d items, want 1", len(search))
	}
	for _, id := range []string{"1", "2", "3"} {
		if _, err := GetCatalogItem("alias_legacy", id); err == nil {
			t.Errorf("disabled item %s remains addressable", id)
		}
	}
}

func TestTypePolicyToggleAndDisplayKeepAliasesTogether(t *testing.T) {
	setupTypeAliasPolicy(t)
	rows, err := GetAllGlobalTypes()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.TypeName != "剧情片" && row.CollectEnabled != 0 {
			t.Errorf("enabled alias displayed despite explicit denial: %+v", row)
		}
	}
	if err := SetGlobalTypeCollectEnabled("倫理片", true); err != nil {
		t.Fatal(err)
	}
	if !IsTypeCollectEnabled("伦理片") {
		t.Fatal("re-enabling an alias left its family blocked")
	}
	if err := SetGlobalTypeCollectEnabled("伦理片", false); err != nil {
		t.Fatal(err)
	}
	var enabled int
	if err := DB().Get(&enabled, `SELECT collect_enabled FROM global_types WHERE type_name='倫理片'`); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatal("disabling did not update the existing traditional alias")
	}
}
