package library

import (
	"encoding/json"
	"os"
	"testing"
)

// Maintainer tool: transform an upstream collection into an offline metadata
// library. Only supported static rules are retained; scripts/APKs are omitted.
func TestBuildSourceLibrarySnapshot(t *testing.T) {
	input, output := os.Getenv("CCZJ_CATALOG_INPUT"), os.Getenv("CCZJ_CATALOG_OUTPUT")
	if input == "" || output == "" {
		t.Skip("set CCZJ_CATALOG_INPUT/OUTPUT to rebuild library snapshot")
	}
	raw, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, kind := range []string{"novel", "comic"} {
		rows, err := parseSourceCatalog(raw, kind, os.Getenv("CCZJ_CATALOG_ORIGIN"))
		if err != nil {
			t.Fatal(err)
		}
		available := 0
		for _, row := range rows {
			record := map[string]any{"name": row.Name, "apiUrl": row.ApiURL, "kind": kind, "project": row.Project, "format": row.Format, "origin": row.Origin, "reason": row.Reason, "summary": row.Summary}
			if row.Compatible {
				record["document"] = json.RawMessage(row.document)
				available++
			}
			out = append(out, record)
		}
		t.Logf("%s: %d candidates, %d compatible", kind, len(rows), available)
	}
	encoded, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, append(encoded, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
}
