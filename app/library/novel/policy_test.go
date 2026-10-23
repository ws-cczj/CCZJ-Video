package novel

import (
	"cczjVideo/app/library/matching"
	"testing"
)

func TestNovelAlignmentConservativeAcrossVolumesAndSplitChapters(t *testing.T) {
	for _, tc := range []struct {
		name             string
		left, right      []matching.Chapter
		identity, status string
	}{
		{"chinese numeral", []matching.Chapter{{ID: 1, Title: "第一章 起风", Available: true}}, []matching.Chapter{{ID: 2, Title: "第1章 起风", Available: true}}, "verified", "matched"},
		{"reset volume", []matching.Chapter{{ID: 1, Title: "第一章 起风", Volume: "第二卷", Available: true}}, []matching.Chapter{{ID: 2, Title: "第1章 起风", Volume: "第一卷", Available: true}}, "verified", "ambiguous"},
		{"same volume", []matching.Chapter{{ID: 1, Title: "第一章 起风", Volume: "第二卷", Available: true}}, []matching.Chapter{{ID: 2, Title: "第1章 起风", Volume: "第2卷", Available: true}}, "verified", "matched"},
		{"split chapter", []matching.Chapter{{ID: 1, Title: "第2章 落雨", Available: true}}, []matching.Chapter{{ID: 2, Title: "第二章 落雨（上）", Available: true}, {ID: 3, Title: "第二章 落雨（下）", Available: true}}, "verified", "ambiguous"},
		{"unverified author", []matching.Chapter{{ID: 1, Title: "第1章 起风", Available: true}}, []matching.Chapter{{ID: 2, Title: "第1章 起风", Available: true}}, "provisional", "ambiguous"},
		{"different work", []matching.Chapter{{ID: 1, Title: "第1章 起风", Available: true}}, []matching.Chapter{{ID: 2, Title: "第1章 起风", Available: true}}, "mismatch", "unmatched"},
		{"duplicate source anchors", []matching.Chapter{{ID: 1, Title: "第1章 起风", Available: true}, {ID: 4, Title: "第1章 起风", Available: true}}, []matching.Chapter{{ID: 2, Title: "第1章 起风", Available: true}}, "verified", "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Align(tc.identity, tc.left, tc.right)
			if len(r.Pairs) == 0 || r.Pairs[0].Status != tc.status {
				t.Fatal(r)
			}
		})
	}
}
func TestLowScoreKnownNovelRemainsFirstChoice(t *testing.T) {
	if Pool(matching.SourceEvidence{Score: 10, KnownWork: true}) != 0 {
		t.Fatal("lost exact work coverage")
	}
	if Pool(matching.SourceEvidence{Score: 99, Confidence: "insufficient", Fresh: true, Readable: true}) != 3 {
		t.Fatal("promoted weak evidence")
	}
}
