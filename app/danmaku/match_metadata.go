package danmaku

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var matchHTML = regexp.MustCompile(`<[^>]*>`)
var matchQuality = regexp.MustCompile(`[【\[(（](国语|粤语|普通话|中字|高清|超清|[0-9]{3,4}[pP]|[0-9]{4})[】\])）]`)
var matchSeason = regexp.MustCompile(`第([零〇一二两三四五六七八九十百0-9]+)季`)
var matchOrdinal = regexp.MustCompile(`^(第\s*)?([零〇一二两三四五六七八九十百0-9]+)(\s*[集话話期回]|\s*$|\s*[:：.、 -])`)
var matchEP = regexp.MustCompile(`(?i)^ep(isode)?\.?\s*([0-9]{1,4})(\b|\s)`)
var matchFraction = regexp.MustCompile(`^(第\s*)?[0-9]+[.\-~至][0-9]`)
var matchExtra = regexp.MustCompile(`(?i)预告|花絮|解说|剪辑|reaction|trailer|teaser|特别篇|番外|合集|抢先看`)
var matchEdition = regexp.MustCompile(`(重制版|重制|重剪版|修复版|修復版|完整版|年番|(?:19|20)[0-9]{2}版)$`)
var matchMovie = regexp.MustCompile(`(?i)^(正片|HD|BD|高清|超清|蓝光|1080P|720P)(中字|国语|粤语)?$`)
var matchYear = regexp.MustCompile(`(?:19|20)[0-9]{2}`)

func ordinalNumber(raw string) (int, bool) {
	if n, err := strconv.Atoi(raw); err == nil && n >= 0 && n < 10000 {
		return n, true
	}
	digits := map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}
	total, digit := 0, 0
	for _, r := range raw {
		if n, ok := digits[r]; ok {
			digit = n
		} else if r == '十' || r == '百' {
			unit := 10
			if r == '百' {
				unit = 100
			}
			if digit == 0 {
				digit = 1
			}
			total += digit * unit
			digit = 0
		} else {
			return 0, false
		}
	}
	return total + digit, raw != ""
}
func normalizedTitle(value string) string {
	value = strings.ToLower(matchQuality.ReplaceAllString(matchHTML.ReplaceAllString(norm.NFKC.String(value), ""), ""))
	value = matchSeason.ReplaceAllStringFunc(value, func(v string) string {
		m := matchSeason.FindStringSubmatch(v)
		if n, ok := ordinalNumber(m[1]); ok {
			return "第" + strconv.Itoa(n) + "季"
		}
		return v
	})
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			return -1
		}
		return r
	}, value)
}
func episodeOrdinal(value string) (int, bool) {
	value = strings.TrimSpace(norm.NFKC.String(value))
	if matchExtra.MatchString(value) || matchFraction.MatchString(value) {
		return 0, false
	}
	if m := matchOrdinal.FindStringSubmatch(value); len(m) > 2 {
		return ordinalNumber(m[2])
	}
	if m := matchEP.FindStringSubmatch(value); len(m) > 2 {
		return ordinalNumber(m[2])
	}
	return 0, false
}
func metadataYear(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) == 10 || len(raw) == 13 {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if len(raw) == 13 {
				n /= 1000
			}
			year := time.Unix(n, 0).UTC().Year()
			if year >= 1900 && year <= 2100 {
				return strconv.Itoa(year)
			}
		}
	}
	return matchYear.FindString(raw)
}
func mediaKind(raw string) string {
	raw = strings.ToLower(raw)
	switch {
	case strings.Contains(raw, "动漫") || strings.Contains(raw, "动画") || raw == "番剧" || raw == "国创" || raw == "media_bangumi" || raw == "anime":
		return "animation"
	case strings.Contains(raw, "电影") || raw == "movie" || raw == "film":
		return "movie"
	case strings.Contains(raw, "电视剧") || strings.Contains(raw, "连续剧") || strings.Contains(raw, "剧集") || raw == "tv":
		return "tv"
	case raw == "国产剧" || raw == "大陆剧" || raw == "内地剧" || raw == "港台剧" || raw == "香港剧" || raw == "台湾剧" || raw == "欧美剧" || raw == "日剧" || raw == "韩剧" || raw == "日本剧" || raw == "韩国剧" || raw == "泰剧" || raw == "海外剧":
		return "tv"
	case strings.Contains(raw, "纪录片") || raw == "documentary":
		return "documentary"
	case strings.Contains(raw, "综艺") || raw == "variety":
		return "variety"
	}
	return ""
}
func matchingWork(rows []Candidate, request MatchRequest) *Candidate {
	bestScore := -1
	var best *Candidate
	for _, row := range rows {
		score := workMatchScore(row, request)
		if score < 0 {
			continue
		}
		if score > bestScore || (score == bestScore && best != nil && row.Provider+":"+row.Ref < best.Provider+":"+best.Ref) {
			copy := row
			best = &copy
			bestScore = score
		}
	}
	return best
}

