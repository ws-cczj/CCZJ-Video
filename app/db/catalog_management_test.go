package db

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"cczjVideo/app/model"
	"github.com/jmoiron/sqlx"
)

func TestPurgingCatalogDoesNotResurfacePreservedUserRecords(t *testing.T) {
	database := useMigratedTestDB(t)
	for _, action := range []string{"purge", "clear_recycle", "truncate_source"} {
		if _, err := database.Exec(`DELETE FROM favorites; DELETE FROM watch_history; DELETE FROM source_videos; DELETE FROM global_video`); err != nil {
			t.Fatal(err)
		}
		if err := UpsertCatalogItems("purged", []*model.Video{{VodId: "one", VodName: "Purged title", TypeId: "1", TypeName: "Purge test category"}}); err != nil {
			t.Fatal(err)
		}
		id, err := GetGlobalIdForVideo("purged", "one")
		if err != nil {
			t.Fatal(err)
		}
		if err := AddFavoriteByIdentity(id, "purged", "one"); err != nil {
			t.Fatal(err)
		}
		if err := SaveWatchHistoryByIdentity(id, "purged", "one", 1, 30); err != nil {
			t.Fatal(err)
		}
		if err := DeleteCatalogVideo("purged", "one"); err != nil {
			t.Fatal(err)
		}
		if action == "clear_recycle" {
			_, err = ClearRecycleBin("purged")
		} else if action == "truncate_source" {
			err = TruncateSourceContext(context.Background(), "purged")
		} else {
			_, err = PurgeCatalogVideo("purged", "one")
		}
		if err != nil {
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
		if len(favorites) != 0 || len(history) != 0 {
			t.Errorf("%s resurfaced hidden records: favorites=%d history=%d", action, len(favorites), len(history))
		}
		var remaining int
		if err := database.Get(&remaining, `SELECT (SELECT COUNT(*) FROM favorites)+(SELECT COUNT(*) FROM watch_history)`); err != nil || remaining != 2 {
			t.Fatal("purge deleted user records", remaining, err)
		}
	}
}

func TestCatalogBatchReloadsAfterManualMerge(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`INSERT INTO global_types(id,type_name,collect_enabled) VALUES(891,'Batch merge A',1),(892,'Batch merge B',1);
	 INSERT INTO global_video(id,vod_name,name_norm,type_id,year,douban_id,pic) VALUES(891,'Batch title','batchtitle',891,'2024','one',''),(892,'Batch title','batchtitle',892,'2024','one','rich')`); err != nil {
		t.Fatal(err)
	}
	batch := NewCatalogBatch()
	row := &model.Video{VodId: "first", VodName: "Batch title", TypeId: "a", TypeName: "Batch merge A", VodYear: "2024"}
	if err := UpsertCatalogItemsBatch(batch, "batch_after_merge", []*model.Video{row}); err != nil {
		t.Fatal(err)
	}
	var before int64
	if err := database.Get(&before, `SELECT global_id FROM source_videos WHERE source_key='batch_after_merge'`); err != nil || before != 891 {
		t.Fatal("fixture did not prime victim identity", before, err)
	}
	if _, _, err := MergeGlobalVideoIdentities([]int64{891, 892}); err != nil {
		t.Fatal(err)
	}
	copy := *row
	copy.VodId = "second"
	if err := UpsertCatalogItemsBatch(batch, "batch_after_merge", []*model.Video{&copy}); err != nil {
		t.Fatal(err)
	}
	var orphan int
	if err := database.Get(&orphan, `SELECT COUNT(*) FROM source_videos sv LEFT JOIN global_video g ON g.id=sv.global_id WHERE g.id IS NULL`); err != nil || orphan != 0 {
		t.Fatal("batch reused a merged-away identity", orphan, err)
	}
}

func TestCatalogWriteAdmissionCanBeCancelled(t *testing.T) {
	release, err := ownCatalogIdentityWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ownCatalogIdentityWrite(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestFailedIdentityMergeRollsBackReferencesAndRevision(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`INSERT INTO global_video(id,vod_name,name_norm,type_id,douban_id,pic) VALUES(861,'Merge rollback','mergerollback',1,'same',''),(862,'Merge rollback','mergerollback',2,'same','rich');
	 INSERT INTO source_videos(source_key,source_vod_id,global_id,vod_name) VALUES('merge_rollback','one',861,'Merge rollback');
	 INSERT INTO favorites(global_id,source_key,vod_id) VALUES(861,'merge_rollback','one');
	 INSERT INTO watch_history(global_id,source_key,vod_id,ep_num,position) VALUES(861,'merge_rollback','one',1,15);
	 CREATE TRIGGER reject_identity_delete BEFORE DELETE ON global_video WHEN OLD.id=861 BEGIN SELECT RAISE(ABORT,'reject merge');END`); err != nil {
		t.Fatal(err)
	}
	revision := catalogIdentityRevision.Load()
	change, err := NewMediaStore(func() *sqlx.DB { return database }).MergeIdentities(context.Background(), []int64{861, 862})
	if err == nil || change.Merged != 0 || catalogIdentityRevision.Load() != revision {
		t.Fatal("failed merge published success", change, err)
	}
	for _, table := range []string{"source_videos", "favorites", "watch_history"} {
		var id int64
		if err := database.Get(&id, `SELECT global_id FROM `+table+` WHERE source_key='merge_rollback'`); err != nil || id != 861 {
			t.Fatal("merge partially repointed references", table, id, err)
		}
	}
}

func TestPurgeFailureRollsBackMarkerAndRow(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`INSERT INTO global_video(id,vod_name,name_norm,type_id) VALUES(851,'Rollback','rollback',0);
	 INSERT INTO source_videos(source_key,source_vod_id,global_id,vod_name,lifecycle_state) VALUES('rollback','one',851,'Rollback','deleted');
	 CREATE TRIGGER reject_catalog_purge BEFORE DELETE ON source_videos BEGIN SELECT RAISE(ABORT,'reject purge'); END`); err != nil {
		t.Fatal(err)
	}
	for _, clear := range []bool{false, true} {
		store := NewMediaStore(func() *sqlx.DB { return database })
		var change CatalogChange
		var err error
		if clear {
			change, err = store.ClearCatalogRecycleBin(context.Background(), "rollback")
		} else {
			change, err = store.PurgeCatalog(context.Background(), "rollback", "one")
		}
		if err == nil || change.Affected != 0 {
			t.Fatal("failed transaction reported success", change, err)
		}
		var markers, rows int
		if err := database.Get(&markers, `SELECT COUNT(*) FROM catalog_retirements`); err != nil {
			t.Fatal(err)
		}
		if err := database.Get(&rows, `SELECT COUNT(*) FROM source_videos WHERE source_key='rollback'`); err != nil {
			t.Fatal(err)
		}
		if markers != 0 || rows != 1 {
			t.Fatal("rollback left a partial catalog retirement", markers, rows)
		}
	}
}

