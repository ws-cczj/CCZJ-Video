package library

import (
	"cczjVideo/app/model"
	"context"
	"testing"
)

func TestProductAuditRegistryMatchesCompletedFrozenBatch(t *testing.T) {
	s := setupLibraryTest(t)
	defer func() {
		if err := s.Close(context.Background()); err != nil {
			t.Error(err)
		}
	}()
	total := 0
	for _, tc := range []struct {
		kind           model.SourceKind
		count, adapted int
		// The frozen inputs are unchanged. One maintained catalog adapter was added; two
		// fixed lists no longer collapse into converted keyword-search identities.
	}{{model.SourceKindNovel, 3705, 42}, {model.SourceKindComic, 2530, 4}} {
		rows, err := s.registryFor(tc.kind)
		if err != nil {
			t.Fatal(err)
		}
		adapted := 0
		seen := map[string]bool{}
		for _, row := range rows {
			if row.Kind != string(tc.kind) || seen[row.ID] {
				t.Fatal("mixed or duplicate source", row.ID)
			}
			seen[row.ID] = true
			if row.CanRead {
				adapted++
			}
		}
		if len(rows) != tc.count || adapted != tc.adapted {
			t.Fatal(tc.kind, len(rows), adapted)
		}
		total += len(rows)
	}
	if total != 6235 {
		t.Fatal(total)
	}
}
