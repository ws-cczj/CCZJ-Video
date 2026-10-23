package library

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

func TestCatalogPreviewSelectsOnlyChosenSourceAndNeverSilentlyConvertsScripts(t *testing.T) {
	s := setupLibraryTest(t)
	payload, _ := json.Marshal([]any{
		map[string]any{"name": "one", "apiUrl": "https://one.example", "kind": "novel", "document": json.RawMessage(novelDocument())},
		map[string]any{"name": "two", "apiUrl": "https://two.example", "kind": "novel", "document": json.RawMessage(novelDocument())},
		map[string]any{"bookSourceName": "script", "bookSourceUrl": "https://script.example", "searchUrl": "<js>java.ajax()</js>"},
	})
	preview, err := s.PreviewSources(context.Background(), model.SourceKindNovel, string(payload), "https://github.com/example/sources")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Candidates) != 3 {
		t.Fatal(preview)
	}
	rows, _ := db.GetSourcesByKind(model.SourceKindNovel)
	if len(rows) != 0 {
		t.Fatal("preview installed sources")
	}
	var selected string
	for _, candidate := range preview.Candidates {
		if candidate.Name == "one" {
			selected = candidate.ID
		}
		if candidate.Name == "script" && candidate.Compatible {
			t.Fatal("silently accepted script")
		}
	}
	result, err := s.ImportSourceCandidates(context.Background(), preview.ID, []string{selected})
	if err != nil || len(result) != 1 || result[0].Status != "imported" {
		t.Fatal(result, err)
	}
	result, err = s.ImportSourceCandidates(context.Background(), preview.ID, []string{selected})
	if err != nil || result[0].Status != "existing" {
		t.Fatal(result, err)
	}
	rows, _ = db.GetSourcesByKind(model.SourceKindNovel)
	if len(rows) != 1 || rows[0].Name != "one" {
		t.Fatal(rows)
	}
	s.catalogMu.Lock()
	s.catalogs[preview.ID].expires = time.Now().Add(-time.Second)
	s.catalogMu.Unlock()
	if _, err = s.ImportSourceCandidates(context.Background(), preview.ID, []string{selected}); err == nil {
		t.Fatal("accepted expired preview")
	}
}

func TestQualityRanksCompleteReadingOverFastSearchAndKeepsUntestedDistinct(t *testing.T) {
	s := setupLibraryTest(t)
	partial, err := s.saveSourceQuality(strings.Repeat("a", 64), &TestOutcome{Stages: []*TestStage{{Name: "config", Status: "ok"}, {Name: "search", Status: "ok"}, {Name: "toc", Status: "failed"}}})
	if err != nil {
		t.Fatal(err)
	}
	good, err := s.saveSourceQuality(strings.Repeat("b", 64), &TestOutcome{OK: true, Stages: []*TestStage{{Name: "config", Status: "ok"}, {Name: "search", Status: "ok", Millis: 2000}, {Name: "toc", Status: "ok"}, {Name: "content", Status: "ok"}}})
	if err != nil {
		t.Fatal(err)
	}
	candidates := []*SourceCandidate{{Name: "unknown", Compatible: true}, {Name: "partial", Compatible: true, Quality: partial}, {Name: "good", Compatible: true, Quality: good}, {Name: "unsupported"}}
	rankSourceCandidates(candidates)
	if candidates[0].Name != "good" || candidates[1].Name != "partial" || candidates[2].Quality != nil || candidates[3].Compatible {
		t.Fatal(candidates)
	}
	if partial.Status == "healthy" || good.Samples != 1 || good.Successes != 1 {
		t.Fatal(partial, good)
	}
}

func TestLegadoStaticRulesAndMihonMetadataHaveExplicitCompatibility(t *testing.T) {
	raw := `[{"bookSourceName":"static","bookSourceUrl":"https://example.org","searchUrl":"/search?q={{key}}","ruleSearch":{"bookList":"@css:.books a","name":"text","bookUrl":"href"},"ruleBookInfo":{"name":"@css:h1@text"},"ruleToc":{"chapterList":"@css:.chapters a","chapterName":"text","chapterUrl":"href"},"ruleContent":{"content":"@css:#content@text"}}]`
	rows, err := parseSourceCatalog([]byte(raw), "novel", "")
	if err != nil || len(rows) != 1 || !rows[0].Compatible {
		t.Fatal(rows, err)
	}
	raw = `[{"pkg":"eu.kanade.tachiyomi.extension.zh.test","sources":[{"name":"extension","baseUrl":"https://example.org","lang":"zh"}]}]`
	rows, err = parseSourceCatalog([]byte(raw), "comic", "")
	if err != nil || len(rows) != 1 || rows[0].Compatible || rows[0].Reason == "" {
		t.Fatal(rows, err)
	}
}

