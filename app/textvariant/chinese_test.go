package textvariant

import "testing"

func TestTraditionalSimplifiedTableShape(t *testing.T) {
	runes := []rune(traditionalSimplifiedPairs)
	if len(runes)%2 != 0 {
		t.Fatalf("繁简对照表长度应为偶数，实际 %d", len(runes))
	}
	for i := 0; i < len(runes); i += 2 {
		if runes[i] == runes[i+1] {
			t.Errorf("第 %d 组是自映射: %q", i/2, string(runes[i]))
		}
	}
	if len(traditionalToSimplified)*2 > len(runes) {
		t.Fatalf("表里的键有重复：%d 个键对 %d 组", len(traditionalToSimplified), len(runes)/2)
	}
	for _, probe := range []struct{ from, to string }{{"無", "无"}, {"發", "发"}, {"東", "东"}} {
		if got := Simplified(probe.from); got != probe.to {
			t.Errorf("常见繁体字折叠失败: %q -> %q，期望 %q", probe.from, got, probe.to)
		}
	}
}
