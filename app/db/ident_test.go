package db

import "testing"

func TestSafeIdentDoesNotNormalizeUnsafeInput(t *testing.T) {
	if got := safeIdent("source_1"); got != "source_1" {
		t.Fatalf("safeIdent valid value = %q", got)
	}
	for _, raw := range []string{"source-1", "source!", "", "source key"} {
		if got := safeIdent(raw); got != "__invalid_identifier__" {
			t.Fatalf("safeIdent(%q) = %q, want sentinel", raw, got)
		}
	}
}
