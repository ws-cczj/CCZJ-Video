package db

import (
	"cczjVideo/app/model"
	"testing"
)

// Upstream search APIs commonly match only a title prefix, so "大臣" misses
// "是，大臣". The catalog LIKE search is what recovers those titles.
func TestCatalogKeywordSearchMatchesSubstringTitles(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const (
		sourceKey = "catalog_keyword_test"
		typeName  = "Catalog keyword type"
	)
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(sourceKey, []*model.Video{
		{VodId: "yes-minister", TypeId: "1", TypeName: typeName, VodName: "是，大臣", VodTime: "2026-01-02"},
		{VodId: "unrelated", TypeId: "1", TypeName: typeName, VodName: "完全不同的剧", VodTime: "2026-01-01"},
	}); err != nil {
		t.Fatal(err)
	}

	page, err := GetCatalogVideoPage(sourceKey, FilterParams{Keyword: "大臣", PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Videos) != 1 || page.Videos[0].VodId != "yes-minister" {
		t.Fatalf("keyword search = total %d, videos %+v; want only 是，大臣", page.Total, page.Videos)
	}
}

// A keyword that names a category must not turn the supplement into a dump of
// every video of that category.
func TestSearchCatalogByTitleIgnoresTypeAndRemarks(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const (
		sourceKey = "catalog_title_only_test"
		typeName  = "综艺"
	)
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(sourceKey, []*model.Video{
		{VodId: "match", TypeId: "1", TypeName: typeName, VodName: "快乐综艺大挑战", VodTime: "2026-01-02"},
		{VodId: "type-only", TypeId: "1", TypeName: typeName, VodName: "完全不相关的名字", VodTime: "2026-01-01"},
	}); err != nil {
		t.Fatal(err)
	}

	matches, err := SearchCatalogByTitle(sourceKey, "综艺", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 1 || matches[0].VodId != "match" {
		t.Fatalf("title search = %+v; want only the title match", matches)
	}
	if !matches[0].InCatalog {
		t.Error("title matches must be flagged as already stored")
	}
}

func TestExistingCatalogVodIDsReportsStoredRowsOnly(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const (
		sourceKey = "catalog_membership_test"
		typeName  = "Catalog membership type"
	)
	if _, err := DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(sourceKey, []*model.Video{
		{VodId: "stored", TypeId: "1", TypeName: typeName, VodName: "Stored title"},
		{VodId: "removed", TypeId: "1", TypeName: typeName, VodName: "Removed title"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := DeleteCatalogVideo(sourceKey, "removed"); err != nil {
		t.Fatal(err)
	}

	existing, err := ExistingCatalogVodIDs(sourceKey, []string{"stored", "removed", "never-seen", "  "})
	if err != nil {
		t.Fatal(err)
	}
	if !existing["stored"] {
		t.Error("stored row not reported as existing")
	}
	if existing["removed"] || existing["never-seen"] || len(existing) != 1 {
		t.Fatalf("membership set = %v; want only stored", existing)
	}

	// An id from another source must not leak into this source's membership.
	otherExisting, err := ExistingCatalogVodIDs("catalog_membership_other", []string{"stored"})
	if err != nil {
		t.Fatal(err)
	}
	if len(otherExisting) != 0 {
		t.Fatalf("other source membership = %v; want empty", otherExisting)
	}
}
