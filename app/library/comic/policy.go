// Package comic owns episode/volume matching and manga search routing.
package comic

import (
	"cczjVideo/app/library/matching"
	"regexp"
	"strings"
	"time"
)

const Mode = "comic"
const SearchDeadline = 4 * time.Second
const SearchBudget = 16 * time.Second
const AuditInterval = 24 * time.Hour
const DeepInterval = 7 * 24 * time.Hour
const SearchBatch = 5

var episodePattern = regexp.MustCompile(`(?i)^\s*(?:第\s*([0-9零〇一二两三四五六七八九十百千万]+(?:\.[0-9]+)?)\s*[话話集]|(?:episode|chapter|ch\.?|ep\.?)\s*([0-9]+(?:\.[0-9]+)?)|([0-9]+(?:\.[0-9]+)?)[.、：:\s])\s*(.*)$`)
var volumePattern = regexp.MustCompile(`(?i)^\s*(?:第\s*([0-9零〇一二两三四五六七八九十百千万]+)\s*卷|vol\.?\s*([0-9]+))\s*(.*)$`)
var partPattern = regexp.MustCompile(`[（(【\[]?(上|中|下|前篇|后篇|後篇)[）)】\]]\s*$`)

func Pool(e matching.SourceEvidence) int { return matching.Pool(e) }
func Parse(c matching.Chapter) matching.Unit {
	u := matching.Unit{Chapter: c, Class: "episode", Label: matching.Normalize(c.Title)}
	if m := volumePattern.FindStringSubmatch(c.Volume); m != nil {
		for _, n := range m[1:3] {
			if n != "" {
				u.Volume = "vol" + matching.Number(n)
				break
			}
		}
	}
	title := strings.Trim(c.Title, " []【】")
	if m := volumePattern.FindStringSubmatch(title); m != nil {
		for _, n := range m[1:3] {
			if n != "" {
				u.Volume = "vol" + matching.Number(n)
				break
			}
		}
		title = m[3]
	}
	for _, special := range []string{"番外", "特典", "预告", "預告", "单行本", "單行本", "重制", "重製", "特别篇", "特別篇"} {
		if strings.Contains(title, special) {
			u.Class = special
			break
		}
	}
	if m := episodePattern.FindStringSubmatch(title); m != nil {
		for _, n := range m[1:4] {
			if n != "" {
				u.Number = matching.Number(n)
				break
			}
		}
		u.Label = matching.Normalize(strings.Trim(m[4], " []【】"))
	}
	if m := partPattern.FindStringSubmatch(title); m != nil {
		u.Part = m[1]
	}
	return u
}
func Align(identity string, a, b []matching.Chapter) matching.Result {
	left, right := make([]matching.Unit, 0, len(a)), make([]matching.Unit, 0, len(b))
	for _, c := range a {
		left = append(left, Parse(c))
	}
	for _, c := range b {
		right = append(right, Parse(c))
	}
	r := matching.Align(Mode, identity, left, right)
	r.Algorithm = "comic-episode.v1"
	return r
}
