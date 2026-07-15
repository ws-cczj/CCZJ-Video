package model

import "testing"

func TestValidateSourceKey(t *testing.T) {
	valid := []string{"source", "source_1", "a", "a123", "a_b_c"}
	for _, key := range valid {
		if err := ValidateSourceKey(key); err != nil {
			t.Fatalf("ValidateSourceKey(%q) returned %v", key, err)
		}
	}

	invalid := []string{"", " Source", "source ", "Source", "source-key", "source;drop", "1" + string(make([]byte, 64))}
	for _, key := range invalid {
		if err := ValidateSourceKey(key); err == nil {
			t.Fatalf("ValidateSourceKey(%q) unexpectedly succeeded", key)
		}
	}
}
