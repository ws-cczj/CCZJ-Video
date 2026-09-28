package collect

import (
	"regexp"
	"strconv"
	"strings"
)

// 集号识别：源站的集表顺序并不可信（新集常排在最前、也有源按热度混排），而观看进度、
// TS 缓存键和下载任务都以 ep_num 认集。用「列表里的第几个」当集号，会让「看到第 24 集」
// 在下次采集后指向另一集，所以能从集名认出真实集号时以集名为准。
var (
	// seasonEpisodeRe 认 S01E05 / S1:E5 / S01-EP05 这类带季的写法，取集号部分。
	seasonEpisodeRe = regexp.MustCompile(`(?i)\bS(\d{1,2})[\s._\-]*E(?:P)?\.?\s*(\d{1,3})\b`)
	// labeledEpisodeRe 认「第X集/话/話/期/章/卷」，数字允许半角、全角和中文数字。
	labeledEpisodeRe = regexp.MustCompile(`第\s*([0-9０-９]{1,4}|[〇零一二两三四五六七八九十百千]{1,8})\s*[集话話期章卷]`)
	// bareEpisodeRe 认集号后置的写法：「05集」「12话」。
	bareEpisodeRe = regexp.MustCompile(`(^|[^\d])(\d{1,3})\s*[集话話期]`)
	// epPrefixRe 认 EP05 / E05；前置必须是非字母数字，免得把 "PRE001" 里的片段读成集号。
	// 尾界只能手工判：Go 的 RE2 不支持 (?![0-9A-Za-z]) 这种先行断言。
	epPrefixRe = regexp.MustCompile(`(?i)(^|[^0-9A-Za-z])EP?\.?\s*(\d{1,3})`)
	// digitsOnlyRe 认整名就是一个数字的情况（不少源把集表命名成 "1"、"02"）。
	digitsOnlyRe = regexp.MustCompile(`^[0-9０-９]{1,3}$`)
)

// episodeNumber 从集名里抠出真实集号，认不出来返回 0 交给调用方回落。
//
// 只认带明确标记的写法：HD中字、1080P、BD国语、TC 这类画质/版本标记里也全是数字，
// 按「取第一个数字」的老办法会把它们读成第 1080 集。
func episodeNumber(name string) int {
	trimmed := strings.TrimSpace(normalizeDigits(name))
	if trimmed == "" {
		return 0
	}
	if m := seasonEpisodeRe.FindStringSubmatch(trimmed); m != nil {
		return toInt(m[2])
	}
	if m := labeledEpisodeRe.FindStringSubmatch(trimmed); m != nil {
		return parseEpisodeNumeral(m[1])
	}
	if m := bareEpisodeRe.FindStringSubmatch(trimmed); m != nil {
		return toInt(m[2])
	}
	if loc := epPrefixRe.FindStringSubmatchIndex(trimmed); loc != nil {
		// 数字后面还跟着字母或数字（"EP1080"、"PRE001"）就不是集号。
		if end := loc[5]; end >= len(trimmed) || !isAlnum(trimmed[end]) {
			return toInt(trimmed[loc[4]:end])
		}
	}
	if digitsOnlyRe.MatchString(trimmed) {
		return toInt(trimmed)
	}
	return 0
}

// normalizeDigits 把全角数字折成半角，其余字符原样留给各条正则。
func normalizeDigits(s string) string {
	if !strings.ContainsAny(s, "０１２３４５６７８９") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '０' && r <= '９' {
			b.WriteRune(rune('0' + (r - '０')))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var chineseDigits = map[rune]int{
	'〇': 0, '零': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4,
	'五': 5, '六': 6, '七': 7, '八': 8, '九': 9,
}

// parseEpisodeNumeral 解析阿拉伯或中文数字。中文只覆盖到三位数（"一百零三"→103），
// 集号没有更大的实际用法。
func parseEpisodeNumeral(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	if !strings.ContainsAny(s, "十百千") {
		total := 0
		for _, r := range s {
			d, ok := chineseDigits[r]
			if !ok {
				return 0
			}
			total = total*10 + d
		}
		return total
	}
	var total, current int
	for _, r := range s {
		switch r {
		case '千', '百', '十':
			place := map[rune]int{'千': 1000, '百': 100, '十': 10}[r]
			if current == 0 {
				current = 1 // "十五" 的十位省略了一
			}
			total += current * place
			current = 0
		default:
			d, ok := chineseDigits[r]
			if !ok {
				return 0
			}
			current = d
		}
	}
	return total + current
}

func toInt(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// isAlnum 只看 ASCII 字母数字：UTF-8 的多字节首字节都 >= 0x80，自然算「不是字母数字」。
func isAlnum(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