// Similarity discovers the work; metadata only orders usable candidates. Short
// names remain exact, and explicit conflicting seasons are different works.
func workTitleSimilarity(local, remote string) int {
	a, b := normalizedTitle(local), normalizedTitle(remote)
	a, b = matchEdition.ReplaceAllString(a, ""), matchEdition.ReplaceAllString(b, "")
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1000
	}
	seasonA, seasonB := matchSeason.FindString(a), matchSeason.FindString(b)
	if seasonA != "" && seasonB != "" && seasonA != seasonB {
		return 0
	}
	x, y := []rune(a), []rune(b)
	if min(len(x), len(y)) <= 3 || min(len(x), len(y))*1000/max(len(x), len(y)) < 800 {
		return 0
	}
	previous := make([]int, len(y)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, char := range x {
		diagonal := previous[0]
		previous[0] = i + 1
		for j, other := range y {
			cost := 1
			if char == other {
				cost = 0
			}
			old := previous[j+1]
			previous[j+1] = min(previous[j+1]+1, previous[j]+1, diagonal+cost)
			diagonal = old
		}
	}
	return (max(len(x), len(y)) - previous[len(y)]) * 1000 / max(len(x), len(y))
}

func workMatchScore(row Candidate, request MatchRequest) int {
	similarity := workTitleSimilarity(request.Title, row.Title)
	if row.Kind == "video" || matchExtra.MatchString(row.Title) || similarity < 800 {
		return -1
	}
	score := similarity
	if year := metadataYear(request.Year); year != "" && year == metadataYear(row.Year) {
		score += 20
	}
	if kind := mediaKind(request.Kind); kind != "" && kind == mediaKind(row.Kind) {
		score += 10
	}
	return score
}

func matchingEpisode(rows []Episode, request MatchRequest) *Episode {
	n, numbered := episodeOrdinal(request.EpisodeLabel)
	movie := !numbered && matchMovie.MatchString(request.EpisodeLabel)
	if !numbered && !movie {
		return nil
	}
	var best *Episode
	bestScore := int64(-1)
	for _, row := range rows {
		if matchExtra.MatchString(row.Title) {
			continue
		}
		label := row.Number
		if label == "" {
			label = row.Title
		}
		remote, ok := episodeOrdinal(label)
		if movie {
			if len(rows) != 1 {
				continue
			}
		} else if !ok || n != remote {
			continue
		}
		score := int64(0)
		if request.DurationMS > 0 && row.DurationMS > 0 {
			difference := row.DurationMS - request.DurationMS
			if difference < 0 {
				difference = -difference
			}
			score = 1000 - difference*1000/max(row.DurationMS, request.DurationMS)
		}
		if score > bestScore || (score == bestScore && best != nil && row.Provider+":"+row.EpisodeID < best.Provider+":"+best.EpisodeID) {
			copy := row
			best = &copy
			bestScore = score
		}
	}
	return best
}
