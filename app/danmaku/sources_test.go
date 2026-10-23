package danmaku

import (
	"context"
	"testing"

	"cczjVideo/app/apperror"
)

func TestSourceSettingsValidationAndDefault(t *testing.T) {
	for _, raw := range []string{`null`, ` null `, `[]`, `{"disabled":null}`, `{"defaultProvider":null}`, `{"defaultProvider":"external"}`, `{"disabled":["external"]}`} {
		if _, err := DecodeSourceSettings(raw); err == nil {
			t.Fatalf("invalid settings accepted: %s", raw)
		}
	}
	s, err := DecodeSourceSettings(`{"defaultProvider":"qq","disabled":["qq","qq"]}`)
	if err != nil || s.DefaultProvider != "bilibili" || s.Enabled("qq") || len(s.Disabled) != 1 {
		t.Fatal(s, err)
	}
	s, err = DecodeSourceSettings(`{"disabled":["bilibili","qq","iqiyi"]}`)
	if err != nil || s.DefaultProvider != "" {
		t.Fatal(s, err)
	}
}

func TestDisabledSourceBlocksReadsWithoutRemovingBinding(t *testing.T) {
	s, p, _ := boundService(t)
	ctx := context.Background()
	database, _ := s.store.db()
	if _, err := database.Exec(`INSERT INTO settings(key,value) VALUES(?,?)`, SourceSettingsKey, `{"disabled":["qq"]}`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ReadWindow(ctx, "video:source:episode", 0, 1); !apperror.Is(err, apperror.PermissionDenied) {
		t.Fatal("disabled source read allowed", err)
	}
	if _, err := s.Search(ctx, "qq", "sample"); !apperror.Is(err, apperror.PermissionDenied) {
		t.Fatal("disabled source search allowed", err)
	}
	if p.calls.Load() != 0 {
		t.Fatal("disabled source reached network")
	}
	if binding, err := s.Binding(ctx, "video:source:episode"); err != nil || binding == nil {
		t.Fatal("disabled source lost its binding", binding, err)
	}
}

func TestRepeatedWindowsReadOnlyTheBoundProviderAndReuseSegments(t *testing.T) {
	s, selected, _ := boundService(t)
	other := &testProvider{}
	s.providers["bilibili"] = other
	s.providers["iqiyi"] = other
	ctx := context.Background()
	first, err := s.ReadWindow(ctx, "video:source:episode", 6000, 1)
	if err != nil || len(first.Items) == 0 {
		t.Fatal(first, err)
	}
	count := selected.calls.Load()
	indexes := selected.indexes.Load()
	for _, position := range []int64{6000, 10000, 20000, 6000} {
		window, err := s.ReadWindow(ctx, "video:source:episode", position, 1)
		if err != nil || !window.Cached {
			t.Fatal(window, err)
		}
	}
	if selected.calls.Load() != count || selected.indexes.Load() != indexes || other.calls.Load() != 0 || other.indexes.Load() != 0 {
		t.Fatal("duplicate or unselected provider reads", selected.calls.Load(), count, other.calls.Load())
	}
}
