package db

import (
	"cczjVideo/app/model"
	"fmt"
	"strings"
	"testing"
)

// 源站偶尔把 type_name 给成一串空格，或者前后带空白。这类名字进了界面就是一个
// 说不出是什么、又点得动的空标签（「最近更新」标签行末尾那个莫名出现的东西），
// 所以读路径要过滤 + trim，写路径不能再把空白存进映射表。
func TestBlankTypeNameNeverSurfacesAsAType(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	const sourceKey = "blank_type_test"
	const paddedName = "Blank test padded type"

	cleanup := func() {
		_, _ = DB().Exec(`DELETE FROM source_types WHERE source_key=?`, sourceKey)
		_, _ = DB().Exec(`DELETE FROM source_videos WHERE source_key=?`, sourceKey)
		_, _ = DB().Exec(`DELETE FROM global_types WHERE type_name=?`, paddedName)
	}
	cleanup()
	defer cleanup()

	// 库里已经躺着的历史行：只能靠读路径修，写路径管不到它们。
	if _, err := DB().Exec(`INSERT INTO source_types(source_key,source_type_id,global_type_id,type_name,updated_at) VALUES
		(?, 'blank', 0, ?, CURRENT_TIMESTAMP),
		(?, 'padded', 0, ?, CURRENT_TIMESTAMP)`, sourceKey, "   ", sourceKey, "  "+paddedName+"  "); err != nil {
		t.Fatal(err)
	}
	types, err := GetTypes(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	if name := onlyTypeName(t, types); name != paddedName {
		t.Fatalf("types = %q; want only %q", name, paddedName)
	}
	if len(types) == 1 && types[0].TypeId.String() != "padded" {
		t.Fatalf("type id = %q; want padded", types[0].TypeId.String())
	}

	// 新采进来的一批：映射表里不该再落下空白名字。
	if _, err := DB().Exec(`DELETE FROM source_types WHERE source_key=?`, sourceKey); err != nil {
		t.Fatal(err)
	}
	if err := UpsertCatalogItems(sourceKey, []*model.Video{
		{VodId: "blank-1", TypeId: "blank", TypeName: "   ", VodName: "Blank type video"},
		{VodId: "padded-1", TypeId: "padded", TypeName: "  " + paddedName + "  ", VodName: "Padded type video"},
	}); err != nil {
		t.Fatal(err)
	}
	stored, err := ExportSourceTypes(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	// 落库时就得把空白去掉：读路径的 trim 只兜住了界面，导出与源站映射还是脏名字。
	for _, row := range stored {
		if row.TypeName != strings.TrimSpace(row.TypeName) {
			t.Fatalf("source type %q stored with surrounding whitespace: %q", row.SourceTypeID, row.TypeName)
		}
		if row.SourceTypeID == "padded" && row.TypeName != paddedName {
			t.Fatalf("padded source type stored as %q; want %q", row.TypeName, paddedName)
		}
	}
	if onlyTypeName(t, mustGetTypes(t, sourceKey)) != paddedName {
		t.Fatal("ingested blank type surfaced")
	}
}

func mustGetTypes(t *testing.T, sourceKey string) []*model.VType {
	t.Helper()
	types, err := GetTypes(sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	return types
}

// 单个类型名，顺便替断言把「多出来一个空标签」这种情况喊清楚。
func onlyTypeName(t *testing.T, types []*model.VType) string {
	t.Helper()
	if len(types) != 1 {
		names := make([]string, 0, len(types))
		for _, item := range types {
			names = append(names, fmt.Sprintf("%q", item.Name))
		}
		t.Fatalf("got %d types: %s; want exactly 1", len(types), strings.Join(names, ", "))
	}
	return types[0].Name
}
