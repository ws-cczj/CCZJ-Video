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

// 收藏与历史列表必须把软删进回收站的目录行一起藏掉，同时不能误伤
// 「只有全局元数据、目录里压根没有这条源记录」的条目。
func TestFavoritesAndHistoryHideSoftDeletedCatalogRows(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	database := DB()
	const (
		sourceKey = "src-soft-delete"
		deletedID = 90011
		ghostID   = 90012
	)
	cleanup := func() {
		_, _ = database.Exec(`DELETE FROM favorites WHERE global_id IN (?, ?)`, deletedID, ghostID)
		_, _ = database.Exec(`DELETE FROM watch_history WHERE global_id IN (?, ?)`, deletedID, ghostID)
		_, _ = database.Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey)
		_, _ = database.Exec(`DELETE FROM global_video WHERE id IN (?, ?)`, deletedID, ghostID)
	}
	cleanup()
	defer cleanup()

	for _, id := range []int64{deletedID, ghostID} {
		name := "软删过滤校验" + string(rune('a'+int(id-90011)))
		// 只有被删的那条有目录行；ghost 走"源坐标压根没被采集进来"的路径，必须仍然可见。
		vodID := "vod-1"
		if id == ghostID {
			vodID = "vod-never-collected"
		}
		if _, err := database.Exec(`INSERT INTO global_video (id, vod_name, name_norm, type_id) VALUES (?, ?, ?, 0)`, id, name, name); err != nil {
			t.Fatal(err)
		}
		if err := AddFavoriteByIdentity(id, sourceKey, vodID); err != nil {
			t.Fatal(err)
		}
		if err := SaveWatchHistoryByIdentity(id, sourceKey, vodID, 1, 10); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := database.Exec(`INSERT INTO source_videos (source_key, source_vod_id, global_id, type_name, lifecycle_state)
		VALUES (?, 'vod-1', ?, '剧情片', 'deleted')`, sourceKey, deletedID); err != nil {
		t.Fatal(err)
	}

	favorites, err := GetFavorites(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	history, err := GetRecentHistory(100)
	if err != nil {
		t.Fatal(err)
	}
	visible := func(inFavorites, inHistory bool, id int64) {
		t.Helper()
		var gotFav, gotHist bool
		for _, f := range favorites {
			if int64(f.GlobalID) == id {
				gotFav = true
			}
		}
		for _, h := range history {
			if int64(h.GlobalID) == id {
				gotHist = true
			}
		}
		if gotFav != inFavorites {
			t.Errorf("favorites visibility for %d = %v, want %v", id, gotFav, inFavorites)
		}
		if gotHist != inHistory {
			t.Errorf("history visibility for %d = %v, want %v", id, gotHist, inHistory)
		}
	}
	visible(false, false, deletedID)
	visible(true, true, ghostID)

	// 目录行复活后必须重新出现在两个列表里，不需要重新收藏。
	if _, err := database.Exec(`UPDATE source_videos SET lifecycle_state='active' WHERE source_key=? AND source_vod_id='vod-1'`, sourceKey); err != nil {
		t.Fatal(err)
	}
	favorites, err = GetFavorites(1, 100)
	if err != nil {
		t.Fatal(err)
	}
	history, err = GetRecentHistory(100)
	if err != nil {
		t.Fatal(err)
	}
	visible(true, true, deletedID)
}
