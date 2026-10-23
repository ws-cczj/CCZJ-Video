package main

import (
	"context"
	"os"
	"testing"

	"cczjVideo/app/library"
	"cczjVideo/app/library/quality"
	"cczjVideo/app/model"
)

type searchFixture struct {
	pages []*library.SearchOutcome
	calls int
}

func (f *searchFixture) SearchPage(_ context.Context, _ model.SourceKind, _ string, _ string) (*library.SearchOutcome, error) {
	p := f.pages[min(f.calls, len(f.pages)-1)]
	f.calls++
	return p, nil
}

func TestRoutedVerificationRejectsRecommendationsAndWrongAuthors(t *testing.T) {
	work := quality.Work{Title: "鬼吹灯", Kind: "novel", Authors: []string{"天下霸唱"}}
	for _, tc := range []struct {
		name, title, author, status string
	}{
		{"recommendation", "王锋锋的第9本书", "王锋锋", "absent"},
		{"wrong author", "鬼吹灯", "另一位作者", "absent"},
		{"sequel", "鬼吹灯之牧野诡事", "天下霸唱", "absent"},
		{"missing author", "鬼吹灯", "", "provisional"},
		{"original", "鬼吹灯", "天下霸唱", "verified"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &searchFixture{pages: []*library.SearchOutcome{{Results: []*library.SourceResult{{Items: []*library.Item{{ID: 1, Title: tc.title, Author: tc.author}}}}}}}
			got := verifyRoutedWork(context.Background(), f, work)
			if got.Status != tc.status {
				t.Fatalf("got %s, want %s", got.Status, tc.status)
			}
		})
	}
}

func TestRoutedVerificationFollowsPendingSourcesButKeepsCeiling(t *testing.T) {
	work := quality.Work{Title: "西游记", Kind: "novel", Authors: []string{"吴承恩"}}
	page := &library.SearchOutcome{PendingSources: 20, NextCursor: "next"}
	f := &searchFixture{pages: []*library.SearchOutcome{page}}
	got := verifyRoutedWork(context.Background(), f, work)
	if got.Status != "pending" || f.calls != 3 || got.PendingSources != 20 {
		t.Fatalf("lost incomplete scope: %+v, calls=%d", got, f.calls)
	}
	f = &searchFixture{pages: []*library.SearchOutcome{page, {Results: []*library.SourceResult{{Items: []*library.Item{{ID: 2, Title: "西游记", Author: "吴承恩"}}}}}}}
	got = verifyRoutedWork(context.Background(), f, work)
	if got.Status != "verified" || f.calls != 2 {
		t.Fatalf("did not extend real search: %+v", got)
	}
}

func TestClassicCorpusIsValidAndSeparatesModes(t *testing.T) {
	raw, err := os.ReadFile("../../scripts/fixtures/reading-classics.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	corpus, err := quality.ParseCorpus(raw)
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, work := range corpus.Works {
		counts[work.Kind]++
	}
	if counts["novel"] != 24 || counts["comic"] != 16 {
		t.Fatalf("unexpected corpus scope: %v", counts)
	}
}
