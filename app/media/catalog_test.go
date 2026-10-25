package media

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"encoding/json"
	"testing"
)

func TestVideoListDecodesShortDramaBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		short         bool
	}{
		{"short drama", `{"source_key":"test","short_drama":true,"page_size":24}`, true},
		{"existing caller", `{"source_key":"test","page_size":24}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := VideoListReq{ShortDrama: true}
			if err := json.Unmarshal([]byte(tc.payload), &req); err != nil {
				t.Fatal(err)
			}
			if req.ShortDrama != tc.short || req.PageSize != 24 {
				t.Fatalf("decoded request: %+v", req)
			}
		})
	}
}

func TestSearchLocalCatalogDropsRemoteHitsAndCapsResults(t *testing.T) {
	const sourceKey = "local_supplement_test"
	if _, err := db.DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	seed := make([]*model.Video, 0, 3)
	for i, name := range []string{"是，大臣", "是的，首相", "大臣笔记"} {
		seed = append(seed, &model.Video{
			VodId: model.FlexibleString("vod-" + string(rune('a'+i))), TypeId: "1",
			TypeName: "Local supplement type", VodName: name, VodTime: "2026-01-0" + string(rune('1'+i)),
		})
	}
	if err := db.UpsertCatalogItems(sourceKey, seed); err != nil {
		t.Fatal(err)
	}

	// "首相" only exists in the catalog; the upstream search never returns it.
	matches := newTestMedia().searchLocalCatalog(context.Background(), sourceKey, "大臣", []string{"vod-a"})
	if len(matches) != 1 {
		t.Fatalf("local matches = %d (%v); want 1 after excluding the remote hit", len(matches), matches)
	}
	if matches[0].VodName != "大臣笔记" {
		t.Fatalf("local match = %q; want 大臣笔记", matches[0].VodName)
	}
	if !matches[0].InCatalog {
		t.Error("local matches must be flagged as already stored")
	}
}
