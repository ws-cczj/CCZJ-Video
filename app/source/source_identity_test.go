package source

import (
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestAddSourceAllocatesIndependentIdentityAndKeepsExplicitKeys(t *testing.T) {
	const address = "https://api.example.com/video"
	seen := make(map[string]bool)
	for _, key := range []string{"", "", "legacy_kept"} {
		row := &model.Source{SourceKey: key, ApiUrl: address, Enabled: 1}
		if err := AddSource(row); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = db.DeleteSource(row.SourceKey) })
		if seen[row.SourceKey] || (key != "" && row.SourceKey != key) {
			t.Fatalf("source identity reused or rewritten: %q", row.SourceKey)
		}
		seen[row.SourceKey] = true
	}
}