func TestCatalogRetirementUpgradeBacksUpV19AndPreservesLegacyRecords(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO global_video(id,vod_name,name_norm,type_id) VALUES(871,'Legacy','legacy',0);
	 INSERT INTO favorites(global_id,source_key,vod_id) VALUES(871,'legacy','one');
	 INSERT INTO watch_history(global_id,source_key,vod_id,ep_num,position) VALUES(871,'legacy','one',1,12);
	 DROP TABLE catalog_retirements;PRAGMA user_version=19`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store := NewMediaStore(func() *sqlx.DB { return database })
	items, err := store.GetFavorites(context.Background(), 1, 10)
	if err != nil || len(items) != 1 {
		t.Fatal("legacy metadata-only favorite was hidden or lost", items, err)
	}
	history, err := store.GetRecentHistory(context.Background(), 10)
	if err != nil || len(history) != 1 || history[0].Position != 12 {
		t.Fatal(history, err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "schema-backups", "*.db"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, file := range files {
		backup, err := sqlx.Open("sqlite", file)
		if err != nil {
			t.Fatal(err)
		}
		var version int
		err = backup.Get(&version, `PRAGMA user_version`)
		if err == nil && version == 19 {
			exists, err := tableExistsIn(backup, "catalog_retirements")
			if err != nil || exists {
				t.Fatal("backup was taken after migration", exists, err)
			}
			found = true
		}
		_ = backup.Close()
	}
	if !found {
		t.Fatal("missing v19 migration backup")
	}
}

func TestManualIdentityMergeRejectsUnsupportedIdentityEvidence(t *testing.T) {
	for _, tc := range []struct{ name, a, b, yearA, yearB, doubanA, doubanB string }{
		{"unrelated with one known ID", "Unrelated A", "Unrelated B", "2020", "2020", "known", ""},
		{"remakes with different years", "Same title", "Same title", "1975", "2023", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			database := useMigratedTestDB(t)
			if _, err := database.Exec(`INSERT INTO global_video(id,vod_name,name_norm,type_id,year,douban_id) VALUES(801,?,?,1,?,?),(802,?,?,2,?,?)`, tc.a, normalizeTitle(tc.a), tc.yearA, tc.doubanA, tc.b, normalizeTitle(tc.b), tc.yearB, tc.doubanB); err != nil {
				t.Fatal(err)
			}
			if _, _, err := MergeGlobalVideoIdentities([]int64{801, 802}); err == nil {
				t.Fatal("merged identities without matching evidence")
			}
		})
	}
}

func TestMergedHistoryKeepsCoordinatesAndTimeOfRetainedProgress(t *testing.T) {
	database := useMigratedTestDB(t)
	if _, err := database.Exec(`
	 INSERT INTO global_video(id,vod_name,name_norm,type_id,douban_id) VALUES(811,'One','one',1,'same'),(812,'One','one',2,'same');
	 INSERT INTO watch_history(global_id,source_key,vod_id,ep_num,position,updated_at) VALUES
	 (811,'same_source','winner',1,80,'2020-01-02 00:00:00'),(812,'same_source','loser',1,20,'2020-01-01 00:00:00');
	`); err != nil {
		t.Fatal(err)
	}
	// The richer surviving identity differs from the history row with more progress.
	if _, err := database.Exec(`UPDATE global_video SET pic='rich' WHERE id=812`); err != nil {
		t.Fatal(err)
	}
	keep, _, err := MergeGlobalVideoIdentities([]int64{811, 812})
	if err != nil {
		t.Fatal(err)
	}
	var vod, time string
	var position float64
	if err := database.QueryRow(`SELECT vod_id,position,strftime('%Y-%m-%d %H:%M:%S',updated_at) FROM watch_history WHERE global_id=?`, keep).Scan(&vod, &position, &time); err != nil {
		t.Fatal(err)
	}
	if vod != "winner" || position != 80 || time != "2020-01-02 00:00:00" {
		t.Fatalf("progress lost its source coordinate or watch time: %s %v %s", vod, position, time)
	}
}
