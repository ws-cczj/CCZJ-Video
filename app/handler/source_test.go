package handler

import (
	"cczjVideo/app/model"
	"strings"
	"testing"
)

func TestAddSourceRejectsUnsafeSourceKeyBeforeDatabaseAccess(t *testing.T) {
	err := AddSource(&model.Source{
		SourceKey: "unsafe-key",
		ApiUrl:    "https://example.com/api.php/provide/vod/",
	})
	if err == nil {
		t.Fatal("expected unsafe source key to be rejected")
	}
	if !strings.Contains(err.Error(), "invalid source_key") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDeriveKeyProducesSafeKey(t *testing.T) {
	key := deriveKey("https://api.example-video.com/api.php/provide/vod/")
	if err := model.ValidateSourceKey(key); err != nil {
		t.Fatalf("derived key %q is invalid: %v", key, err)
	}
}
