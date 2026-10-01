package douban

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// parseDoubanChart 解析豆瓣热榜 HTML
func parseDoubanChart(html string) []DoubanChartItem {
	blocks := chartItemRegex.FindAllStringSubmatch(html, -1)
	var items []DoubanChartItem
	seen := make(map[string]bool)

	for _, block := range blocks {
		if len(block) < 2 {
			continue
		}
		content := block[1]

		idMatch := chartLinkRegex.FindStringSubmatch(content)
		if len(idMatch) < 2 {
			continue
		}
		subjectID := idMatch[1]
		if seen[subjectID] {
			continue
		}
		seen[subjectID] = true

		item := DoubanChartItem{SubjectID: subjectID}

		if m := chartTitleRegex.FindStringSubmatch(content); len(m) >= 2 {
			item.Title = strings.TrimSpace(m[1])
		}
		if m := chartPosterRegex.FindStringSubmatch(content); len(m) >= 2 {
			item.PosterURL = strings.TrimSpace(m[1])
		}
		if m := chartRatingRegex.FindStringSubmatch(content); len(m) >= 2 {
			item.Rating = strings.TrimSpace(m[1])
		}
		if m := chartVotesRegex.FindStringSubmatch(content); len(m) >= 2 {
			item.Votes = strings.TrimSpace(m[1])
		}
		if m := chartInfoRegex.FindStringSubmatch(content); len(m) >= 2 {
			item.Info = strings.TrimSpace(m[1])
		}

		items = append(items, item)
	}
	return items
}

// upsertChartItems 将热榜数据插入 global_video 表
// 逻辑：归一化匹配已有记录 → 匹配成功则更新，匹配不上则新增（绝不跳过）
// isValidRating 检查评分是否为有效正数（排除 "0"、"0.0" 等无效值）
func isValidRating(s string) bool {
	if s == "" {
		return false
	}
	f, err := strconv.ParseFloat(s, 64)
	return err == nil && f > 0
}

// parseInfoFields 从 Info 字段解析 year、area
// Info 格式: "2025-09-05(多伦多电影节) / 2026-05-15(美国) / 克里斯·埃文斯 / ..."
func parseInfoFields(info string) (year, area string) {
	year, area, _, _ = parseInfoFull(info)
	return
}

// parseInfoFull 从 Info 字段完整解析 year、area、releaseDate、cast
func parseInfoFull(info string) (year, area, releaseDate, cast string) {
	if info == "" {
		return "", "", "", ""
	}
	parts := strings.Split(info, "/")
	var names []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}

		// 尝试解析为日期
		dateStr := p
		if idx := strings.Index(dateStr, "("); idx > 0 {
			dateStr = dateStr[:idx]
		}
		dateStr = strings.TrimSpace(dateStr)
		isDate := false
		for _, layout := range []string{"2006-01-02", "2006/01/02", "2006.01.02", "2006"} {
			if t, err := time.Parse(layout, dateStr); err == nil {
				isDate = true
				if year == "" {
					year = fmt.Sprintf("%d", t.Year())
				}
				if releaseDate == "" {
					releaseDate = dateStr
				}
				break
			}
		}
		if isDate {
			// 尝试从括号中提取地区
			if area == "" && strings.Contains(p, "(") {
				start := strings.Index(p, "(")
				end := strings.Index(p, ")")
				if start > 0 && end > start {
					candidate := p[start+1 : end]
					if len([]rune(candidate)) <= 10 && !strings.Contains(candidate, "节") && !strings.Contains(candidate, "展") {
						area = candidate
					}
				}
			}
			continue
		}

		// 不是日期，视为人名
		names = append(names, p)
	}
	if len(names) > 0 {
		// 第一个人名视为导演，其余为演员
		if len(names) > 1 {
			director := names[0]
			actors := strings.Join(names[1:], " / ")
			if len([]rune(actors)) > 60 {
				actors = string([]rune(actors)[:60]) + "..."
			}
			cast = director + " / " + actors
		} else {
			cast = names[0]
		}
	}
	return
}

// parseReleaseDate 从 Info 字段解析最早发布日期
func parseReleaseDate(info string) time.Time {
	if info == "" {
		return time.Time{}
	}
	firstPart := info
	if idx := strings.Index(info, "/"); idx > 0 {
		firstPart = info[:idx]
	}
	if idx := strings.Index(firstPart, "("); idx > 0 {
		firstPart = firstPart[:idx]
	}
	firstPart = strings.TrimSpace(firstPart)
	for _, layout := range []string{"2006-01-02", "2006/01/02", "2006.01.02", "2006"} {
		if t, err := time.Parse(layout, firstPart); err == nil {
			return t
		}
	}
	return time.Time{}
}
