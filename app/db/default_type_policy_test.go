package db

import (
	"testing"

	"cczjVideo/app/model"
)

func TestNewTypeDefaultsAndDisabledOrdering(t *testing.T) {
	database, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	previous := instance
	instance = database
	t.Cleanup(func() { instance = previous; database.Close() })
	for _, name := range []string{"福利", "福利片", "伦理片", "倫理片"} {
		if IsTypeCollectEnabled(name) {
			t.Errorf("unknown type %s must default to disabled", name)
		}
	}
	selection, err := SelectSourceCollectVideos("unseen", []*model.Video{{VodId: "blocked", VodName: "blocked", TypeName: "倫理片"}})
	if err != nil || selection.Disabled != 1 {
		t.Fatalf("unknown restricted type entered search: %+v %v", selection, err)
	}
	if err := UpsertGlobalType("福利"); err != nil {
		t.Fatal(err)
	}
	if _, err := GetOrCreateGlobalTypeId("伦理片"); err != nil {
		t.Fatal(err)
	}
	resolver := &catalogIdentityResolver{typesByExact: map[string]int64{}}
	if _, err := resolver.resolveTypeID(database, "倫理片"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Z catalogue", "A catalogue"} {
		if err := UpsertGlobalType(name); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := GetAllGlobalTypes()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 || rows[0].TypeName != "A catalogue" || rows[1].TypeName != "Z catalogue" {
		t.Fatalf("unexpected order: %+v", rows)
	}
	for _, row := range rows[2:] {
		if row.CollectEnabled != 0 {
			t.Errorf("%s enabled by default", row.TypeName)
		}
	}
	if err := SetGlobalTypeCollectEnabled("伦理片", true); err != nil {
		t.Fatal(err)
	}
	if !IsTypeCollectEnabled("倫理片") {
		t.Fatal("explicit user choice did not enable the alias family")
	}
}

func TestUpgradeDefaultTypePolicyRunsOnce(t *testing.T) {
	dir := t.TempDir()
	database, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`INSERT INTO global_types(type_name,collect_enabled) VALUES ('福利',1),('伦理片',1),('倫理片',1),('剧情片',1); PRAGMA user_version=20`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := database.Get(&count, `SELECT COUNT(*) FROM global_types WHERE collect_enabled=0`); err != nil || count != 3 {
		t.Fatal(count, err)
	}
	if _, err := database.Exec(`UPDATE global_types SET collect_enabled=1 WHERE type_name='福利'`); err != nil {
		t.Fatal(err)
	}
	database.Close()
	database, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	var enabled int
	if err := database.Get(&enabled, `SELECT collect_enabled FROM global_types WHERE type_name='福利'`); err != nil || enabled != 1 {
		t.Fatal("later explicit choice overwritten", enabled, err)
	}
}
