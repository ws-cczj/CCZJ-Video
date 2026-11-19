package danmaku

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"cczjVideo/app/textvariant"
	"golang.org/x/text/unicode/norm"
)

var matchHTML = regexp.MustCompile(`<[^>]*>`)
var matchQuality = regexp.MustCompile(`[【\[(（](国语|粤语|普通话|中字|高清|超清|[0-9]{3,4}[pP]|[0-9]{4})[】\])）]`)
var matchSeason = regexp.MustCompile(`第([零〇一二两三四五六七八九十百0-9]+)季`)
var matchXiguaPart = regexp.MustCompile(`第([零〇一二两三四五六七八九十百0-9]+)部$`)
var matchLatinSeason = regexp.MustCompile(`(?i)\b(?:season\s*|s)0*([0-9]{1,3})\b`)
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
	value = matchLatinSeason.ReplaceAllString(value, "第${1}季")
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
	ambiguous := false
	for i := range rows {
		row := &rows[i]
		score := workMatchScore(*row, request)
		if score < 0 {
			continue
		}
		if score > bestScore {
			best = row
			bestScore = score
			ambiguous = false
		} else if row.Ref != best.Ref || row.Provider != best.Provider {
			ambiguous = true
		}
	}
	if ambiguous {
		return nil
	}
	return best
}

// A second directory probe is limited to compatible metadata and a distinct work.
func fallbackWork(rows []Candidate, request MatchRequest, first *Candidate) *Candidate {
	candidates := make([]Candidate, 0, len(rows))
	localSeason := matchSeason.FindString(normalizedTitle(request.Title))
	localYear, localKind := metadataYear(request.Year), mediaKind(request.Kind)
	referenceEdition := edition(request.Title)
	if referenceEdition == "" {
		referenceEdition = edition(first.Title)
	}
	for _, row := range rows {
		if row.Ref == first.Ref || matchSeason.FindString(normalizedTitle(row.Title)) != localSeason {
			continue
		}
		if value := edition(row.Title); referenceEdition != "" && value != referenceEdition {
			continue
		}
		year, kind := metadataYear(row.Year), mediaKind(row.Kind)
		if (localYear != "" && year != "" && year != localYear) || (localKind != "" && kind != "" && kind != localKind) {
			continue
		}
		if candidateTitleSimilarity(row, request.Title) < 1000 && (localYear == "" || localYear != year || localKind == "" || localKind != kind) {
			continue
		}
		candidates = append(candidates, row)
	}
	return matchingWork(candidates, request)
}

func edition(title string) string {
	value := strings.TrimSuffix(matchEdition.FindString(normalizedTitle(title)), "版")
	if value == "修復" {
		return "修复"
	}
	return value
}

// Similarity discovers the work; metadata only orders usable candidates. Short
// names remain exact, and explicit conflicting seasons are different works.
func workTitleSimilarity(local, remote string) int {
	a, b := normalizedTitle(local), normalizedTitle(remote)
	if localEdition, remoteEdition := edition(a), edition(b); localEdition != "" && remoteEdition != "" && localEdition != remoteEdition {
		return 0
	}
	a, b = matchEdition.ReplaceAllString(a, ""), matchEdition.ReplaceAllString(b, "")
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1000
	}
	seasonA, seasonB := matchSeason.FindString(a), matchSeason.FindString(b)
	// An unqualified title cannot identify an explicitly later season.
	if seasonA == "" {
		seasonA = "第1季"
	}
	if seasonB == "" {
		seasonB = "第1季"
	}
	if seasonA != seasonB {
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

func candidateTitleSimilarity(row Candidate, title string) int {
	local, remote := title, row.Title
	switch row.Provider {
	case bahamutProviderID:
		local, remote = textvariant.Simplified(local), textvariant.Simplified(remote)
	case xiguaProviderID:
		local, remote = xiguaPartTitle(local), xiguaPartTitle(remote)
	}
	return workTitleSimilarity(local, remote)
}

func workMatchScore(row Candidate, request MatchRequest) int {
	similarity := candidateTitleSimilarity(row, request.Title)
	if row.Kind == "video" || matchExtra.MatchString(row.Title) || similarity < 800 {
		return -1
	}
	score := similarity
	if localEdition := edition(request.Title); localEdition != "" && localEdition == edition(row.Title) {
		score += 30
	}
	if year := metadataYear(request.Year); year != "" && year == metadataYear(row.Year) {
		score += 20
	}
	if kind := mediaKind(request.Kind); kind != "" && kind == mediaKind(row.Kind) {
		score += 10
	}
	return score
}

func xiguaPartTitle(title string) string {
	title = strings.TrimSpace(norm.NFKC.String(title))
	match := matchXiguaPart.FindStringSubmatchIndex(title)
	if match == nil {
		return title
	}
	part, ok := ordinalNumber(title[match[2]:match[3]])
	if !ok {
		return title
	}
	if part == 1 {
		return title[:match[0]]
	}
	return title[:match[0]] + "第" + strconv.Itoa(part) + "季"
}

func matchingEpisode(rows []Episode, request MatchRequest) *Episode {
	n, numbered := episodeOrdinal(request.EpisodeLabel)
	movie := !numbered && matchMovie.MatchString(request.EpisodeLabel)
	if (!numbered && !movie) || (movie && len(rows) != 1) {
		return nil
	}
	var best *Episode
	bestScore := int64(-1)
	ambiguous := false
	for i := range rows {
		row := &rows[i]
		score, reason := episodeScore(*row, request, n, movie)
		if reason != "" {
			continue
		}
		if score > bestScore {
			best = row
			bestScore = score
			ambiguous = false
		} else if score == bestScore && (row.EpisodeID != best.EpisodeID || row.AID != best.AID || row.Provider != best.Provider) {
			ambiguous = true
		}
	}
	if ambiguous {
		return nil
	}
	return best
}

func episodeScore(row Episode, request MatchRequest, number int, movie bool) (int64, string) {
	if !movie {
		label := strings.TrimSpace(row.Number)
		if label == "" {
			label = row.Title
		}
		remote, ok := episodeOrdinal(label)
		if !ok {
			return 0, "EPISODE_NUMBER_UNKNOWN"
		}
		title := strings.TrimSpace(norm.NFKC.String(row.Title))
		if strings.HasPrefix(title, "第") || matchEP.MatchString(title) {
			if declared, ok := episodeOrdinal(title); ok && declared != remote && (declared == number || remote == number) {
				return 0, "AMBIGUOUS_EPISODE"
			}
		}
		if remote != number {
			return 0, "EPISODE_NUMBER_MISMATCH"
		}
	}
	if matchExtra.MatchString(row.Title) {
		return 0, "EPISODE_SPECIAL_REJECTED"
	}
	localEdition, remoteEdition := edition(request.Title), edition(row.Title)
	if localEdition != "" && remoteEdition != "" && localEdition != remoteEdition {
		return 0, "EPISODE_VERSION_MISMATCH"
	}
	score := int64(0)
	if request.DurationMS > 0 && row.DurationMS > 0 {
		longer, shorter := max(row.DurationMS, request.DurationMS), min(row.DurationMS, request.DurationMS)
		difference := longer - shorter
		if difference > int64((2*time.Minute)/time.Millisecond) && difference*5 > longer {
			return 0, "EPISODE_DURATION_MISMATCH"
		}
		// Round duration evidence to whole percentage points so tiny differences
		// cannot decide between different pools with the same episode number.
		score = 100 - (difference*100+longer/2)/longer
	}
	if localEdition != "" && localEdition == remoteEdition {
		score += 10
	}
	return score, ""
}
