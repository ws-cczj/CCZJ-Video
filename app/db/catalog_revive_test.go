package db

import (
	"cczjVideo/app/model"
	"testing"
)

func lifecycleStateOf(t *testing.T, sourceKey, vodID string) string {
	t.Helper()
	var state string
	if err := DB().Get(&state, `SELECT lifecycle_state FROM source_videos WHERE source_key=? AND source_vod_id=?`, sourceKey, vodID); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAutomaticUpsertHonoursReviveDeletedSetting(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	defer SetSetting(SettingReviveDeletedCatalogItems, "")

	const (
		sourceKey = "revive_test"
		typeName  = "Revive test type"
	)
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := DB().Exec(`DELETE FROM global_types WHERE type_name=?`, typeName); err != nil {
		t.Fatal(err)
	}
	videos := func(remarks string) []*model.Video {
		return []*model.Video{{VodId: "revive-1", TypeId: "revive", TypeName: typeName, VodName: "Revive title", VodRemarks: remarks}}
	}

	// Default (setting unset) keeps the historical behaviour: collection revives deleted items.
	if err := UpsertCatalogItems(sourceKey, videos("first")); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCatalogVideo(sourceKey, "revive-1"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(sourceKey, videos("second")); err != nil {
		t.Fatal(err)
	}
	if got := lifecycleStateOf(t, sourceKey, "revive-1"); got != "active" {
		t.Fatalf("default lifecycle_state = %q; want active", got)
	}
	if err := DeleteCatalogVideo(sourceKey, "revive-1"); err != nil {
		t.Fatal(err)
	}

	if err := SetSetting(SettingReviveDeletedCatalogItems, "false"); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(sourceKey, videos("third")); err != nil {
		t.Fatal(err)
	}
	if got := lifecycleStateOf(t, sourceKey, "revive-1"); got != "deleted" {
		t.Fatalf("revive disabled lifecycle_state = %q; want deleted", got)
	}
	var remarks string
	if err := DB().Get(&remarks, `SELECT vod_remarks FROM source_videos WHERE source_key=? AND source_vod_id=?`, sourceKey, "revive-1"); err != nil {
		t.Fatal(err)
	}
	if remarks != "third" {
		t.Fatalf("revive disabled vod_remarks = %q; want the fresh metadata to still land", remarks)
	}

	// Explicit imports always restore, setting or not.
	if err := UpsertCatalogItemsWithRevival(sourceKey, videos("fourth")); err != nil {
		t.Fatal(err)
	}
	if got := lifecycleStateOf(t, sourceKey, "revive-1"); got != "active" {
		t.Fatalf("imported lifecycle_state = %q; want active", got)
	}
}
