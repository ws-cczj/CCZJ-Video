package douban

import (
	"html"
	"regexp"
	"strconv"
	"strings"
)

// cleanTitle 去除标题中的不可见字符和年份后缀
func cleanTitle(title string) string {
	title = html.UnescapeString(stripHTMLTags(title))
	// 去除不可见 Unicode 字符（LTR mark、RTL mark 等）
	title = strings.Map(func(r rune) rune {
		if r == '\u200E' || r == '\u200F' || r == '\u200B' || r == '\uFEFF' {
			return -1 // 删除
		}
		return r
	}, title)
	// 去除年份后缀
	title = searchYearRegex.ReplaceAllString(title, "")
	title = bareYearSuffixRegex.ReplaceAllString(title, "")
	title = strings.TrimSpace(title)
	return title
}

// splitItemBlocks 将搜索结果 HTML 拆分为独立的候选项块
func splitItemBlocks(html string) []string {
	var blocks []string
	markers := itemRootOpenRegex.FindAllStringIndex(html, -1)
	for i, marker := range markers {
		start := marker[1]
		end := len(html)
		if i+1 < len(markers) {
			end = markers[i+1][0]
		}
		if start < end {
			blocks = append(blocks, html[start:end])
		}
	}
	return blocks
}

func parseHTMLAttributes(tag string) map[string]string {
	attrs := make(map[string]string)
	for _, match := range htmlAttrRegex.FindAllStringSubmatch(tag, -1) {
		if len(match) < 5 {
			continue
		}
		value := match[2]
		if value == "" {
			value = match[3]
		}
		if value == "" {
			value = match[4]
		}
		attrs[strings.ToLower(match[1])] = html.UnescapeString(value)
	}
	return attrs
}

func hasHTMLClass(classes, className string) bool {
	for _, class := range strings.Fields(classes) {
		if class == className {
			return true
		}
	}
	return false
}

func extractSubjectID(text string) string {
	for _, pattern := range []*regexp.Regexp{
		moreurlSubjectIDRegex,
		subjectIDRegex,
		subjectIDRegexSmart,
		subjectIDRegexJSON,
	} {
		if match := pattern.FindStringSubmatch(text); len(match) >= 2 {
			return match[1]
		}
	}
	return ""
}

func extractCandidateTitle(block string) string {
	for _, anchor := range anchorRegex.FindAllString(block, -1) {
		openEnd := strings.Index(anchor, ">")
		if openEnd < 0 {
			continue
		}
		attrs := parseHTMLAttributes(anchor[:openEnd+1])
		closeStart := strings.LastIndex(strings.ToLower(anchor), "</a>")
		if closeStart < openEnd {
			continue
		}
		text := strings.TrimSpace(html.UnescapeString(stripHTMLTags(anchor[openEnd+1 : closeStart])))
		classes := attrs["class"]
		if (hasHTMLClass(classes, "title-text") || hasHTMLClass(classes, "DouWeb-SR-subject-info-name")) && text != "" {
			return text
		}
		if extractSubjectID(anchor) != "" && attrs["title"] != "" {
			return strings.TrimSpace(html.UnescapeString(attrs["title"]))
		}
	}
	return ""
}

func parseSearchCandidatesFromAnchors(html string) []SearchCandidate {
	var candidates []SearchCandidate
	seen := make(map[string]bool)
	for _, anchor := range anchorRegex.FindAllString(html, -1) {
		subjectID := extractSubjectID(anchor)
		if subjectID == "" || seen[subjectID] {
			continue
		}
		title := extractCandidateTitle(anchor)
		if title == "" {
			continue
		}
		seen[subjectID] = true
		year := 0
		if match := searchYearRegex.FindStringSubmatch(title); len(match) >= 2 {
			year, _ = strconv.Atoi(match[1])
		}
		candidates = append(candidates, SearchCandidate{SubjectID: subjectID, Title: cleanTitle(title), Year: year})
	}
	return candidates
}

// parseSearchCandidates 解析搜索结果，提取所有候选项及其元数据。
// 优先读页面内嵌的 window.__DATA__（当前豆瓣搜索页唯一的候选来源），
// 拿不到再退回去 node 节点的老 HTML 路径。
func parseSearchCandidates(html string) []SearchCandidate {
	if candidates := parseSearchCandidatesFromJSON(html); len(candidates) > 0 {
		return candidates
	}
	blocks := splitItemBlocks(html)
	if len(blocks) == 0 {
		return parseSearchCandidatesFromAnchors(html)
	}

	var candidates []SearchCandidate
	for _, block := range blocks {
		// 提取 subject ID（从 data-moreurl 的 JS 参数中）
		subjectID := extractSubjectID(block)
		if subjectID == "" {
			continue
		}

		// 提取标题
		rawTitle := extractCandidateTitle(block)
		if rawTitle == "" {
			continue
		}

		// 提取年份
		year := 0
		if ym := searchYearRegex.FindStringSubmatch(rawTitle); len(ym) >= 2 {
			year, _ = strconv.Atoi(ym[1])
		}
		title := cleanTitle(rawTitle)

		// 提取所有 meta abstract 内容
		metaMatches := searchMetaRegex.FindAllStringSubmatch(block, -1)
		var meta1, meta2 string
		if len(metaMatches) >= 1 {
			meta1 = stripHTMLTags(metaMatches[0][1])
		}
		if len(metaMatches) >= 2 {
			meta2 = stripHTMLTags(metaMatches[1][1])
		}

		// 判断是否为剧集
		isSeries := strings.Contains(block, `[剧集]`) ||
			strings.Contains(block, `is_tv:'1'`) ||
			strings.Contains(block, `is_tv:"1"`) ||
			strings.Contains(block, `is_tv=\"1\"`)

		// 解析导演/演员（meta2 中导演在前、演员在后，用 / 分隔）
		director, actor := parseDirectorActor(meta2)

		candidates = append(candidates, SearchCandidate{
			SubjectID: subjectID,
			Title:     title,
			Year:      year,
			IsSeries:  isSeries,
			Meta:      meta1,
			Director:  director,
			Actor:     actor,
		})
	}
	return candidates
}

