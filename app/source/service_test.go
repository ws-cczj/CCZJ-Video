package source

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/andybalholm/brotli"
)

var testDBDir string

func TestMain(m *testing.M) {
	testDBDir, _ = os.MkdirTemp("", "cczj-source-test-")
	code := m.Run()
	db.Close()
	_ = os.RemoveAll(testDBDir)
	os.Exit(code)
}

func TestImportRejectsUnsafeSourceKeyBeforeDatabaseAccess(t *testing.T) {
	service := NewService()
	_, err := service.Import(Payload{Source: &model.Source{SourceKey: "unsafe-key"}}, "test")
	if err == nil {
		t.Fatal("expected unsafe source key to be rejected")
	}
}

func TestV2ExportIsCatalogOnlyAndRoundTripsTypeMappings(t *testing.T) {
	dir := testDBDir
	if err := db.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	src := &model.Source{SourceKey: "source_1", Name: "Source", ApiUrl: "https://example.test/api", Enabled: 1}
	if err := db.AddSource(src); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertCatalogItems(src.SourceKey, []*model.Video{{VodId: "upstream-1", TypeId: "remote-7", TypeName: "Movie", VodName: "Catalog title", VodPic: "cover", VodRemarks: "remark"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.ImportSourceTypes(src.SourceKey, []db.SourceTypeExport{{SourceTypeID: "remote-7", GlobalTypeID: 42, TypeName: "Movie"}}); err != nil {
		t.Fatal(err)
	}
	path, err := NewService().Export(dir, src.SourceKey)
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	reader := brotli.NewReader(file)
	var payload Payload
	if err := json.NewDecoder(reader).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Version != 2 || len(payload.Videos) != 1 || len(payload.Types) != 1 {
		t.Fatalf("payload=%+v", payload)
	}
	if payload.Types[0].TypeID != "remote-7" || payload.Types[0].GlobalTypeID != 42 {
		t.Fatalf("type mapping lost: %+v", payload.Types[0])
	}
	// Brotli bytes are not searchable; decode them for a field-level assertion.
	if _, err = file.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(brotli.NewReader(file))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"vod_content", "vod_actor", "vod_director", "vod_play_url", "vod_down_url"} {
		if strings.Contains(string(b), forbidden) {
			t.Fatalf("v2 export contains %s", forbidden)
		}
	}
	if err := db.TruncateSource(src.SourceKey); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService().Import(payload, "roundtrip"); err != nil {
		t.Fatal(err)
	}
	types, err := db.ExportSourceTypes(src.SourceKey)
	if err != nil || len(types) != 1 || types[0].GlobalTypeID != 42 {
		t.Fatalf("restored types=%+v err=%v", types, err)
	}
}

func TestV1DetailFieldsAreIgnoredDuringImport(t *testing.T) {
	dir := testDBDir
	if err := db.InitDB(dir); err != nil {
		t.Fatal(err)
	}
	var payload Payload
	if err := json.Unmarshal([]byte(`{"version":1,"source":{"source_key":"source_2","name":"S","api_url":"https://example.test"},"videos":[{"vod_id":"v1","type_id":"t1","type_name":"Movie","vod_name":"Title","vod_content":"secret","vod_actor":"actor","vod_director":"director","vod_play_url":"play","vod_down_url":"download"}]}`), &payload); err != nil {
		t.Fatal(err)
	}
	if _, err := NewService().Import(payload, "v1"); err != nil {
		t.Fatal(err)
	}
	item, err := db.GetCatalogItem("source_2", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if item.VodName != "Title" {
		t.Fatalf("catalog=%+v", item)
	}
	var columns []string
	if err := db.DB().Select(&columns, `SELECT name FROM pragma_table_info('global_video') WHERE name IN ('content','actor','director')`); err != nil {
		t.Fatal(err)
	}
	if len(columns) != 0 {
		t.Fatalf("v1 import retained detail columns: %v", columns)
	}
}

func TestImportRejectsMissingSource(t *testing.T) {
	service := NewService()
	_, err := service.Import(Payload{}, "test")
	if err == nil {
		t.Fatal("expected missing source to be rejected")
	}
}
