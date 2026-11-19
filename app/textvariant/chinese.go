package textvariant

import "strings"

// traditionalSimplifiedPairs 这张表由 scripts/gen_t2s_table.py 从 OpenCC/Unihan 派生
// 字典里抽出，只含「单字对单字、且折叠方向永远是繁→简」的条目；需要上下文才能定形
// 的字（著/彥外的 乾、瞭、餘 之类词义替换）留在短语层，我们拿不到，也就不会折错。
//
// 折错的代价只是两侧同时折成一个错字、比较结果不变；漏折的代价是繁简两边对不上，
// 所以这张表宁可全一点。

// Simplified folds single-character traditional variants for title matching.
func Simplified(s string) string {
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

var simplifiedToTraditional = func() map[rune]rune {
	pairs := []rune(simplifiedTraditionalPairs)
	result := make(map[rune]rune, len(pairs)/2)
	for i := 0; i < len(pairs); i += 2 {
		result[pairs[i]] = pairs[i+1]
	}
	return result
}()

// Traditional produces a search variant for catalogs that use traditional Chinese.
func Traditional(s string) string {
	return strings.Map(func(r rune) rune {
		if converted, ok := simplifiedToTraditional[r]; ok {
			return converted
		}
		return r
	}, s)
}

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