// parseDirectorActor 从 meta2 字符串中分离导演和演员
// 豆瓣搜索结果 meta2 格式：导演 / 导演 / 演员 / 演员 / ...
// 第一个 / 之前的部分是导演，其余是演员
func parseDirectorActor(meta2 string) (string, string) {
	if meta2 == "" {
		return "", ""
	}
	parts := strings.Split(meta2, "/")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	// 豆瓣搜索页 meta2 中导演通常在前，用第一个非空分隔
	// 实际观察：导演有1-2个，后面全是演员
	// 简单策略：前2个为导演，其余为演员（如果总数<=2则全是导演）
	if len(parts) <= 2 {
		return strings.Join(parts, ","), ""
	}
	// 取前2个为导演，其余为演员
	return strings.Join(parts[:2], ","), strings.Join(parts[2:], ",")
}

// scoreCandidate 计算候选项与搜索元数据的匹配分数
// 注意：豆瓣的「剧集/电影」标签不属于类型，类型比较由 global_types 层负责
func scoreCandidate(c *SearchCandidate, meta SearchMeta) int {
	normTitle := normalizeTitle(c.Title)
	normKeyword := normalizeTitle(meta.VodName)
	candBase, candSeason, candPart := splitSeasonTag(normTitle)
	metaBase, metaSeason, metaPart := splitSeasonTag(normKeyword)

	// 片名是门槛，不是加分项：对不上就直接零分，年份/导演/演员都不再参与。
	// 否则「同年 + 同导演×2 + 同演员」足以把一部片名无关的候选抬过阈值。
	score := titleMatchScore(normTitle, normKeyword, candBase, metaBase)
	if score == 0 {
		return 0
	}
	// 季号冲突走同一条路：短路，而不是扣分。同一部剧各季的片名、导演、演员几乎
	// 全都相同，扣掉的分能被旁证加回来，而挂错季会连带污染评分、海报和兄弟继承。
	delta, conflict := seasonMatchDelta(metaSeason, metaPart, candSeason, candPart)
	if conflict {
		return seasonConflictScore
	}
	score += delta

	// 年份匹配
	if meta.Year != "" && c.Year > 0 {
		if strconv.Itoa(c.Year) == meta.Year {
			score += 30
		} else {
			score -= 15 // 年份不匹配扣分
		}
	}

	// 导演匹配（豆瓣用 / 分隔，本地数据可能用 , 或 、 分隔）
	if meta.Director != "" && c.Director != "" {
		metaDirectors := splitCSV(meta.Director)
		cDirectors := splitCSV(c.Director)
		for _, md := range metaDirectors {
			for _, cd := range cDirectors {
				if md != "" && cd != "" && md == cd {
					score += 20
				}
			}
		}
	}

	// 演员匹配（最多 +15 分）
	if meta.Actor != "" && c.Actor != "" {
		metaActors := splitCSV(meta.Actor)
		cActors := splitCSV(c.Actor)
		actorScore := 0
		for _, ma := range metaActors {
			for _, ca := range cActors {
				if ma != "" && ca != "" && ma == ca {
					actorScore += 5
					if actorScore >= 15 {
						break
					}
				}
			}
			if actorScore >= 15 {
				break
			}
		}
		score += actorScore
	}

	return score
}

// splitCSV 将各种分隔符（逗号、斜杠、顿号、全角逗号等）拆分为列表
func splitCSV(s string) []string {
	// 统一分隔符：将 / 、 ， 全部替换为英文逗号
	s = strings.ReplaceAll(s, "/", ",")
	s = strings.ReplaceAll(s, "、", ",")
	s = strings.ReplaceAll(s, "\uff0c", ",") // 全角逗号
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// bestMatch 从候选列表中选择最佳匹配
func bestMatch(candidates []SearchCandidate, meta SearchMeta) (*SearchCandidate, int) {
	if len(candidates) == 0 {
		return nil, 0
	}
	best := &candidates[0]
	bestScore := scoreCandidate(&candidates[0], meta)
	for i := 1; i < len(candidates); i++ {
		s := scoreCandidate(&candidates[i], meta)
		if s > bestScore {
			bestScore = s
			best = &candidates[i]
		}
	}
	return best, bestScore
}
