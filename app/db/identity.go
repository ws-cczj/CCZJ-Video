package db

import (
	"fmt"
	"math/bits"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jmoiron/sqlx"
)

// global_video 的身份判定只有这里一份实现：归一化、匹配阶梯、插入兜底。
//
// 过去同一套逻辑抄了三份（采集批量入库、豆瓣补全、收藏/历史各一份），而且
// Go 侧的归一化和 SQL 函数索引里的表达式各写各的：Go 认得全角感叹号/问号和
// 零宽空格，SQL 表达式不认，于是同一部片能在库里留下两条 global_video，收藏、
// 历史、豆瓣字段各自挂在不同 id 上。现在归一化只由 Go 算一次，结果存进
// global_video.name_norm，唯一索引和所有查询都读这一列。

// normalizeTitle 去掉所有空白、全角标点转半角并转小写，是标题归一化的唯一版本。
func normalizeTitle(s string) string {
	s = removeAllWhitespace(s)
	s = normalizeFullWidth(s)
	return strings.ToLower(s)
}

const (
	zeroWidthSpace = 0x200B
	noBreakSpace   = 0x00A0
)

// removeAllWhitespace 去除所有 Unicode 空白字符（包括全角空格、不间断空格、零宽空格）
func removeAllWhitespace(s string) string {
	var buf strings.Builder
	buf.Grow(len(s))
	for _, r := range s {
		if !unicode.IsSpace(r) && r != zeroWidthSpace && r != noBreakSpace {
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

// normalizeFullWidth 全角标点转半角
func normalizeFullWidth(s string) string {
	pairs := [][2]string{
		{"：", ":"}, // ：→ :
		{"（", "("}, // （→ (
		{"）", ")"}, // ）→ )
		{"！", "!"}, // ！→ !
		{"？", "?"}, // ？→ ?
		{"，", ","}, // ，→ ,
		{"　", ""},  // 　→ 删除
	}
	for _, pair := range pairs {
		s = strings.ReplaceAll(s, pair[0], pair[1])
	}
	return s
}

// titleAliasKey 识别标题的展示差异，不作为数据库唯一键。
// 它会去掉空白和标点，但保留数字、季数、部数等语义信息：
// 同一个感叹号放在序号前后会得到同一个 key，
// 「权力的游戏第三季」与「权力的游戏第八季」仍然不同。
func titleAliasKey(s string) string {
	s = normalizeTitle(s)
	var result strings.Builder
	result.Grow(len(s))
	for _, r := range s {
		if unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		result.WriteRune(r)
	}
	return result.String()
}

// globalCandidate 是身份匹配用的最小候选行：一个批次要匹配上千条，
// 候选集越窄越便宜。
type globalCandidate struct {
	ID      int64  `db:"id"`
	VodName string `db:"vod_name"`
	TypeID  int64  `db:"type_id"`
	Year    string `db:"year"`
}

// globalVideoIndex 把候选按三级键各建一张表，阶梯前三级只做哈希查找，
// 只有相似度那一档才扫候选。候选必须按 id 升序，保证同名多条时永远命中
// 最早建立的那条，源记录的先后顺序不会被随机化。
//
// norms/lens/sigs 是按 candidates 下标对齐的派生数据。归一化每条候选只做一次：
// 曾经每匹配一个新标题就把整表重新归一化一遍并跑满长度矩阵的编辑距离，
// 一次采集几百页就是几百万次重复劳动。
type globalVideoIndex struct {
	candidates []globalCandidate
	byID       map[int64]int
	byExact    map[string][]int
	byNorm     map[string][]int
	byAlias    map[string][]int
	norms      []string
	lens       []int
	sigs       []uint64
}

func newGlobalVideoIndex(candidates []globalCandidate) *globalVideoIndex {
	index := &globalVideoIndex{
		candidates: candidates,
		byID:       make(map[int64]int, len(candidates)),
		byExact:    make(map[string][]int, len(candidates)),
		byNorm:     make(map[string][]int, len(candidates)),
		byAlias:    make(map[string][]int, len(candidates)),
		norms:      make([]string, len(candidates)),
		lens:       make([]int, len(candidates)),
		sigs:       make([]uint64, len(candidates)),
	}
	for i, candidate := range candidates {
		index.indexAt(i, candidate)
	}
	return index
}

func (index *globalVideoIndex) indexAt(i int, candidate globalCandidate) {
	index.byID[candidate.ID] = i
	index.byExact[candidate.VodName] = append(index.byExact[candidate.VodName], i)
	norm := normalizeTitle(candidate.VodName)
	if norm != "" {
		index.byNorm[norm] = append(index.byNorm[norm], i)
	}
	if key := titleAliasKey(candidate.VodName); key != "" {
		index.byAlias[key] = append(index.byAlias[key], i)
	}
	index.norms[i] = norm
	index.lens[i] = utf8.RuneCountInString(norm)
	index.sigs[i] = runeSignature(norm)
}

// runeSignature 把标题里每个 rune 散到一个 64 位位图上。一次编辑最多让两侧各多
// 一个「落单」的 rune，每个落单 rune 至多翻转一位，所以两边签名的不同位数
// 不超过 2×编辑距离——match 里那道位差剪枝就靠它，且只会放宽不会错杀。
func runeSignature(s string) uint64 {
	var sig uint64
	for _, r := range s {
		h := uint64(r) * 0x9E3779B97F4A7C15
		h ^= h >> 32
		sig |= 1 << (h & 63)
	}
	return sig
}

// add 把新建的行并入索引，同一批次里后续同名记录才不会又走一遍插入。
func (index *globalVideoIndex) add(candidate globalCandidate) {
	i := len(index.candidates)
	index.candidates = append(index.candidates, candidate)
	index.norms = append(index.norms, "")
	index.lens = append(index.lens, 0)
	index.sigs = append(index.sigs, 0)
	index.indexAt(i, candidate)
}

// update 同步批次补齐进已有行的 type_id/year。名称没变，所以三级键都不用重建，
// 只改候选本身；不这样处理的话，同一批次里另一条同名记录还会被判定成「类型未知」。
func (index *globalVideoIndex) update(id, typeID int64, year string) {
	i, ok := index.byID[id]
	if !ok {
		return
	}
	if typeID != 0 {
		index.candidates[i].TypeID = typeID
	}
	if year != "" {
		index.candidates[i].Year = year
	}
}

// matchTier 说明命中了阶梯的哪一级，日志靠它定位错挂来源。
type matchTier string

const (
	tierExact matchTier = "exact"
	tierNorm  matchTier = "norm"
	tierAlias matchTier = "alias"
	tierMeta  matchTier = "similarity+meta"
	tierHigh  matchTier = "high-similarity"
	tierNew   matchTier = "new"
)

// similarityRuledOut 用两个编辑距离的下界提前判死：距离不小于长度差，也不小于
// 位图差的一半；而 ≥0.90 的相似度等价于 dist ≤ maxLen/10。任一上界越过去，这一对
// 名字就不可能达标。它只会放过本该淘汰的候选，绝不会错杀达标的。
func similarityRuledOut(lenDiff, maxLen, sigDiff int) bool {
	if lenDiff*10 > maxLen {
		return true
	}
	return sigDiff > 2*(maxLen/10)
}

// match 是身份阶梯的唯一实现：精确 → 归一化 → 别名 → 相似度+元数据。
// typeID=0 表示「不知道类型」（热榜、收藏、历史），此时不限类型；
// 已有记录 type_id=0 视为占位符，任何类型都可以认领它。
func (index *globalVideoIndex) match(vodName, year string, typeID int64) (int64, matchTier) {
	accepts := func(candidate globalCandidate) bool {
		return typeID == 0 || candidate.TypeID == typeID || candidate.TypeID == 0
	}
	normQuery := normalizeTitle(vodName)
	for _, i := range index.byExact[vodName] {
		if c := index.candidates[i]; accepts(c) {
			return c.ID, tierExact
		}
	}
	for _, i := range index.byNorm[normQuery] {
		if c := index.candidates[i]; accepts(c) {
			return c.ID, tierNorm
		}
	}
	if key := titleAliasKey(vodName); key != "" {
		for _, i := range index.byAlias[key] {
			c := index.candidates[i]
			if !accepts(c) {
				continue
			}
			if year != "" && c.Year != "" && year != c.Year {
				continue
			}
			if hasSeasonSuffix(vodName, c.VodName) {
				continue
			}
			return c.ID, tierAlias
		}
	}

	// 相似度档仍按 id 升序扫，同名多条时命中最早那条的行为不能变。
	// 每条候选先过 similarityRuledOut 的 O(1) 剪枝，真正跑编辑距离的只剩极少数，
	// 而归一化在建索引时已经算过一次，不必每配对重复归一化。
	var best *globalCandidate
	bestSimilarity := 0.0
	queryLen := utf8.RuneCountInString(normQuery)
	querySig := runeSignature(normQuery)
	for i := range index.candidates {
		candidate := &index.candidates[i]
		if !accepts(*candidate) {
			continue
		}
		maxLen := queryLen
		if index.lens[i] > maxLen {
			maxLen = index.lens[i]
		}
		diff := queryLen - index.lens[i]
		if diff < 0 {
			diff = -diff
		}
		if similarityRuledOut(diff, maxLen, bits.OnesCount64(querySig^index.sigs[i])) {
			continue
		}
		similarity := normalizedSimilarity(normQuery, index.norms[i])
		if similarity < 0.90 || hasSeasonSuffix(vodName, candidate.VodName) {
			continue
		}
		// 90%+ 且年份不冲突才算同一部；缺任一方年份时按可用信息判断。
		if year == "" || candidate.Year == "" || year == candidate.Year {
			return candidate.ID, tierMeta
		}
		// 极度相似(≥95%)但年份对不上：仍视为同一部，否则同一片的分年记录会各建一条。
		if similarity >= 0.95 && similarity > bestSimilarity {
			bestSimilarity = similarity
			best = candidate
		}
	}
	if best != nil {
		return best.ID, tierHigh
	}
	return 0, tierNew
}

// matchIndexedGlobal 只用 vod_name / name_norm 两个索引答精确档与归一化档，
// 与 globalVideoIndex.match 的前两段等价：同 id 升序取第一条类型相容的候选。
// 返回 id==0 表示这两档都没命中，调用方需要回落到整表候选做别名/相似度判断。
func matchIndexedGlobal(exec sqlx.Ext, vodName string, typeID int64) (int64, matchTier, error) {
	steps := []struct {
		clause string
		arg    string
		tier   matchTier
	}{
		{"vod_name=?", vodName, tierExact},
		{"name_norm=?", normalizeTitle(vodName), tierNorm},
	}
	for _, step := range steps {
		var rows []globalCandidate
		if err := sqlx.Select(exec, &rows, `SELECT id, vod_name, type_id, year FROM global_video WHERE `+step.clause+` ORDER BY id ASC`, step.arg); err != nil {
			return 0, tierNew, fmt.Errorf("match global video by %s: %w", step.clause, err)
		}
		for _, c := range rows {
			if typeID == 0 || c.TypeID == typeID || c.TypeID == 0 {
				return c.ID, step.tier, nil
			}
		}
	}
	return 0, tierNew, nil
}

// loadGlobalCandidates 读出全部候选。一轮采集由 CatalogBatch 调一次并复用返回的
// 索引；单独写几条的调用方（导入、详情页补录）按调用读一次，不值得常驻内存。
func loadGlobalCandidates(exec sqlx.Ext) ([]globalCandidate, error) {
	var rows []globalCandidate
	err := sqlx.Select(exec, &rows, `SELECT id, vod_name, type_id, year FROM global_video ORDER BY id ASC`)
	if err != nil {
		return nil, fmt.Errorf("load global video candidates: %w", err)
	}
	return rows, nil
}

// insertGlobalVideo 新建 global_video，并写入 Go 侧算好的 name_norm。
// 唯一索引冲突时（同名同类型已存在，或并发插入）回读既有行，
// 绝不因为 INSERT OR IGNORE 静默吞掉而返回 0。
func insertGlobalVideo(exec sqlx.Ext, vodName string, typeID int64) (int64, error) {
	vodName = strings.TrimSpace(vodName)
	if vodName == "" {
		return 0, fmt.Errorf("insert global video: empty vod_name")
	}
	nameNorm := normalizeTitle(vodName)
	if _, err := exec.Exec(`INSERT OR IGNORE INTO global_video (vod_name, name_norm, type_id, updated_at) VALUES (?, ?, ?, CURRENT_TIMESTAMP)`,
		vodName, nameNorm, typeID); err != nil {
		return 0, fmt.Errorf("insert global video %q: %w", vodName, err)
	}
	var id int64
	if err := sqlx.Get(exec, &id, `SELECT id FROM global_video WHERE name_norm=? AND type_id=? LIMIT 1`, nameNorm, typeID); err == nil && id > 0 {
		return id, nil
	}
	// type_id=0 是轮播图这类无类型数据建的占位记录，同名记录要并进去而不是另起一条。
	if err := sqlx.Get(exec, &id, `SELECT id FROM global_video WHERE name_norm=? AND type_id=0 LIMIT 1`, nameNorm); err == nil && id > 0 {
		return id, nil
	}
	if err := sqlx.Get(exec, &id, `SELECT id FROM global_video WHERE vod_name=? LIMIT 1`, vodName); err == nil && id > 0 {
		return id, nil
	}
	return 0, fmt.Errorf("global_video %q 插入后仍查不到", vodName)
}

// resolveGlobalVideoID 是「给一个标题拿到 global_id，没有就建」的唯一入口。
// 任何要写 global_video 的调用方都必须经它，而不是自己 INSERT：name_norm 只有
// 这一处写，绕过去就会让同一部片留下两条身份，收藏和历史各自挂在不同 id 上。
// index 为 nil 时按需载入全部候选——一次性的单条解析可以接受，循环里调用必须先自建
// 索引复用（采集走 CatalogBatch，热榜走 UpsertChartItems）。
func resolveGlobalVideoID(exec sqlx.Ext, index *globalVideoIndex, vodName, year string, typeID int64) (int64, matchTier, error) {
	vodName = strings.TrimSpace(vodName)
	if vodName == "" {
		return 0, tierNew, fmt.Errorf("resolve global video: empty vod_name")
	}
	if index == nil {
		// 单条调用方先试索引直接答得出来的两档：重复入库的同一部片绝大多数落在这里，
		// 为它们整表载入建索引等于每次调用都 O(表)。落空才回落别名/相似度档。
		id, tier, err := matchIndexedGlobal(exec, vodName, typeID)
		if err != nil {
			return 0, tierNew, err
		}
		if id > 0 {
			return id, tier, nil
		}
		candidates, err := loadGlobalCandidates(exec)
		if err != nil {
			return 0, tierNew, err
		}
		index = newGlobalVideoIndex(candidates)
	}
	if id, tier := index.match(vodName, year, typeID); id > 0 {
		return id, tier, nil
	}
	id, err := insertGlobalVideo(exec, vodName, typeID)
	if err != nil {
		return 0, tierNew, err
	}
	index.add(globalCandidate{ID: id, VodName: vodName, TypeID: typeID, Year: year})
	return id, tierNew, nil
}

// backfillNameNorm 用当前归一化实现重算全部 name_norm。
// 归一化规则以后有修正时，追加一条迁移调用它即可，不需要再改动索引语义。
func backfillNameNorm(exec sqlx.Ext) (int, error) {
	var rows []struct {
		ID      int64  `db:"id"`
		VodName string `db:"vod_name"`
	}
	if err := sqlx.Select(exec, &rows, `SELECT id, vod_name FROM global_video`); err != nil {
		return 0, fmt.Errorf("read global video names: %w", err)
	}
	updated := 0
	for _, row := range rows {
		if _, err := exec.Exec(`UPDATE global_video SET name_norm=? WHERE id=?`, normalizeTitle(row.VodName), row.ID); err != nil {
			return updated, fmt.Errorf("backfill name_norm for id=%d: %w", row.ID, err)
		}
		updated++
	}
	return updated, nil
}
