package db

import (
	"regexp"
	"strings"
)

// 地区归一化。
//
// area 同时来自源站字段和豆瓣国家列表，两边写法不一样：同一个大陆产出会写成
// 「大陆」「中国大陆」「内地」，香港写成「香港」或「中国香港」，按地区筛选时
// 一个产地就被切成几个桶。这里只收拢确定等价的写法，认不出的一律原样保留——
// 宁可多留一个桶，也不能把「韩国」猜成别的国家。

// areaSynonyms 是明确等价的写法：键是去空白后的原文，值是规范名。
var areaSynonyms = map[string]string{
	"中国":   "大陆",
	"中国大陆": "大陆",
	"内地":   "大陆",
	"中国香港": "香港",
	"中国澳门": "澳门",
	"中国台湾": "台湾",
	"美国网络": "美国",
	"其他":   "其它",
}

// areaSeparators 是源站和豆瓣各自的分隔写法。
//
// 空格刻意不算分隔符：它是单个写法内部的排版（"Visible area"、"South Korea"），
// 拿它切会把一个地区名拆成两截再拼回去，改写后的值既不是原值也进不了同义词表。
var areaSeparators = regexp.MustCompile(`[,，、/|]+`)

// normalizeArea 归一化一个地区字段。多值（「澳大利亚,美国」）逐个归一后按
// 首次出现顺序重新拼接并去重；空值原样返回，好让「空 = 不知道地区」
// 不被写成「其它」这种看起来像数据的占位符。
func normalizeArea(area string) string {
	area = strings.TrimSpace(area)
	if area == "" {
		return ""
	}
	tokens := areaSeparators.Split(area, -1)
	seen := make(map[string]bool, len(tokens))
	out := make([]string, 0, len(tokens))
	for _, token := range tokens {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		name, ok := areaSynonyms[token]
		if !ok {
			name = token
		}
		if seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, name)
	}
	if len(out) == 0 {
		return area
	}
	return strings.Join(out, ",")
}
