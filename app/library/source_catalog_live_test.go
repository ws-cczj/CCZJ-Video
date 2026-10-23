package library

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"cczjVideo/app/model"
)

// Opt-in public-network test: isolated database, production importer, provider,
// source-scoped HTTP, catalog storage, text cache and image-content validation.
func TestReadingSourceCatalogLive(t *testing.T) {
	if os.Getenv("CCZJ_LIVE_READING") != "1" {
		t.Skip("set CCZJ_LIVE_READING=1 for real upstream requests")
	}
	s := setupLibraryTest(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var evidence []map[string]any
	for _, kind := range []model.SourceKind{model.SourceKindNovel, model.SourceKindComic} {
		preview, err := s.PreviewSources(ctx, kind, "", "")
		if err != nil {
			t.Fatal(err)
		}
		for _, candidate := range preview.Candidates {
			if !candidate.Compatible {
				continue
			}
			t.Run(candidate.Name, func(t *testing.T) {
				checked, err := s.CheckSourceCandidate(ctx, preview.ID, candidate.ID, "")
				if err != nil {
					t.Error(err)
					evidence = append(evidence, map[string]any{"name": candidate.Name, "error": err.Error()})
					return
				}
				record := map[string]any{"id": candidate.ID, "name": candidate.Name, "project": candidate.Project, "origin": candidate.Origin, "quality": checked.Quality}
				evidence = append(evidence, record)
				if checked.Quality == nil || checked.Quality.Status != "healthy" {
					t.Errorf("incomplete live reading flow: %+v", checked.Quality)
					return
				}
				imported, err := s.ImportSourceCandidates(ctx, preview.ID, []string{candidate.ID})
				if err != nil || len(imported) != 1 || imported[0].Status != "imported" {
					t.Errorf("import: %+v %v", imported, err)
					return
				}
				// Use the imported configuration through the real aggregation/storage path,
				// not just the candidate probe's extraction helpers.
				outcome, err := s.SearchPage(ctx, kind, "三国", "")
				if kind == model.SourceKindComic {
					outcome, err = s.SearchPage(ctx, kind, "斗罗", "")
				}
				if err != nil {
					t.Error(err)
					return
				}
				var selected *Item
				for _, result := range outcome.Results {
					if result.SourceKey == imported[0].SourceKey && len(result.Items) > 0 {
						selected = result.Items[0]
					}
				}
				if selected == nil {
					// Each source's own check keyword is authoritative (e.g. Dongman).
					resolved, resolveErr := s.resolve(imported[0].SourceKey, kind)
					if resolveErr != nil {
						t.Error(resolveErr)
						return
					}
					outcome, err = s.SearchPage(ctx, kind, resolved.document.CheckKeyword, "")
					if err != nil {
						t.Error(err)
						return
					}
					for _, result := range outcome.Results {
						if result.SourceKey == imported[0].SourceKey && len(result.Items) > 0 {
							selected = result.Items[0]
						}
					}
				}
				if selected == nil {
					t.Errorf("imported source returned no items: %+v", outcome)
					return
				}
				detail, err := s.RefreshToc(ctx, selected.ID)
				if err != nil {
					t.Error(err)
					return
				}
				record["title"] = detail.Item.Title
				record["chapters"] = len(detail.Chapters)
				var target *Chapter
				for _, chapter := range detail.Chapters {
					if chapter.Available && !chapter.IsVolume {
						target = chapter
						break
					}
				}
				if target == nil {
					t.Error("no readable chapter")
					return
				}
				if kind == model.SourceKindNovel {
					body, err := s.ChapterText(ctx, target.ID)
					if err != nil || len(body.Paragraphs) == 0 {
						t.Errorf("text: %+v %v", body, err)
						return
					}
					record["paragraphs"] = len(body.Paragraphs)
				} else {
					pages, err := s.ChapterPages(ctx, target.ID)
					if err != nil || len(pages.Pages) == 0 {
						t.Errorf("pages: %+v %v", pages, err)
						return
					}
					record["pages"] = len(pages.Pages)
				}
			})
		}
	}
	if path := os.Getenv("CCZJ_LIVE_READING_REPORT"); path != "" {
		raw, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
