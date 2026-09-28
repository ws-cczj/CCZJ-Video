package db

import (
	"math/bits"
	"math/rand"
	"testing"
	"unicode/utf8"
)

// 相似度档加了 O(1) 剪枝，是为了把「每匹配一个新标题就和全表做一次编辑距离」
// 变成常数级跳过。剪枝的代价是可能把真正同一部片拆成两条身份，所以这里钉两件事：
// 达标的一对绝不被剪掉，以及剪枝确实有活可干（语料退化时测试要失败）。

func similarityPruneCorpus() [][2]string {
	bases := []string{
		"阿凡达：水之道",
		"avatar the way of water",
		"老友记 第六季",
		"权力的游戏 第二季",
		"三体，问题",
		"这是一个用来测试长度窗口边界情况的很长的中文片名",
		"this one is an english title with quite a few words",
	}
	rng := rand.New(rand.NewSource(20260927))
	pairs := make([][2]string, 0, len(bases)*120)
	for _, base := range bases {
		runes := []rune(base)
		pairs = append(pairs, [2]string{base, base})
		for i := 0; i < 120; i++ {
			mutated := mutateTitle(rng, runes, 1+rng.Intn(3))
			pairs = append(pairs, [2]string{base, string(mutated)})
		}
	}
	return pairs
}

// mutateTitle 随机做 k 次 删除/替换/插入，覆盖阈值 0.90 两侧的边界。
func mutateTitle(rng *rand.Rand, runes []rune, k int) []rune {
	out := make([]rune, len(runes))
	copy(out, runes)
	for ; k > 0; k-- {
		if len(out) == 0 {
			break
		}
		pos := rng.Intn(len(out) + 1)
		switch rng.Intn(3) {
		case 0:
			if pos < len(out) {
				out = append(out[:pos], out[pos+1:]...)
			}
		case 1:
			if pos < len(out) {
				out[pos] = rune('a' + rng.Intn(26))
			}
		default:
			out = append(out, 0)
			copy(out[pos+1:], out[pos:])
			out[pos] = 'X'
		}
	}
	return out
}

func TestSimilarityPruneNeverRejectsAdmissiblePair(t *testing.T) {
	admissible := 0
	for _, pair := range similarityPruneCorpus() {
		na, nb := normalizeTitle(pair[0]), normalizeTitle(pair[1])
		if normalizedSimilarity(na, nb) < 0.90 {
			continue
		}
		la, lb := utf8.RuneCountInString(na), utf8.RuneCountInString(nb)
		maxLen, diff := la, la-lb
		if lb > maxLen {
			maxLen = lb
		}
		if diff < 0 {
			diff = -diff
		}
		sigDiff := bits.OnesCount64(runeSignature(na) ^ runeSignature(nb))
		if similarityRuledOut(diff, maxLen, sigDiff) {
			t.Fatalf("剪枝挡掉了达标的一对 %q / %q (len %d/%d, 长度差 %d, 位差 %d)", pair[0], pair[1], la, lb, diff, sigDiff)
		}
		admissible++
	}
	if admissible == 0 {
		t.Fatal("语料没产生任何达标对，剪枝没有被检验")
	}
	t.Logf("检验了 %d 对达标名字", admissible)
}

// 剪枝的收益：位差上界是 2×(maxLen/10)，绝大多数毫不相关的候选在这里就被跳过。
func TestSimilarityPruneRejectsUnrelatedPairs(t *testing.T) {
	na := normalizeTitle("这是一个用来测试长度窗口边界情况的很长的中文片名")
	nb := normalizeTitle("老友记 第六季")
	la, lb := utf8.RuneCountInString(na), utf8.RuneCountInString(nb)
	if !similarityRuledOut(la-lb, la, bits.OnesCount64(runeSignature(na)^runeSignature(nb))) {
		t.Fatal("长度差悬殊的一对应当被剪枝直接判死")
	}
}

