package quality

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"cczjVideo/app/provider"
)

type Issue struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Evidence string `json:"evidence"`
}
type Identity struct {
	Status string `json:"status"` // verified, provisional, mismatch, absent, ambiguous
	Reason string `json:"reason"`
}

// IdentityAlgorithm versions the author-credit rules independently of the
// compatible corpus JSON schema (Version).
const IdentityAlgorithm = "reading-author-credit.v2"

func Match(w Work, item *provider.Item) Identity {
	if item == nil {
		return Identity{"absent", "没有结果"}
	}
	title := Normalize(item.Title)
	same := title == Normalize(w.Title)
	for _, a := range w.Aliases {
		same = same || title == Normalize(a)
	}
	for _, localized := range w.SearchTitles {
		same = same || title == Normalize(localized)
	}
	if !same {
		return Identity{"mismatch", "标题或版本不符"}
	}
	// A canonical first-party work URL is an identity anchor even when the
	// official endpoint does not return an author field.
	for _, r := range w.References {
		if r.Role != "ranking" && canonicalURL(r.URL) == canonicalURL(item.URL) {
			return Identity{"verified", "标题与第一方作品地址吻合"}
		}
	}
	return MatchAuthors(w.Authors, item.Author)
}

// MatchAuthors compares explicit author credits. It removes only recognized
// nationality/dynasty labels and role suffixes, never arbitrary bracket text,
// translator credits, name substrings or guessed spelling aliases.
func MatchAuthors(expected []string, actual string) Identity {
	hasExpected := false
	for _, credit := range expected {
		hasExpected = hasExpected || strings.TrimSpace(credit) != ""
	}
	if strings.TrimSpace(actual) == "" || !hasExpected {
		return Identity{"provisional", "标题吻合，缺少可核对的作者"}
	}
	if utf8.RuneCountInString(actual) > 100 {
		return Identity{"provisional", "作者字段异常长，身份待核对"}
	}
	names := authorNames(actual)
	for _, credit := range expected {
		if utf8.RuneCountInString(credit) > 100 {
			return Identity{"provisional", "作者字段异常长，身份待核对"}
		}
		for name := range authorNames(credit) {
			if names[name] {
				return Identity{"verified", "标题与作者吻合"}
			}
		}
	}
	return Identity{"mismatch", "同名但作者不符"}
}

var authorLocale = regexp.MustCompile(`^[\[（(【]\s*(?:中|英|美|日|法|德|俄|苏|澳|加|意|西|奥|挪|丹|捷|明|清|元|宋|唐|汉|古希腊|古罗马)(?:国)?\s*[\]）)】]\s*`)

func authorNames(credit string) map[string]bool {
	out := map[string]bool{}
	for _, author := range strings.FieldsFunc(credit, func(r rune) bool { return strings.ContainsRune("/／、,，;；&\n", r) }) {
		author = strings.TrimSpace(author)
		if strings.HasPrefix(author, "译者") || strings.HasPrefix(author, "翻译") {
			continue
		}
		for _, prefix := range []string{"作者：", "作者:", "原作：", "原作:", "作画：", "作画:"} {
			author = strings.TrimSpace(strings.TrimPrefix(author, prefix))
		}
		author = authorLocale.ReplaceAllString(author, "")
		parts := strings.Fields(author)
		if len(parts) > 1 {
			role := parts[len(parts)-1]
			if role == "译" || role == "译著" || role == "编译" {
				continue
			}
			if role == "著" || role == "原著" || role == "编著" {
				author = strings.Join(parts[:len(parts)-1], " ")
			}
		}
		if name := Normalize(author); name != "" {
			out[name] = true
		}
	}
	return out
}
func canonicalURL(s string) string { return strings.TrimRight(strings.TrimSpace(s), "/") }

var numbered = regexp.MustCompile(`^\s*(?:第\s*([0-9零〇一二两三四五六七八九十百千万]+)\s*([章话話回节節集])|(?:\[?第)?([0-9]+(?:\.[0-9]+)?)\s*(?:话|話|章|集|回|\]|[.、:：\s]))`)
var chineseDigits = map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}

