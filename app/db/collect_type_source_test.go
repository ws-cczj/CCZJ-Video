package db

import (
	"cczjVideo/app/model"
	"testing"
)

func TestSourceCategoryCannotBeBypassedWithMissingOrConflictingTypeName(t *testing.T) {
	setupTypeAliasPolicy(t)
	if err := ImportSourceTypes("alias_missing", []SourceTypeExport{{SourceTypeID: "adult", GlobalTypeID: 902, TypeName: "伦理片"}}); err != nil {
		t.Fatal(err)
	}
	rows := []*model.Video{{VodId: "1", TypeId: "adult", VodName: "missing category"}, {VodId: "2", TypeId: "adult", TypeName: "剧情片", VodName: "conflicting category"}}
	if err := UpsertCatalogItems("alias_missing", rows); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := DB().Get(&count, `SELECT COUNT(*) FROM source_videos WHERE source_key='alias_missing'`); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("source category denial bypassed by name: %d rows", count)
	}
}

func TestLegacySourceCategoryDenialHidesMislabelledRows(t *testing.T) {
	setupTypeAliasPolicy(t)
	if err := ImportSourceTypes("alias_old_source", []SourceTypeExport{{SourceTypeID: "adult", GlobalTypeID: 902, TypeName: "伦理片"}}); err != nil {
		t.Fatal(err)
	}
	_, err := DB().Exec(`INSERT INTO source_videos(source_key,source_vod_id,global_id,source_type_id,type_name,global_type_id,vod_name,lifecycle_state)
	 VALUES ('alias_old_source','old',0,'adult','剧情片',905,'legacy disguised','active')`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GetCatalogItem("alias_old_source", "old"); err == nil {
		t.Fatal("source-category denial was bypassed by old display name and global ID")
	}
}
