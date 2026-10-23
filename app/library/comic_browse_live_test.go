package library

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func TestExpandedComicBrowseLive(t *testing.T) {
	if os.Getenv("CCZJ_LIVE_COMIC_BROWSE") != "1" {
		t.Skip("set CCZJ_LIVE_COMIC_BROWSE=1 for upstream browsing/reading")
	}
	s := setupLibraryTest(t)
	evidence := []map[string]any{}
	for _, channel := range []string{"guazi", "dongman"} {
		t.Run(channel, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
			defer cancel()
			page, err := s.BrowseComics(ctx, channel, true)
			if err != nil {
				t.Fatal(err)
			}
			record := map[string]any{"channel": channel, "count": len(page.Items), "source": page.SourceName}
			evidence = append(evidence, record)
			if len(page.Items) < 6 {
				t.Fatalf("too few browse items: %d", len(page.Items))
			}
			book, err := s.RefreshToc(ctx, page.Items[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			record["title"], record["chapters"] = book.Item.Title, len(book.Chapters)
			for _, chapter := range book.Chapters {
				if chapter.IsVolume || !chapter.Available {
					continue
				}
				pages, err := s.ChapterPages(ctx, chapter.ID)
				if err != nil {
					t.Fatal(err)
				}
				if len(pages.Pages) == 0 {
					t.Fatal("chapter returned no image pages")
				}
				record["pages"] = len(pages.Pages)
				return
			}
			t.Fatal("no public chapter")
		})
	}
	if path := os.Getenv("CCZJ_LIVE_COMIC_BROWSE_REPORT"); path != "" {
		raw, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
