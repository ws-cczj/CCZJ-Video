package handler

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRemoteSearchAndImportRespectDisabledSourceCategories(t *testing.T) {
	const key = "blocked_search_fixture"
	rows := []*model.Video{
		{VodId: "1", TypeId: "welfare", TypeName: "福利", VodName: "blocked welfare"},
		{VodId: "2", TypeId: "ethics", TypeName: "倫理片", VodName: "blocked traditional"},
		{VodId: "3", TypeId: "ethics", VodName: "blocked missing name"},
		{VodId: "4", TypeId: "ethics", TypeName: "剧情片", VodName: "blocked wrong name"},
		{VodId: "5", TypeId: "normal", TypeName: "剧情片", VodName: "allowed search"},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"code": 1, "total": len(rows), "pagecount": 1, "page": 1, "list": rows})
	}))
	defer server.Close()
	if _, err := db.DB().Exec(`INSERT INTO global_types(type_name,collect_enabled) VALUES ('福利',0),('伦理片',0),('倫理片',1)`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.DeleteSource(key)
		db.DB().Exec(`DELETE FROM global_types WHERE type_name IN ('福利','伦理片','倫理片')`)
	})
	if err := db.AddSource(&model.Source{SourceKey: key, Name: "Policy test", ApiUrl: server.URL, Enabled: 1}); err != nil {
		t.Fatal(err)
	}
	var id int64
	if err := db.DB().Get(&id, `SELECT id FROM global_types WHERE type_name='伦理片'`); err != nil {
		t.Fatal(err)
	}
	if err := db.ImportSourceTypes(key, []db.SourceTypeExport{{SourceTypeID: "ethics", GlobalTypeID: id, TypeName: "伦理片"}}); err != nil {
		t.Fatal(err)
	}
	preview, err := SearchSource(key, "search", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Videos) != 1 || preview.Videos[0].VodName != "allowed search" {
		t.Fatalf("source preview leaked blocked categories: %+v", preview.Videos)
	}
	result, err := SearchVideos(VideoSearchReq{SourceKey: key, Keyword: "search", Page: 1, PageSize: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Videos) != 1 || result.Videos[0].VodName != "allowed search" {
		t.Fatalf("remote search leaked blocked categories: %+v", result.Videos)
	}
	count, err := ImportSourceVideos(key, rows)
	if err != nil || count != 1 {
		t.Fatalf("import count=%d error=%v", count, err)
	}
	var stored int
	if err := db.DB().Get(&stored, `SELECT COUNT(*) FROM source_videos WHERE source_key=?`, key); err != nil {
		t.Fatal(err)
	}
	if stored != 1 {
		t.Fatalf("stored %d rows, want only allowed result", stored)
	}
}
