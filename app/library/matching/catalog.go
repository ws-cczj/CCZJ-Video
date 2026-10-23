// Package matching supplies bounded catalog indexing; each reading mode owns its
// own parser, routing policy and interpretation of chapter/episode evidence.
package matching

import (
	"strconv"

	"cczjVideo/app/library/quality"
)

type Chapter struct {
	ID                  int
	Title, Volume       string
	IsVolume, Available bool
}
type Unit struct {
	Chapter
	Number, Label, Part, Class string
}
type Pair struct {
	ChapterID int    `json:"chapterId"`
	Status    string `json:"status"`
	TargetIDs []int  `json:"targetIds"`
	Evidence  string `json:"evidence"`
}
type Result struct {
	Algorithm string `json:"algorithm"`
	Kind      string `json:"kind"`
	Identity  string `json:"identity"`
	Pairs     []Pair `json:"pairs"`
	Matched   int    `json:"matched"`
	Ambiguous int    `json:"ambiguous"`
	Unmatched int    `json:"unmatched"`
}

func Normalize(s string) string { return quality.Normalize(s) }

func Number(s string) string {
	if n, err := strconv.ParseFloat(s, 64); err == nil {
		return strconv.FormatFloat(n, 'f', -1, 64)
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, part, digit := 0, 0, 0
	for _, r := range s {
		if n, ok := digits[r]; ok {
			digit = n
			continue
		}
		m := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}[r]
		if m == 0 {
			return ""
		}
		if m == 10000 {
			total += (part + digit) * m
			part, digit = 0, 0
		} else {
			if digit == 0 {
				digit = 1
			}
			part += digit * m
			digit = 0
		}
	}
	if s == "" {
		return ""
	}
	return strconv.Itoa(total + part + digit)
}

func Identity(titleA, authorA, titleB, authorB string) string {
	if Normalize(titleA) == "" || Normalize(titleA) != Normalize(titleB) {
		return "mismatch"
	}
	return quality.MatchAuthors([]string{authorA}, authorB).Status
}

// Align is expected O(A+B) with a fixed candidate ceiling. Number-only matches
// and split/merged units are review candidates, never automatic progress maps.
func Align(kind, identity string, left, right []Unit) Result {
	r := Result{Kind: kind, Identity: identity, Pairs: []Pair{}}
	exact, labels, numbers := map[string][]int{}, map[string][]int{}, map[string][]int{}
	leftCounts := map[string]int{}
	add := func(m map[string][]int, key string, id int) {
		if len(m[key]) < 9 {
			m[key] = append(m[key], id)
		}
	}
	key := func(u Unit) string {
		return Normalize(u.Volume) + "\x00" + u.Class + "\x00" + u.Number + "\x00" + u.Label + "\x00" + u.Part
	}
	for _, u := range left {
		if u.Available && !u.IsVolume {
			leftCounts[key(u)]++
		}
	}
	for _, u := range right {
		if !u.Available || u.IsVolume {
			continue
		}
		if u.Label != "" {
			add(exact, key(u), u.ID)
			add(labels, u.Class+"\x00"+u.Label, u.ID)
		}
		if u.Number != "" {
			add(numbers, Normalize(u.Volume)+"\x00"+u.Class+"\x00"+u.Number, u.ID)
		}
	}
	for _, u := range left {
		if u.IsVolume {
			continue
		}
		p := Pair{ChapterID: u.ID, Status: "unmatched", TargetIDs: []int{}, Evidence: "没有可确认的对应章节"}
		if identity != "mismatch" && u.Available {
			hits := exact[key(u)]
			if len(hits) == 1 && leftCounts[key(u)] == 1 && identity == "verified" && u.Label != "" {
				p.Status = "matched"
				p.TargetIDs = append(p.TargetIDs, hits...)
				p.Evidence = "同作品作者、卷、编号与标题吻合"
			} else {
				if len(hits) == 0 && u.Label != "" {
					hits = labels[u.Class+"\x00"+u.Label]
				}
				if len(hits) == 0 && u.Number != "" {
					hits = numbers[Normalize(u.Volume)+"\x00"+u.Class+"\x00"+u.Number]
				}
				if len(hits) > 0 {
					p.Status = "ambiguous"
					p.TargetIDs = append(p.TargetIDs, hits[:min(len(hits), 8)]...)
					p.Evidence = "仅有候选：需核对身份、分卷、拆合章或版本"
				}
			}
		}
		switch p.Status {
		case "matched":
			r.Matched++
		case "ambiguous":
			r.Ambiguous++
		default:
			r.Unmatched++
		}
		r.Pairs = append(r.Pairs, p)
	}
	return r
}

type SourceEvidence struct {
	Score              int
	Confidence, Status string
	Fresh, Readable    bool
	KnownWork          bool
}

// Pool gives exact observed work coverage precedence over the source average.
// It never permanently excludes unmeasured, low-score or specialist sources.
func Pool(e SourceEvidence) int {
	if e.KnownWork {
		return 0
	}
	if !e.Fresh || e.Confidence == "insufficient" || e.Status == "failed" {
		return 3
	}
	if e.Score >= 75 && e.Readable {
		return 1
	}
	if e.Score >= 60 {
		return 2
	}
	return 3
}
