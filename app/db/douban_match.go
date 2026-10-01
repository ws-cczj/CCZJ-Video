package db

import (
	"cczjVideo/app/applog"
	"regexp"
	"strings"
	"unicode/utf8"
)

// editDistance 计算两个字符串的编辑距离（Levenshtein）
func editDistance(a, b string) int {
	la, lb := utf8.RuneCountInString(a), utf8.RuneCountInString(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	ra := []rune(a)
	rb := []rune(b)

	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min3(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

// normalizedSimilarity 计算两个「已归一化」名称的相似度 0.0~1.0。
// 归一化由调用方负责（identity.go 的 normalizeTitle），这样身份阶梯里同一对
// 字符串不会被反复归一化；公式仍是 1 - 编辑距离/最大长度。
func normalizedSimilarity(na, nb string) float64 {
	if na == nb {
		return 1.0
	}
	if len(na) == 0 || len(nb) == 0 {
		return 0.0
	}
	maxLen := utf8.RuneCountInString(na)
	if nbLen := utf8.RuneCountInString(nb); nbLen > maxLen {
		maxLen = nbLen
	}
	dist := editDistance(na, nb)
	return 1.0 - float64(dist)/float64(maxLen)
}

// seasonSuffixPattern 匹配季/部/期/卷等后缀模式
// 用于防止“权力的游戏 第一季”和“权力的游戏 第二季”被误判为同一视频
var seasonSuffixPattern = regexp.MustCompile(`第[一二三四五六七八九十百千\d]+[季部期卷集]|season\s*\d+|s\d+|part\s*\d+|[（(]\s*\d+\s*[）)]|[ⅠⅡⅢⅣⅤⅥⅦⅧⅨⅩ]+|[上下][集部篇]?`)

// hasSeasonSuffix 检查两个名称的差异部分是否包含季/部/期等后缀
// 如果 a 和 b 的差异仅在于季/部/期后缀不同，返回 true
func hasSeasonSuffix(a, b string) bool {
	na := normalizeTitle(a)
	nb := normalizeTitle(b)
	if na == nb {
		return false
	}
	// 检查较长的名称中是否包含季/部/期后缀，且较短的名称中不包含
	var longer, shorter string
	if len([]rune(na)) > len([]rune(nb)) {
		longer, shorter = na, nb
	} else {
		longer, shorter = nb, na
	}
	// 如果较长名称有季/部/期后缀但较短名称没有，视为不同视频
	hasLong := seasonSuffixPattern.MatchString(longer)
	hasShort := seasonSuffixPattern.MatchString(shorter)
	if hasLong && !hasShort {
		return true
	}
	// 如果两者都有季/部/期后缀但后缀不同（如"第一季" vs "第二季"），也视为不同视频
	if hasLong && hasShort {
		// 提取后缀进行比较
		lm := seasonSuffixPattern.FindString(longer)
		sm := seasonSuffixPattern.FindString(shorter)
		if lm != "" && sm != "" && lm != sm {
			return true
		}
	}
	return false
}

// metadataMatch 比对视频的关键元数据是否吻合
// 要求年份相同，且导演或演员至少有一个非空交集
func metadataMatch(yearA, directorA, actorA, yearB, directorB, actorB string) bool {
	// 年份必须匹配（如果双方都有年份）
	if yearA != "" && yearB != "" && yearA != yearB {
		return false
	}
	// 导演或演员至少有一个交集
	dirOverlap := hasCommonToken(directorA, directorB)
	actOverlap := hasCommonToken(actorA, actorB)
	return dirOverlap || actOverlap
}

// hasCommonToken 检查两个逗号/空格分隔的字符串是否有共同项
func hasCommonToken(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	split := func(s string) []string {
		s = strings.ReplaceAll(s, ",", "\x00")
		s = strings.ReplaceAll(s, "/", "\x00")
		return strings.Split(s, "\x00")
	}
	tokensA := split(strings.ToLower(a))
	tokensB := split(strings.ToLower(b))
	set := make(map[string]bool, len(tokensA))
	for _, t := range tokensA {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = true
		}
	}
	for _, t := range tokensB {
		t = strings.TrimSpace(t)
		if t != "" && set[t] {
			return true
		}
	}
	return false
}

// GetOrCreateGlobalID 按标题取或建 global_video 记录。阶梯只有一份实现，见 identity.go。
func GetOrCreateGlobalID(vodName string, typeId int64) (int64, error) {
	return GetOrCreateGlobalIDWithMeta(vodName, "", typeId)
}

// GetOrCreateGlobalIDWithMeta 带元数据的身份解析，用于采集入库、豆瓣补全、收藏与历史。
// year 只参与别名档和相似度档的校验；typeId=0 表示「不知道类型」，此时不限类型匹配。
func GetOrCreateGlobalIDWithMeta(vodName, year string, typeId int64) (int64, error) {
	id, tier, err := resolveGlobalVideoID(instance, nil, vodName, year, typeId)
	if err != nil {
		return 0, err
	}
	if tier != tierNew {
		applog.Info("[global] 身份匹配(%s): %q (year=%q, type_id=%d) -> global_id=%d", tier, vodName, year, typeId, id)
	} else {
		applog.Info("[global] 新建条目: %q (year=%q, type_id=%d) -> global_id=%d", vodName, year, typeId, id)
	}
	return id, nil
}
