package source

import (
	"cczjVideo/app/model"
	"testing"
)

func TestImportRejectsUnsafeSourceKeyBeforeDatabaseAccess(t *testing.T) {
	service := NewService()
	_, err := service.Import(Payload{Source: &model.Source{SourceKey: "unsafe-key"}}, "test")
	if err == nil {
		t.Fatal("expected unsafe source key to be rejected")
	}
}

func TestImportRejectsMissingSource(t *testing.T) {
	service := NewService()
	_, err := service.Import(Payload{}, "test")
	if err == nil {
		t.Fatal("expected missing source to be rejected")
	}
}
