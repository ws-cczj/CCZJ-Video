package comic

import (
	"cczjVideo/app/library/matching"
	"testing"
)

func TestComicEpisodeMatchingKeepsSpecialsDecimalAndVolumeSemantics(t *testing.T) {
	for _, tc := range []struct{ name, a, b, va, vb, status string }{
		{"decimal", "第12.5话 梦", "Episode 12.5 梦", "", "", "matched"},
		{"decimal not integer", "第12.5话 梦", "第12话 梦", "", "", "ambiguous"},
		{"volume spelling", "第1话 梦", "第2卷 第1话 梦", "第二卷", "", "matched"},
		{"volume reset", "第1话 梦", "第1话 梦", "第1卷", "第2卷", "ambiguous"},
		{"special not regular", "第1话 番外", "第1话 正篇", "", "", "unmatched"},
		{"missing episode title", "第1话", "第1话", "", "", "ambiguous"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Align("verified", []matching.Chapter{{ID: 1, Title: tc.a, Volume: tc.va, Available: true}}, []matching.Chapter{{ID: 2, Title: tc.b, Volume: tc.vb, Available: true}})
			if r.Pairs[0].Status != tc.status {
				t.Fatal(r)
			}
		})
	}
}
