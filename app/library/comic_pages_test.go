package library

import (
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
	"testing"
)

func TestComicPagesExcludeReviewedPromotionWithoutDroppingLastArtwork(t *testing.T) {
	s := setupLibraryTest(t)
	source := &resolvedSource{source: &model.Source{ApiUrl: "https://www.guazimanhua.com", SourceKey: "comic"}}
	row := &chapterRow{url: "https://www.guazimanhua.com/chapter.php?id=1977722"}
	pages := []*provider.Page{{Index: 0, ImageURL: "https://img.guazicdn.com/th/comics/chapters/26/260501/2_17.webp"}, {Index: 1, ImageURL: "https://img.guazicdn.com/th/comics/chapters/26/260501/2_18.webp"}}
	found := s.proxyPages(pages, row, source)
	if len(found) != 1 || found[0].Index != 0 {
		t.Fatalf("reviewed promotional image remained: %+v", found)
	}
	// Same ordinal in another asset path is normal artwork until reviewed.
	pages[1].ImageURL = "https://img.guazicdn.com/th/comics/chapters/other/2_18.webp"
	if got := s.proxyPages(pages, row, source); len(got) != 2 {
		t.Fatal("dropped unreviewed last page")
	}
	source.source.ApiUrl = "https://another.example"
	pages[1].ImageURL = "https://img.guazicdn.com/th/comics/chapters/26/260501/2_18.webp"
	if got := s.proxyPages(pages, row, source); len(got) != 2 {
		t.Fatal("leaked a site rule into another source")
	}
}
