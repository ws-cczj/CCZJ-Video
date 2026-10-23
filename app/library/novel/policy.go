// Package novel owns text-chapter matching and novel search routing.
package novel

import (
	"cczjVideo/app/library/matching"
	"regexp"
	"strings"
	"time"
)

const Mode = "novel"
const SearchDeadline = 4 * time.Second
const SearchBudget = 16 * time.Second
const AuditInterval = 24 * time.Hour
const DeepInterval = 7 * 24 * time.Hour
const SearchBatch = 5

var chapterPattern = regexp.MustCompile(`(?i)^\s*(?:第\s*([0-9零〇一二两三四五六七八九十百千万]+)\s*[章回节節]|chapter\s*([0-9]+)|([0-9]+)[.、：:\s])\s*(.*)$`)
var partPattern = regexp.MustCompile(`[（(【\[]?(上|中|下)[）)】\]]\s*$`)
var volumePattern = regexp.MustCompile(`^\s*第\s*([0-9零〇一二两三四五六七八九十百千万]+)\s*卷`)

func Pool(e matching.SourceEvidence) int { return matching.Pool(e) }
func Parse(c matching.Chapter) matching.Unit {
	u := matching.Unit{Chapter: c, Class: "chapter", Label: matching.Normalize(c.Title)}
	if m := volumePattern.FindStringSubmatch(c.Volume); m != nil {
		u.Volume = "vol" + matching.Number(m[1])
	}
	title := strings.Trim(c.Title, " []【】")
	for _, special := range []string{"番外", "序章", "楔子", "后记", "後記", "完本", "完结", "感言", "请假", "通知"} {
		if strings.Contains(title, special) {
			u.Class = "extra"
			break
		}
	}
	if m := chapterPattern.FindStringSubmatch(title); m != nil {
		for _, n := range m[1:4] {
			if n != "" {
				u.Number = matching.Number(n)
				break
			}
		}
		u.Label = matching.Normalize(m[4])
	}
	if m := partPattern.FindStringSubmatch(title); m != nil {
		u.Part = m[1]
		u.Label = matching.Normalize(partPattern.ReplaceAllString(u.Label, ""))
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
	r.Algorithm = "novel-chapter.v1"
	return r
}