// 阶梯的整体结果不能因为剪枝而变：这几条是历史上真出过问题的形状。
func TestMatchLadderKeepsResultsAfterPruning(t *testing.T) {
	index := newGlobalVideoIndex([]globalCandidate{
		{ID: 1, VodName: "阿凡达：水之道", TypeID: 3, Year: "2022"},
		{ID: 2, VodName: "老友记第六季", TypeID: 5, Year: "2000"},
		{ID: 3, VodName: "this one is an english title with quite a few words", TypeID: 4, Year: "2010"},
		{ID: 4, VodName: "this one is an english title with quite a few word", TypeID: 4, Year: "2011"},
	})
	// 只差一个尾字母，长度 41/41：编辑距离 1 远在阈值内，年份冲突时靠高相似度档并成一条。
	if id, tier := index.match("this one is an english title with quite a few word", "", 4); id != 4 || tier != tierExact {
		t.Fatalf("精确档应先于相似度档生效，got (%d, %s)", id, tier)
	}
	if id, tier := index.match("this one is AN ENGLISH TITLE WITH QUITE A FEW WORDS!", "", 4); id != 3 || tier != tierAlias {
		t.Fatalf("标点与大小写差异应落在别名档，got (%d, %s)", id, tier)
	}
	if id, tier := index.match("老友记 第六季!", "2000", 5); id != 2 || tier != tierAlias {
		t.Fatalf("空白加标点差异且年份吻合应落在别名档，got (%d, %s)", id, tier)
	}
	// 相似度档单独验：一个字母之差 + 年份冲突 → tierHigh；年份缺失 → tierMeta。
	// 这两条都只在剪枝后的候选里跑得起来，剪枝若错杀就会退化成 tierNew。
	simIndex := newGlobalVideoIndex([]globalCandidate{
		{ID: 7, VodName: "this one is an english title with quite a few words", TypeID: 4, Year: "2010"},
	})
	if id, tier := simIndex.match("this one is an english title with quite a few wordz", "2011", 4); id != 7 || tier != tierHigh {
		t.Fatalf("年份冲突但相似度仍在高位档，got (%d, %s)", id, tier)
	}
	if id, tier := simIndex.match("this one is an english title with quite a few wordz", "", 4); id != 7 || tier != tierMeta {
		t.Fatalf("年份缺失时相似度档应直接认领，got (%d, %s)", id, tier)
	}
	// 与所有候选都不达标：必须判新，不能被剪枝放宽后错挂。
	if id, tier := index.match("完全不相干的一个名字", "", 3); id != 0 || tier != tierNew {
		t.Fatalf("不相关标题应新建，got (%d, %s)", id, tier)
	}
}

// 建一次索引的派生数据（norms/lens/sigs）必须和下标严格对齐，
// 否则剪枝读到的就是别人的长度和位图。
func TestIndexDerivedArraysAlignWithCandidates(t *testing.T) {
	pairs := similarityPruneCorpus()
	candidates := make([]globalCandidate, 0, len(pairs))
	for i, pair := range pairs {
		candidates = append(candidates, globalCandidate{ID: int64(i + 1), VodName: pair[0], TypeID: 1})
		candidates = append(candidates, globalCandidate{ID: int64(len(pairs) + i + 1), VodName: pair[1], TypeID: 2})
	}
	index := newGlobalVideoIndex(candidates)
	for i, candidate := range index.candidates {
		norm := normalizeTitle(candidate.VodName)
		if index.norms[i] != norm {
			t.Fatalf("index %d: norms = %q, want %q", i, index.norms[i], norm)
		}
		if index.lens[i] != utf8.RuneCountInString(norm) {
			t.Fatalf("index %d: lens = %d, want %d", i, index.lens[i], utf8.RuneCountInString(norm))
		}
		if index.sigs[i] != runeSignature(norm) {
			t.Fatalf("index %d: sigs 与归一化标题不一致", i)
		}
	}
	if len(index.byID) != len(candidates) {
		t.Fatalf("byID 有 %d 条，候选有 %d 条", len(index.byID), len(candidates))
	}
}

// 采集一轮要匹配上万标题，这里给出匹配的单次开销量级（候选数取真机库的规模）。
func BenchmarkGlobalVideoIndexMatch(b *testing.B) {
	candidates := make([]globalCandidate, 0, 20000)
	for i := 0; i < 20000; i++ {
		candidates = append(candidates, globalCandidate{
			ID:      int64(i + 1),
			VodName: "影片 " + string(rune('a'+i%26)) + string(rune('a'+(i/26)%26)) + " 名称正文",
			TypeID:  int64(i%9) + 1,
			Year:    "2010",
		})
	}
	index := newGlobalVideoIndex(candidates)
	queries := make([]string, 0, 200)
	for i := 0; i < 200; i++ {
		queries = append(queries, "影片 名称正文 "+string(rune('a'+i%26))+" 无关后缀")
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for _, q := range queries {
			index.match(q, "2010", 3)
		}
	}
}
