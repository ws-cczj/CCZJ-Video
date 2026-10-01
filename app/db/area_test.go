package db

import "testing"

// 地区写法只收拢确定等价的，认不出的一律原样保留：把一个产地切成几个桶是缺陷，
// 把「韩国」猜成别的国家是事故。
func TestNormalizeAreaCollapsesEquivalentSpellings(t *testing.T) {
	cases := []struct{ in, want string }{
		{"中国大陆", "大陆"},
		{"中国", "大陆"},
		{"内地", "大陆"},
		{" 大陆 ", "大陆"},
		{"中国香港", "香港"},
		{"中国台湾", "台湾"},
		{"美国网络", "美国"},
		{"其他", "其它"},
		{"韩国", "韩国"},
		{"", ""},
		{"   ", ""},
		// 多值：逐个归一，按首次出现顺序重拼，同一产地被两侧写法切开时只留一次。
		{"中国大陆,中国香港", "大陆,香港"},
		{"澳大利亚、美国", "澳大利亚,美国"},
		{"中国/内地", "大陆"},
		// 空格是写法内部的排版，不是分隔符：拆了再拼会把 "Visible area" 改成别的值。
		{"美国 英国", "美国 英国"},
		{"Visible area", "Visible area"},
	}
	for _, c := range cases {
		if got := normalizeArea(c.in); got != c.want {
			t.Errorf("normalizeArea(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 归一化必须幂等：采集侧每次重写 area 都走同一个函数，二次归一不该再改动值。
func TestNormalizeAreaIsIdempotent(t *testing.T) {
	for _, in := range []string{"中国大陆", "中国香港,内地", "美国网络", "韩国", ""} {
		once := normalizeArea(in)
		if twice := normalizeArea(once); twice != once {
			t.Errorf("normalizeArea 不幂等: %q -> %q -> %q", in, once, twice)
		}
	}
}
