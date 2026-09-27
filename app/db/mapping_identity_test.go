package db

import "testing"

func TestIdentityMappedFavoritesAndHistoryKeepSourceCoordinates(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const globalID = 90001
	database := DB()
	if _, err := database.Exec(`DELETE FROM favorites; DELETE FROM watch_history; DELETE FROM global_video WHERE id=?; INSERT INTO global_video(id,vod_name) VALUES (?, 'same title')`, globalID, globalID); err != nil {
		t.Fatal(err)
	}

	if err := AddFavoriteByIdentity(globalID, "source-a", "vod-a"); err != nil {
		t.Fatal(err)
	}
	if err := AddFavoriteByIdentity(globalID, "source-b", "vod-b"); err != nil {
		t.Fatal(err)
	}
	var favorites int
	if err := database.Get(&favorites, `SELECT COUNT(*) FROM favorites WHERE global_id=?`, globalID); err != nil {
		t.Fatal(err)
	}
	if favorites != 1 {
		t.Fatalf("favorites = %d, want one global favorite", favorites)
	}

	if err := SaveWatchHistoryByIdentity(globalID, "source-a", "vod-a", 1, 12.5); err != nil {
		t.Fatal(err)
	}
	if err := SaveWatchHistoryByIdentity(globalID, "source-b", "vod-b", 1, 31.5); err != nil {
		t.Fatal(err)
	}
	position, err := GetWatchHistoryByIdentity(globalID, "source-a", "vod-a", 1)
	if err != nil || position != 12.5 {
		t.Fatalf("source-a position = %v, %v", position, err)
	}
	position, err = GetWatchHistoryByIdentity(globalID, "source-b", "vod-b", 1)
	if err != nil || position != 31.5 {
		t.Fatalf("source-b position = %v, %v", position, err)
	}
	if err := DeleteHistoryItemByIdentity(globalID, "source-a", "vod-a", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := GetWatchHistoryByIdentity(globalID, "source-a", "vod-a", 1); err == nil {
		t.Fatal("source-a history was not deleted")
	}
	position, err = GetWatchHistoryByIdentity(globalID, "source-b", "vod-b", 1)
	if err != nil || position != 31.5 {
		t.Fatalf("source-b history was affected: %v, %v", position, err)
	}
}