func chapterNumber(s string) (float64, string, bool) {
	s = strings.TrimLeft(s, "[【 ")
	m := numbered.FindStringSubmatch(s)
	if m == nil {
		return 0, "", false
	}
	n := m[1]
	unit := "chapter"
	if m[2] == "话" || m[2] == "話" || m[2] == "集" {
		unit = "episode"
	}
	if n == "" {
		n = m[3]
		unit = "unknown"
	}
	if v, e := strconv.ParseFloat(n, 64); e == nil {
		return v, unit, true
	}
	total, part, digit := 0, 0, 0
	for _, r := range n {
		if v, ok := chineseDigits[r]; ok {
			digit = v
			continue
		}
		mult := map[rune]int{'十': 10, '百': 100, '千': 1000, '万': 10000}[r]
		if mult == 0 {
			return 0, "", false
		}
		if mult == 10000 {
			total += (part + digit) * mult
			part, digit = 0, 0
		} else {
			if digit == 0 {
				digit = 1
			}
			part += digit * mult
			digit = 0
		}
	}
	return float64(total + part + digit), unit, true
}
func chapterLabel(s string) string {
	return Normalize(numbered.ReplaceAllString(strings.TrimLeft(s, "[【 "), ""))
}

type CatalogEvidence struct {
	Entries             int     `json:"entries"`
	Numbered            int     `json:"numbered"`
	AnchorMatches       int     `json:"anchorMatches"`
	ReferenceComparable bool    `json:"referenceComparable"`
	CountRelation       string  `json:"countRelation"` // unknown, exact, less, more
	Issues              []Issue `json:"issues"`
}

func AnalyzeCatalog(w Work, chapters []*provider.Chapter, now time.Time) CatalogEvidence {
	e := CatalogEvidence{CountRelation: "unknown", Issues: []Issue{}}
	seen := map[string]bool{}
	type anchorLocation struct {
		position int
		unit     string
		count    int
	}
	anchorIndex := map[string]anchorLocation{}
	anchorKey := func(number float64, label, volume string) string {
		return strconv.FormatFloat(number, 'g', -1, 64) + "\x00" + label + "\x00" + volume
	}
	rising, falling, explicitVolumeFalls := 0, 0, 0
	var prev float64
	prevVol := ""
	prevUnit := ""
	havePrev := false
	for position, c := range chapters {
		if c == nil || c.IsVolume {
			havePrev = false
			continue
		}
		e.Entries++
		key := c.URL
		if key == "" {
			key = c.RemoteID
		}
		if key == "" {
			key = c.Volume + "\x00" + Normalize(c.Title)
		}
		if seen[key] {
			e.Issues = append(e.Issues, Issue{"duplicate_chapter", "error", c.Title})
		}
		seen[key] = true
		n, unit, ok := chapterNumber(c.Title)
		if !ok {
			// An unparsed entry can be a volume boundary or a differently named
			// chapter. Do not invent adjacency across it and report false gaps.
			havePrev = false
			continue
		}
		e.Numbered++
		label, volume := chapterLabel(c.Title), Normalize(c.Volume)
		if label != "" {
			keys := []string{anchorKey(n, label, "")}
			if volume != "" {
				keys = append(keys, anchorKey(n, label, volume))
			}
			for _, key := range keys {
				location := anchorIndex[key]
				location.position = position
				location.unit = unit
				location.count++
				anchorIndex[key] = location
			}
		}
		if havePrev && prevVol == c.Volume && prevUnit == unit {
			if n < prev {
				falling++
				if c.Volume != "" {
					explicitVolumeFalls++
				}
			}
			if n > prev {
				rising++
			}
			if n-prev > 1 {
				e.Issues = append(e.Issues, Issue{"numbering_gap_review", "warning", fmt.Sprintf("相邻编号 %g → %g；需核对拆章、番外与源站目录", prev, n)})
			}
		}
		prev, prevVol, prevUnit, havePrev = n, c.Volume, unit, true
	}
	if falling > 0 {
		code, severity := "numbering_decrease_review", "warning"
		if rising == 0 {
			code, severity = "reversed_chapter_order", "error"
		} else if explicitVolumeFalls > 0 {
			code, severity = "mixed_chapter_order", "error"
		}
		e.Issues = append(e.Issues, Issue{code, severity, fmt.Sprintf("升序相邻 %d，倒序相邻 %d；无卷信息时混合下降仅待核查", rising, falling)})
	}
	cat := w.Catalog
	if cat == nil {
		return e
	}
	d, err := time.Parse(time.DateOnly, cat.VerifiedAt)
	if err != nil || now.Before(d) || now.Sub(d) > 14*24*time.Hour && cat.Status != "completed" {
		e.Issues = append(e.Issues, Issue{"stale_reference", "info", "连载目录快照已过期，未比较总数"})
		return e
	}
	anchorPositions := []int{}
	unitMatches := 0
	for _, a := range cat.Anchors {
		an, _, numbered := chapterNumber(a.Title)
		label := chapterLabel(a.Title)
		if numbered && an != a.Number || label == "" {
			continue
		}
		location, ok := anchorIndex[anchorKey(a.Number, label, Normalize(a.Volume))]
		if !ok || location.count != 1 {
			continue
		}
		e.AnchorMatches++
		anchorPositions = append(anchorPositions, location.position)
		if location.unit == cat.Unit {
			unitMatches++
		}
	}
	// Raw counts alone are not comparable across editions, split chapters or
	// public/full catalogs. Require multiple titled, numbered edition anchors.
	ordered := true
	for i := 1; i < len(anchorPositions); i++ {
		if anchorPositions[i] <= anchorPositions[i-1] {
			ordered = false
		}
	}
	e.ReferenceComparable = e.AnchorMatches >= 2 && unitMatches >= 2 && ordered && cat.Count > 0 && e.Numbered == e.Entries
	if e.ReferenceComparable {
		e.CountRelation = "exact"
		if e.Entries < cat.Count {
			e.CountRelation = "less"
		}
		if e.Entries > cat.Count {
			e.CountRelation = "more"
		}
		if e.CountRelation == "less" {
			e.Issues = append(e.Issues, Issue{"catalog_shorter_than_reference", "warning", fmt.Sprintf("目录 %d 项，%s参考 %d 项；检查漏页、拆章与公开范围", e.Entries, cat.Scope, cat.Count)})
		}
	}
	return e
}

