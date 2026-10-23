package library

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

func TestSourceIdentitySurvivesAddressChangeAndAllowsSharedHost(t *testing.T) {
	s := setupLibraryTest(t)
	var comic provider.Document
	if err := json.Unmarshal([]byte(novelDocument()), &comic); err != nil {
		t.Fatal(err)
	}
	comic.Kind = "comic"
	comic.Operations.Pages = &provider.Operation{Format: "html", List: &provider.Selector{Query: "img"}, Fields: map[string]provider.Selector{provider.FieldImageURL: {Query: "img", Attr: "src"}}}
	comicJSON, err := json.Marshal(&comic)
	if err != nil {
		t.Fatal(err)
	}
	addrs := []struct {
		kind              model.SourceKind
		address, document string
	}{
		{model.SourceKindNovel, "https://api.example.com/novel", novelDocument()},
		{model.SourceKindNovel, "https://api.example.com/comic", novelDocument()},
		{model.SourceKindNovel, "https://example.cn/api", novelDocument()},
		{model.SourceKindComic, "https://api.example.com/novel", string(comicJSON)},
	}
	keys := make(map[string]bool)
	for _, entry := range addrs {
		source, err := s.AddSource(entry.kind, "source", entry.address, entry.document)
		if err != nil {
			t.Fatal(err)
		}
		if keys[source.SourceKey] {
			t.Fatal("independent source instances shared an identity")
		}
		keys[source.SourceKey] = true
		if err := s.UpdateSource(source.SourceKey, "renamed", "https://replacement.example.net/new", ""); err != nil {
			t.Fatal(err)
		}
		got, err := db.GetSourceByKey(source.SourceKey)
		if err != nil || got.SourceKey != source.SourceKey || got.ApiUrl != "https://replacement.example.net/new" {
			t.Fatalf("source identity changed on update: %+v, %v", got, err)
		}
	}
}

func TestSourceCreationRejectsNonHTTPAddress(t *testing.T) {
	s := setupLibraryTest(t)
	for _, address := range []string{"example.com/novel", "file:///novel", "https:///novel"} {
		if _, err := s.AddSource(model.SourceKindNovel, "invalid", address, novelDocument()); err == nil {
			t.Fatalf("accepted invalid source address %q", address)
		}
	}
}

func TestTextCachePreservesDiagnosticsAndRejectsLegacyEntries(t *testing.T) {
	s := setupLibraryTest(t)
	content := &provider.TextContent{Paragraphs: []string{"partial text"}, Truncated: true, Garbled: true, FontObfuscated: true}
	s.writeTextCache("diagnostics-v2", content)
	cached, ok := s.readTextCache("diagnostics-v2")
	if !ok || !reflect.DeepEqual(contentWarnings(cached), contentWarnings(content)) || !reflect.DeepEqual(cached.Paragraphs, content.Paragraphs) {
		t.Fatalf("cached content lost diagnostics: %+v", cached)
	}
	s.textCache.Write("legacy", []byte(`{"paragraphs":["unknown completeness"]}`), "application/json")
	if _, ok := s.readTextCache("legacy"); ok {
		t.Fatal("legacy cache without completeness metadata was accepted")
	}
}

func TestChapterTextKeepsTruncationWarningOnCacheHit(t *testing.T) {
	s := setupLibraryTest(t)
	s.limits.MaxContentBytes = 1
	server := fakeSource()
	defer server.Close()
	addTestSource(t, server, "truncated_text", novelDocument())
	found, err := s.Search(context.Background(), model.SourceKindNovel, "示例", 1, nil)
	if err != nil || found.Total == 0 {
		t.Fatalf("search: %+v, %v", found, err)
	}
	detail, err := s.Detail(context.Background(), found.Results[0].Items[0].ID, true)
	if err != nil || len(detail.Chapters) == 0 {
		t.Fatalf("detail: %+v, %v", detail, err)
	}
	chapterID := detail.Chapters[0].ID
	first, err := s.ChapterText(context.Background(), chapterID)
	if err != nil || len(first.Warnings) == 0 {
		t.Fatalf("expected initial truncation warning: %+v, %v", first, err)
	}
	second, err := s.ChapterText(context.Background(), chapterID)
	if err != nil || !second.FromCache || !reflect.DeepEqual(first.Warnings, second.Warnings) {
		t.Fatalf("cached warning changed: %+v, %v", second, err)
	}
	s.limits.MaxContentBytes = provider.DefaultLimits().MaxContentBytes
	third, err := s.ChapterText(context.Background(), chapterID)
	if err != nil || third.FromCache || len(third.Warnings) != 0 {
		t.Fatalf("raised content budget did not invalidate partial cache: %+v, %v", third, err)
	}
}
