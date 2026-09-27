package db

import (
	"os"
	"strings"
	"testing"
)

var boundaryTestDBDir string

func TestMain(m *testing.M) {
	boundaryTestDBDir, _ = os.MkdirTemp("", "cczj-db-test-")
	code := m.Run()
	Close()
	_ = os.RemoveAll(boundaryTestDBDir)
	os.Exit(code)
}

func TestFreshRuntimeSchemaHasNoDynamicSourceTables(t *testing.T) {
	if err := InitDB(boundaryTestDBDir); err != nil {
		t.Fatal(err)
	}
	var names []string
	if err := DB().Select(&names, `SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`); err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		if strings.HasPrefix(name, "v_") || strings.HasPrefix(name, "e_") {
			t.Fatalf("fresh runtime created legacy dynamic table %q", name)
		}
	}
	var detailColumns []string
	if err := DB().Select(&detailColumns, `SELECT name FROM pragma_table_info('global_video') WHERE name IN ('director','actor','content')`); err != nil {
		t.Fatal(err)
	}
	if len(detailColumns) != 0 {
		t.Fatalf("global_video persists detail columns: %v", detailColumns)
	}
}
