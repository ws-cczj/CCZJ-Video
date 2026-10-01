package db

import "testing"

// 类型名的同义词映射必须发生在剥后缀之后：库里既有「连续剧」也有「电视」，
// 早先是先剥后缀再查表，而表里的键是剥之前的写法，于是「连续剧」被剥成「连续」
// 后查不到，和「电视」分成两个类型桶。
func TestNormalizeTypeNameMapsSynonymsAfterSuffixStrip(t *testing.T) {
	cases := []struct{ in, want string }{
		{"连续剧", "电视"},
		{"电视", "电视"},
		{"电视剧", "电视"},
		{"连续剧类", "电视"},
		{"动画片", "动漫"},
		{"动画", "动漫"},
		{"动漫", "动漫"},
		{"记录片", "纪录"},
		{"纪录片", "纪录"},
		{"电影", "电影"},
		{"  综艺  ", "综艺"},
		{"欧美剧", "欧美"},
	}
	for _, c := range cases {
		if got := normalizeTypeName(c.in); got != c.want {
			t.Errorf("normalizeTypeName(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 写成同义词的两种写法必须落到同一个键，否则 global_types 会长出成对的类型。
func TestNormalizeTypeNamePutsSpellingVariantsOnOneKey(t *testing.T) {
	pairs := [][2]string{
		{"连续剧", "电视"},
		{"动画", "动漫"},
		{"记录片", "纪录片"},
	}
	for _, pair := range pairs {
		if normalizeTypeName(pair[0]) != normalizeTypeName(pair[1]) {
			t.Errorf("%q 与 %q 归一后不同: %q vs %q",
				pair[0], pair[1], normalizeTypeName(pair[0]), normalizeTypeName(pair[1]))
		}
	}
}
