package douban

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// 标题匹配打分之前，先把标题折成可比形式：采集源多为简体、豆瓣可能回繁体，
// 全角标点和书名号随手就来；季/部还必须在打分时单独参与，否则「是，大臣」会把
// 第一季和第二季当成同一条候选，谁排在结果第一位就选谁。
//
// 这份归一化只服务于「和豆瓣候选比大小」，和 db 里 global_video.name_norm 的
// 归一化不是同一份，也不可能是：那边一旦改表达式，就得回填整张表的唯一索引。

// normalizeTitle 打分用的标题归一化：删空白与零宽字符、全角转半角、去标点、
// 繁体转简体、转小写。年份后缀由 cleanTitle 负责，季/部标记由 splitSeasonTag 负责。
func normalizeTitle(title string) string {
	s := cleanTitle(title)
	s = foldWidth(s)
	s = foldTraditional(s)
	return strings.ToLower(s)
}

// foldWidth 全角 ASCII（！到～）整体减 0xFEE0 落到半角；空白与零宽字符直接删掉，
// 豆瓣标题里的空格位置并不可靠。
//
// 必须先降半角再判标点：全角「，」只有折成「,」之后才被当成装饰符号丢掉，
// 反过来先判就漏（case 只会命中第一个分支）。
func foldWidth(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0xFF01 && r <= 0xFF5E {
			r -= 0xFEE0
		}
		switch {
		case r == 0x200B || r == 0xFEFF || unicode.IsSpace(r):
			continue
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			// 书名号、间隔号、连字符都是包装，两边同样处理即可对齐。
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// traditionalSimplifiedPairs 这张表由 scripts/gen_t2s_table.py 从 OpenCC/Unihan 派生
// 字典里抽出，只含「单字对单字、且折叠方向永远是繁→简」的条目；需要上下文才能定形
// 的字（著/彥外的 乾、瞭、餘 之类词义替换）留在短语层，我们拿不到，也就不会折错。
//
// 折错的代价只是两侧同时折成一个错字、比较结果不变；漏折的代价是繁简两边对不上，
// 所以这张表宁可全一点。

func foldTraditional(s string) string {
	if !strings.ContainsAny(s, traditionalKeys) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if t, ok := traditionalToSimplified[r]; ok {
			b.WriteRune(t)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

var traditionalToSimplified, traditionalKeys = buildTraditionalMap()

func buildTraditionalMap() (map[rune]rune, string) {
	runes := []rune(traditionalSimplifiedPairs)
	if len(runes)%2 != 0 {
		panic("traditionalSimplifiedPairs 长度必须是偶数（繁简成对）")
	}
	m := make(map[rune]rune, len(runes)/2)
	var keys strings.Builder
	keys.Grow(len(runes))
	for i := 0; i < len(runes); i += 2 {
		from, to := runes[i], runes[i+1]
		if from == to {
			continue // 自映射只是表里的噪声，不值得为它改变结果
		}
		m[from] = to
		keys.WriteRune(from)
	}
	return m, keys.String()
}

// 季/部标记规则，按优先级排列：先认最明确的「第X季」，最后才猜结尾的数字和罗马数字。
//
// group 是季号所在捕获组（1 起，FindStringSubmatchIndex 里占 2*group 和 2*group+1）。
// tail 为真表示标记贴死在标题结尾，切 base 时直接用到季号组起点为止，免得定界用的
// 那个字符（「战狼2」的「狼」）被一起删掉。
type seasonRule struct {
	re    *regexp.Regexp
	group int
	tail  bool
}

var seasonRules = []seasonRule{
	// 第一季 / 第2季 / 第三部 / 第二辑 / 第4卷 / 第五期。刻意不含「集」：
	// 「第5集」是分集标题，不是季号。
	{re: regexp.MustCompile(`第([0-9]{1,3}|[零一二两三四五六七八九十百]+)[季部辑卷期]`), group: 1},
	// Season2 / Part3 / Stage2 / Chapter4（空白已在上一步丢掉）
	{re: regexp.MustCompile(`(?:season|series|stage|part|chapter)([0-9]{1,3}|[ivxl]{1,5})`), group: 1},
	// S02E01：剧集文件名最常见的写法
	{re: regexp.MustCompile(`s0*([1-9][0-9]{0,1})e[0-9]{1,3}`), group: 1},
	// 结尾裸数字：战狼2 对上 战狼第二部（四位数年份已被 cleanTitle 摘掉）
	{re: regexp.MustCompile(`([^0-9]|^)([1-9][0-9]{0,2})$`), group: 2, tail: true},
	// 结尾罗马数字：怪奇物语II（要求两位以上，单个 i/v 多半是片名本身）
	{re: regexp.MustCompile(`([^a-z0-9]|^)([ivxl]{2,5})$`), group: 2, tail: true},
}

// splitSeasonTag 把归一化后的标题拆成「不含季号的片名 + 季号 + 上/下部」。
// season 为 0、part 为空表示这一侧没有可辨认的季/部信息，调用方不能据此判定
// 两侧是不同季——只有两边都明确标了号才有资格判负。
func splitSeasonTag(s string) (base string, season int, part string) {
	if s == "" {
		return "", 0, ""
	}
	// 「××（上）」「×× 上部」在归一化后都只剩一个结尾的「上」。
	if t := strings.TrimSuffix(s, "部"); t != s && endsWithPart(t) {
		s = t
	}
	for _, p := range []string{"上", "下"} {
		if strings.HasSuffix(s, p) {
			part = p
			s = strings.TrimSuffix(s, p)
			break
		}
	}
	for _, rule := range seasonRules {
		at := 2 * rule.group
		m := rule.re.FindStringSubmatchIndex(s)
		if m == nil || at+1 >= len(m) {
			continue
		}
		n, ok := parseSeasonNumber(s[m[at]:m[at+1]])
		if !ok {
			continue
		}
		if rule.tail {
			base = s[:m[at]]
		} else {
			base = s[:m[0]] + s[m[1]:]
		}
		return base, n, part
	}
	return s, 0, part
}

func endsWithPart(s string) bool {
	return strings.HasSuffix(s, "上") || strings.HasSuffix(s, "下")
}

var cnDigits = map[rune]int{
	'零': 0, '一': 1, '两': 2, '二': 2, '三': 3, '四': 4, '五': 5,
	'六': 6, '七': 7, '八': 8, '九': 9,
}

// parseSeasonNumber 接受阿拉伯数字、中文数字和罗马数字，只覆盖季号会用到的范围。
func parseSeasonNumber(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false
	}
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return n, true
	}
	if n, ok := parseChineseNumber(raw); ok {
		return n, true
	}
	return parseRomanNumber(raw)
}

func parseChineseNumber(s string) (int, bool) {
	section, digits := 0, 0
	units := map[rune]int{'十': 10, '百': 100, '千': 1000}
	for _, r := range s {
		if d, ok := cnDigits[r]; ok {
			digits = d
			continue
		}
		u, ok := units[r]
		if !ok {
			return 0, false
		}
		if digits == 0 {
			digits = 1 // 「十一」的十前面没有数字，按 1 个十算
		}
		section += digits * u
		digits = 0
	}
	total := section + digits
	if total <= 0 {
		return 0, false
	}
	return total, true
}

func parseRomanNumber(s string) (int, bool) {
	val := map[rune]int{'i': 1, 'v': 5, 'x': 10, 'l': 50}
	runes := []rune(strings.ToLower(s))
	if len(runes) == 0 {
		return 0, false
	}
	total := 0
	for i, r := range runes {
		v, ok := val[r]
		if !ok {
			return 0, false
		}
		if i+1 < len(runes) {
			if next, ok := val[runes[i+1]]; ok && next > v {
				total -= v
				continue
			}
		}
		total += v
	}
	if total <= 0 || total > 200 {
		return 0, false
	}
	return total, true
}

// seasonConflictScore 是季/部冲突的固定返回值：负到任何旁证都加不回来。
//
// 冲突不能用「扣分」表达。同一部剧的各季在豆瓣是不同 subject，片名却完全相同，
// 导演和演员也常常相同，于是冲突候选的原始分是 90 + 30 + 每个导演 20 + 演员 15：
// 扣 100 之后仍能靠两个同名导演翻成正数、越过接受阈值。硬否决只能靠短路返回实现。
const seasonConflictScore = -1000

// seasonMatchDelta 把「是不是同一季」换算成分值。季号一致是最强的正向证据之一；
// 两侧都明确标了不同季号则是「这是另一部片」的确定证据。
//
// 只在两边都有可辨认的号时才下判断，单侧未知一律返回 (0,false)：不知道不等于冲突。
// conflict 为真时调用方必须短路，此时 delta 的数值没有意义。
func seasonMatchDelta(metaSeason int, metaPart string, candSeason int, candPart string) (delta int, conflict bool) {
	if metaSeason > 0 && candSeason > 0 {
		if metaSeason == candSeason {
			return 25, false
		}
		return seasonConflictScore, true
	}
	if metaPart != "" && candPart != "" {
		if metaPart == candPart {
			return 25, false
		}
		return seasonConflictScore, true
	}
	return 0, false
}

// titleMatchScore 只比片名，季/部由 seasonMatchDelta 单独参与：完全同名最强，
// 摘掉季号后片名一致次之，一方包含另一方最弱。
//
// 返回 0 表示片名压根没对上，调用方据此直接判零分——年份、导演、演员都只是旁证，
// 一部「同年同导演但片名无关」的候选不该有任何被接受的可能。
func titleMatchScore(normTitle, normKeyword, candBase, metaBase string) int {
	switch {
	case normTitle == "" || normKeyword == "":
		// 归一化后什么都不剩（纯符号标题），不配拿标题分。
	case normTitle == normKeyword:
		return 100
	case candBase != "" && candBase == metaBase:
		return 90
	case candBase != "" && metaBase != "" &&
		(strings.Contains(candBase, metaBase) || strings.Contains(metaBase, candBase)):
		// 涵盖「万界独尊」↔「万界独尊合集」这类包装名，但也只有这么多分量。
		return 50
	}
	return 0
}