type TextEvidence struct {
	Runes       int     `json:"runes"`
	Paragraphs  int     `json:"paragraphs"`
	Fingerprint string  `json:"fingerprint"`
	Issues      []Issue `json:"issues"`
}

var promoURL = regexp.MustCompile(`(?i)(https?://|www\.|[a-z0-9-]+\.(?:com|net|org|cn|cc|xyz)(?:/|\b))`)

func AnalyzeText(body *provider.TextContent) TextEvidence {
	e := TextEvidence{Issues: []Issue{}}
	if body == nil {
		return e
	}
	t := strings.Join(body.Paragraphs, "\n")
	e.Runes = utf8.RuneCountInString(t)
	e.Paragraphs = len(body.Paragraphs)
	hash := sha256.Sum256([]byte(strings.Join(strings.Fields(t), "")))
	e.Fingerprint = hex.EncodeToString(hash[:])
	if body.Garbled || body.FontObfuscated || body.Truncated {
		e.Issues = append(e.Issues, Issue{"damaged_text", "error", fmt.Sprintf("乱码=%t 字体混淆=%t 截断=%t", body.Garbled, body.FontObfuscated, body.Truncated)})
	}
	if e.Runes == 0 {
		e.Issues = append(e.Issues, Issue{"empty_text", "error", "正文为空"})
	}
	for _, line := range strings.Split(t, "\n") {
		line = strings.TrimSpace(line)
		if utf8.RuneCountInString(line) > 160 {
			continue
		}
		compact := Normalize(line)
		if compact == "上一章" || compact == "下一章" || compact == "加入书签" || compact == "返回目录" {
			e.Issues = append(e.Issues, Issue{"navigation_in_text", "warning", line})
			continue
		}
		promo := false
		for _, s := range []string{"最新网址", "最新域名", "备用网址", "下载APP", "下载App", "下载app", "扫码下载", "加入书友群"} {
			promo = promo || strings.Contains(line, s)
		}
		if promo && (promoURL.MatchString(line) || strings.Contains(line, "下载") || strings.Contains(line, "扫码") || strings.Contains(line, "书友群")) {
			e.Issues = append(e.Issues, Issue{"promotion_in_text", "warning", line})
		}
	}
	return e
}
