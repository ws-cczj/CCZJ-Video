package douban

import (
	"strings"
	"testing"
)

// C1：标题归一化与季/部打分。这里只测纯函数，不碰网络和数据库。

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
		if got := foldTraditional(probe.from); got != probe.to {
			t.Errorf("常见繁体字折叠失败: %q -> %q，期望 %q", probe.from, got, probe.to)
		}
	}
}

func TestNormalizeTitleFoldsVariants(t *testing.T) {
	cases := []struct{ in, want string }{
		{"权力的游戏 第八季 (2019)", "权力的游戏第八季"},
		{"是，大臣 第一季", "是大臣第一季"},
		{"《甄嬛传》", "甄嬛传"},
		{"  零\u200b宽\u00a0空\u3000格  ", "零宽空格"},
		{"全！角？标·点—", "全角标点"},
		{"無間道", "无间道"},
		{"ALL IN", "allin"},
	}
	for _, c := range cases {
		if got := normalizeTitle(c.in); got != c.want {
			t.Errorf("normalizeTitle(%q) = %q，期望 %q", c.in, got, c.want)
		}
	}
}

func TestSplitSeasonTag(t *testing.T) {
	cases := []struct {
		in     string
		base   string
		season int
		part   string
	}{
		{"是，大臣 第一季", "是大臣", 1, ""},
		{"权力的游戏 第8季", "权力的游戏", 8, ""},
		{"权力的游戏第八季", "权力的游戏", 8, ""},
		{"Game of Thrones Season 8", "gameofthrones", 8, ""},
		{"生活大爆炸 S12E01", "生活大爆炸", 12, ""},
		{"战狼2", "战狼", 2, ""},
		{"复仇者联盟IV", "复仇者联盟", 4, ""},
		{"甄嬛传 上部", "甄嬛传", 0, "上"},
		{"甄嬛传（下）", "甄嬛传", 0, "下"},
		{"怪奇物语 第二季 上", "怪奇物语", 2, "上"},
		// 没有季号的必须原样返回：不能凭空造出一个季号来参与判负。
		{"琅琊榜", "琅琊榜", 0, ""},
		{"战狼 2015", "战狼", 0, ""},
		{"十一罗汉", "十一罗汉", 0, ""},
		{"2012", "2012", 0, ""},
		{"功夫熊猫3", "功夫熊猫", 3, ""},
	}
	for _, c := range cases {
		base, season, part := splitSeasonTag(normalizeTitle(c.in))
		if base != c.base || season != c.season || part != c.part {
			t.Errorf("splitSeasonTag(%q) = (%q,%d,%q)，期望 (%q,%d,%q)",
				c.in, base, season, part, c.base, c.season, c.part)
		}
	}
}

func TestParseSeasonNumber(t *testing.T) {
	oks := []struct {
		in   string
		want int
	}{
		{"12", 12}, {"零三", 3}, {"十一", 11}, {"二十三", 23}, {"一百零八", 108},
		{"iv", 4}, {"ix", 9}, {"xx", 20},
	}
	for _, c := range oks {
		got, ok := parseSeasonNumber(c.in)
		if !ok || got != c.want {
			t.Errorf("parseSeasonNumber(%q) = (%d,%v)，期望 (%d,true)", c.in, got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "  ", "abc", "0", "-3", "y"} {
		if got, ok := parseSeasonNumber(bad); ok {
			t.Errorf("parseSeasonNumber(%q) 不该解析成 %d", bad, got)
		}
	}
}

// 同一部剧的各季之间只差季号，靠片名相同是同分；这一条是 C1 的核心回归。
func TestScoreCandidateSeparatesSeasons(t *testing.T) {
	meta := SearchMeta{VodName: "是，大臣 第二季"}
	candidates := []SearchCandidate{
		{Title: "是，大臣 第一季"},
		{Title: "是，大臣 第二季"},
		{Title: "是，大臣 第三季"},
	}
	best, score := bestMatch(candidates, meta)
	if best == nil || best.Title != "是，大臣 第二季" {
		t.Fatalf("选中的是 %+v (score=%d)，期望第二季", best, score)
	}
	first := scoreCandidate(&candidates[0], meta)
	if score <= first {
		t.Fatalf("第二季 %d 分没有严格高于第一季 %d 分", score, first)
	}
}

func TestScoreCandidatePrefersFoldedTitles(t *testing.T) {
	cases := []struct {
		name    string
		keyword string
		title   string
		want    int // 至少这么多分
	}{
		{"繁简等价", "无间道", "無間道", 100},
		{"全半角与空格", "是，大臣", "是大臣", 100},
		{"书名号", "甄嬛传", "《甄嬛传》", 100},
		{"片名相同仅季号缺失", "琅琊榜", "琅琊榜 第一季", 90},
		{"不同季必须低于同季", "琅琊榜 第一季", "琅琊榜 第二季", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			meta := SearchMeta{VodName: c.keyword}
			got := scoreCandidate(&SearchCandidate{Title: c.title}, meta)
			if c.want == 0 {
				same := scoreCandidate(&SearchCandidate{Title: "琅琊榜 第一季"}, meta)
				if got >= same {
					t.Fatalf("不同季 %d 分不该不低于同季 %d 分", got, same)
				}
				return
			}
			if got < c.want {
				t.Fatalf("scoreCandidate(%q, %q) = %d，期望至少 %d", c.title, c.keyword, got, c.want)
			}
		})
	}
}

func TestSeasonMatchDeltaOnlyJudgesWhenBothSidesKnown(t *testing.T) {
	cases := []struct {
		name         string
		metaSeason   int
		metaPart     string
		candSeason   int
		candPart     string
		wantDelta    int
		wantConflict bool
	}{
		{name: "同季", metaSeason: 2, candSeason: 2, wantDelta: 25},
		{name: "异季", metaSeason: 2, candSeason: 3, wantConflict: true},
		{name: "候选没标季号", metaSeason: 0, candSeason: 3},
		{name: "关键词没标季号", metaSeason: 3, candSeason: 0},
		{name: "上/下同部", metaPart: "下", candPart: "下", wantDelta: 25},
		{name: "上/下冲突", metaPart: "下", candPart: "上", wantConflict: true},
		{name: "单侧有部", metaPart: "下", candSeason: 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			delta, conflict := seasonMatchDelta(c.metaSeason, c.metaPart, c.candSeason, c.candPart)
			if conflict != c.wantConflict {
				t.Fatalf("conflict = %v，期望 %v", conflict, c.wantConflict)
			}
			if !conflict && delta != c.wantDelta {
				t.Fatalf("delta = %d，期望 %d", delta, c.wantDelta)
			}
		})
	}
}