func TestModernMihonCatalogAndImportMoreThanThree(t *testing.T) {
	modern := `{"name":"Keiyoushi","extensionList":{"extensions":[{"packageName":"example.extension","sources":[{"name":"咚漫","homeUrl":"https://www.dongmanmanhua.cn","language":"zh"},{"name":"未知","homeUrl":"https://example.org","language":"zh"}]}]}}`
	rows, err := parseSourceCatalog([]byte(modern), "comic", "")
	if err != nil || len(rows) != 2 || !rows[0].Compatible || rows[1].Compatible {
		t.Fatalf("modern index: %+v %v", rows, err)
	}
	s := setupLibraryTest(t)
	var payload []map[string]any
	for _, host := range []string{"one", "two", "three", "four", "five"} {
		payload = append(payload, map[string]any{"name": host, "apiUrl": "https://" + host + ".example", "kind": "novel", "document": json.RawMessage(novelDocument())})
	}
	raw, _ := json.Marshal(payload)
	preview, err := s.PreviewSources(context.Background(), model.SourceKindNovel, string(raw), "")
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, row := range preview.Candidates {
		ids = append(ids, row.ID)
	}
	imported, err := s.ImportSourceCandidates(context.Background(), preview.ID, ids)
	if err != nil || len(imported) != 5 {
		t.Fatalf("five imports: %+v %v", imported, err)
	}
	for _, row := range imported {
		if row.Status != "imported" {
			t.Fatal(row)
		}
	}
}

func TestOfflineSourceLibrariesExposeFullCatalogWithoutNetworkOrAutomaticImports(t *testing.T) {
	s := setupLibraryTest(t)
	for _, kind := range []model.SourceKind{model.SourceKindNovel, model.SourceKindComic} {
		preview, err := s.PreviewSources(context.Background(), kind, "community", "")
		if err != nil {
			t.Fatal(err)
		}
		ready := 0
		for _, row := range preview.Candidates {
			if row.Compatible {
				ready++
			}
		}
		if len(preview.Candidates) < 1000 || ready < 4 {
			t.Fatalf("%s: total=%d compatible=%d", kind, len(preview.Candidates), ready)
		}
		installed, err := s.ListSources(kind)
		if err != nil || len(installed) != 0 {
			t.Fatalf("opening catalog installed sources: %+v %v", installed, err)
		}
	}
}

func TestDefaultCatalogOnlyOffersAdaptedSourcesAndCommunityRetainsMeasuredFailures(t *testing.T) {
	s := setupLibraryTest(t)
	ctx := context.Background()
	for _, kind := range []model.SourceKind{model.SourceKindNovel, model.SourceKindComic} {
		preview, err := s.PreviewSources(ctx, kind, "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range preview.Candidates {
			if !row.Compatible {
				t.Fatalf("default adapted-source tab offered unsupported %s", row.Name)
			}
		}
	}
	preview, err := s.PreviewSources(ctx, model.SourceKindNovel, "community", "")
	if err != nil {
		t.Fatal(err)
	}
	measured, failed := 0, 0
	for _, row := range preview.Candidates {
		if row.Quality == nil {
			continue
		}
		if row.Quality.Evidence != "bundled" {
			t.Fatal("shipped observations must be distinguished from local checks")
		}
		measured++
		if row.Quality.Status == "failed" {
			failed++
		}
	}
	if measured < 40 || failed < 30 {
		t.Fatalf("lost measured source failures: measured=%d failed=%d", measured, failed)
	}
}

func TestRemoteSourcePreviewDoesNotTrustSuppliedScoresAndImportRequiresValidSelection(t *testing.T) {
	s := setupLibraryTest(t)
	raw, _ := json.Marshal([]any{map[string]any{"name": "remote", "apiUrl": "https://remote.example", "kind": "novel", "document": json.RawMessage(novelDocument()), "quality": map[string]any{"status": "healthy", "score": 100, "checkedAt": time.Now().Unix()}}})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}))
	defer server.Close()
	preview, err := s.PreviewSources(context.Background(), model.SourceKindNovel, server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Candidates) != 1 || preview.Candidates[0].Quality != nil {
		t.Fatal("trusted remote self-reported score", preview)
	}
	if _, err = s.ImportSourceCandidates(context.Background(), preview.ID, []string{"unselected-foreign-id"}); err == nil {
		t.Fatal("accepted a source outside preview")
	}
}