// 纯符号标题归一化后是空串，Contains(空) 恒真，不能让它白拿分数。
func TestScoreCandidateIgnoresEmptyTitles(t *testing.T) {
	if got := scoreCandidate(&SearchCandidate{Title: "正常的名字"}, SearchMeta{VodName: "！！！"}); got != 0 {
		t.Errorf("空关键词拿到了 %d 分", got)
	}
	if got := scoreCandidate(&SearchCandidate{Title: "？？"}, SearchMeta{VodName: "！！！"}); got != 0 {
		t.Errorf("两侧都空时拿到了 %d 分", got)
	}
}

// C2：阈值必须真的能拒绝。这些用例直接对照 doubanMatchThreshold，
// 断言的是「敢不敢挂」，不是「分数好不好看」。
func TestMatchThresholdAcceptsOnlyTitleBackedMatches(t *testing.T) {
	cases := []struct {
		name   string
		meta   SearchMeta
		cand   SearchCandidate
		accept bool
	}{
		{
			name:   "折叠后完全同名",
			meta:   SearchMeta{VodName: "无间道"},
			cand:   SearchCandidate{Title: "無間道"},
			accept: true,
		},
		{
			name:   "同季且同年",
			meta:   SearchMeta{VodName: "是，大臣 第二季", Year: "1986"},
			cand:   SearchCandidate{Title: "是，大臣 第二季", Year: 1986},
			accept: true,
		},
		{
			name:   "片名一致但本地没标季号",
			meta:   SearchMeta{VodName: "琅琊榜"},
			cand:   SearchCandidate{Title: "琅琊榜 第一季"},
			accept: true,
		},
		{
			name:   "只有包含关系：爱情 对上 爱情公寓",
			meta:   SearchMeta{VodName: "爱情"},
			cand:   SearchCandidate{Title: "爱情公寓"},
			accept: false,
		},
		{
			name:   "包含关系 + 同年可以佐证",
			meta:   SearchMeta{VodName: "爱情", Year: "2014"},
			cand:   SearchCandidate{Title: "爱情公寓", Year: 2014},
			accept: true,
		},
		{
			name: "片名无关，但年份/导演/演员全对上",
			meta: SearchMeta{VodName: "琅琊榜", Year: "2015", Director: "孔笙,李雪", Actor: "胡歌,刘涛,王凯"},
			cand: SearchCandidate{Title: "伪装者", Year: 2015, Director: "孔笙 / 李雪", Actor: "胡歌,刘涛,王凯"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scoreCandidate(&c.cand, c.meta)
			if (got >= doubanMatchThreshold) != c.accept {
				t.Fatalf("scoreCandidate = %d，阈值 %d，接受性应为 %v", got, doubanMatchThreshold, c.accept)
			}
		})
	}
}

// 同一部剧各季的导演和演员几乎都相同，所以季号冲突不能只是「扣分」：
// 扣完还能被旁证加回来的否决等于没有否决。
func TestSeasonConflictRejectsEvenWithEveryBonus(t *testing.T) {
	meta := SearchMeta{
		VodName:  "怪奇物语 第一季",
		Year:     "2016",
		Director: "张三,李四,王五",
		Actor:    "米莉,薇诺娜,芬恩",
	}
	cand := SearchCandidate{
		Title:    "怪奇物语 第二季",
		Year:     2016,
		Director: "张三 / 李四 / 王五",
		Actor:    "米莉,薇诺娜,芬恩",
	}
	if got := scoreCandidate(&cand, meta); got >= doubanMatchThreshold {
		t.Fatalf("冲突季号拿到了 %d 分，阈值 %d", got, doubanMatchThreshold)
	}
	// 上/下部冲突走同一条路。
	if got := scoreCandidate(&SearchCandidate{Title: "甄嬛传 上部"}, SearchMeta{VodName: "甄嬛传 下部"}); got >= doubanMatchThreshold {
		t.Fatalf("上/下冲突拿到了 %d 分", got)
	}
}

func TestNormalizeTitleIsIdempotent(t *testing.T) {
	for _, in := range []string{"权力的游戏 第八季 (2019)", "無間道", "战狼2", "是，大臣"} {
		once := normalizeTitle(in)
		if twice := normalizeTitle(once); twice != once {
			t.Errorf("归一化不幂等: %q -> %q -> %q", in, once, twice)
		}
		if strings.ContainsAny(once, " ()（）") {
			t.Errorf("归一化后仍残留空白或括号: %q", once)
		}
	}
}
