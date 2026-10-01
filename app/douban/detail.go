package douban

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

func ExtractLinkTexts(html string) string {
	links := linkTextRegex.FindAllStringSubmatch(html, -1)
	var names []string
	for _, link := range links {
		if len(link) >= 2 {
			name := strings.TrimSpace(link[1])
			if name != "" {
				names = append(names, name)
			}
		}
	}
	return strings.Join(names, " / ")
}

// subjectAbstract 对应 /j/subject_abstract 的 JSON 结构，只声明会用到的字段。
type subjectAbstract struct {
	R       int `json:"r"`
	Subject struct {
		ID            string   `json:"id"`
		Title         string   `json:"title"`
		Rate          string   `json:"rate"`
		Directors     []string `json:"directors"`
		Actors        []string `json:"actors"`
		Types         []string `json:"types"`
		Region        string   `json:"region"`
		Duration      string   `json:"duration"`
		EpisodesCount string   `json:"episodes_count"`
		ReleaseYear   string   `json:"release_year"`
	} `json:"subject"`
}

// parseSubjectAbstract 把 JSON 兜底数据填进 DoubanInfo。
// 评价人数、编剧、语言、又名、IMDb、海报在这个接口里没有，留空即可：
// db.UpsertDoubanInfo 对所有空字段都是“保留数据库里的旧值”，不会把已有数据冲掉。
func parseSubjectAbstract(subjectID, body string) (*DoubanInfo, error) {
	var payload subjectAbstract
	if err := json.Unmarshal([]byte(strings.TrimSpace(body)), &payload); err != nil {
		return nil, apperror.Wrap(apperror.Corrupt, err, fmt.Sprintf("decode subject_abstract for %s", subjectID))
	}
	s := payload.Subject
	if s.Rate == "" && len(s.Directors) == 0 && len(s.Actors) == 0 {
		return nil, apperror.Newf(apperror.Corrupt, "subject_abstract for %s has no usable fields", subjectID)
	}
	return &DoubanInfo{
		SubjectID:    subjectID,
		Title:        strings.TrimSpace(s.Title),
		Rating:       strings.TrimSpace(s.Rate),
		Director:     strings.Join(s.Directors, " / "),
		Actor:        strings.Join(s.Actors, " / "),
		Genre:        strings.Join(s.Types, "/"),
		Country:      strings.TrimSpace(s.Region),
		ReleaseDate:  strings.TrimSpace(s.ReleaseYear),
		EpisodeCount: strings.TrimSpace(s.EpisodesCount),
		Duration:     strings.TrimSpace(s.Duration),
	}, nil
}

// fetchDetailByAbstract 在详情页被反爬拦截时改走 JSON 接口。
// ignoreBlock 是必须的：详情页那次 302 已经给自己记了一轮静默期，
// 若连兜底请求都被本地闸门挡住，这条路径永远不会执行。
func fetchDetailByAbstract(subjectID string) (*DoubanInfo, error) {
	body, err := fetchDouban(fmt.Sprintf(subjectAbstractURL, subjectID), true)
	if err != nil {
		return nil, err
	}
	info, err := parseSubjectAbstract(subjectID, body)
	if err != nil {
		return nil, err
	}
	applog.Info("[Douban] 详情页被拦截，已用 subject_abstract 兜底: %s Rating='%s' Director='%s' Actor='%s' Genre='%s'",
		subjectID, info.Rating, truncate(info.Director, 30), truncate(info.Actor, 30), info.Genre)
	return info, nil
}

func ParseDetail(subjectID string) (*DoubanInfo, error) {
	applog.Info("[Douban] Parsing detail for subject ID: %s", subjectID)

	if !detailProbeAllowed() {
		applog.Info("[Douban] 详情页仍在拦截期内（每 %s 才试探一次网页），本次直接用 subject_abstract: %s", detailProbeInterval, subjectID)
		return fetchDetailByAbstract(subjectID)
	}

	urlStr := fmt.Sprintf(detailURL, subjectID)

	html, err := fetchHTML(urlStr)
	if err != nil {
		// /subject/<id>/ 网页层面无条件被 302 到 sec.douban.com 的 JS 验证页，
		// 不带登录 Cookie 就永远拿不到。这里降级到同站 JSON 接口，而不是让整次
		// 更新失败——评分/导演/演员/类型/集数这些主要字段都还能拿到。
		if isRemoteAntiCrawl(err) {
			markDetailChallenged()
		}
		applog.Info("[Douban] Detail page unavailable for %s (%v), falling back to subject_abstract", subjectID, err)
		info, fallbackErr := fetchDetailByAbstract(subjectID)
		if fallbackErr != nil {
			applog.Warn("[Douban] Detail fetch failed for %s: %v (fallback: %v)", subjectID, err, fallbackErr)
			return nil, err
		}
		return info, nil
	}
	clearDetailChallenged()

	info := &DoubanInfo{
		SubjectID: subjectID,
	}

	if matches := ratingRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Rating = strings.TrimSpace(matches[1])
	}

	if matches := votesRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Votes = strings.TrimSpace(matches[1])
	}

	// 解析短评数量
	if matches := shortCommentsRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.ShortComments = strings.ReplaceAll(strings.TrimSpace(matches[1]), ",", "")
	}
	// 短评数量备选：从 "全部 XX 条短评" 格式提取
	if info.ShortComments == "" {
		if matches := regexp.MustCompile(`全部\s*(\d[\d,]*)\s*条`).FindStringSubmatch(html); len(matches) >= 2 {
			info.ShortComments = strings.ReplaceAll(strings.TrimSpace(matches[1]), ",", "")
		}
	}

	if matches := directorRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Director = ExtractLinkTexts(matches[1])
	}

	if matches := writerRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Writer = ExtractLinkTexts(matches[1])
	}

	if matches := actorRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Actor = ExtractLinkTexts(matches[1])
	}

	if matches := genreRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Genre = strings.TrimSpace(matches[1])
	}

	if matches := countryRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Country = strings.TrimSpace(matches[1])
	}

	if matches := languageRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Language = strings.TrimSpace(matches[1])
	}

	if matches := releaseDateRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.ReleaseDate = strings.TrimSpace(matches[1])
	}

	if matches := episodeCountRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.EpisodeCount = strings.TrimSpace(matches[1])
	}

	if matches := seasonCountRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.SeasonCount = strings.TrimSpace(matches[1])
	}

	if matches := durationRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Duration = strings.TrimSpace(matches[1])
	}

	if matches := akaRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.Aka = strings.TrimSpace(matches[1])
	}

	if matches := imdbRegex.FindStringSubmatch(html); len(matches) >= 2 {
		info.IMDb = strings.TrimSpace(matches[1])
	}

	if matches := posterRegex.FindStringSubmatch(html); len(matches) >= 3 {
		info.PosterURL = strings.TrimSpace(matches[1])
		info.Title = strings.TrimSpace(matches[2])
	}

	// 兜底标题提取：从 <h1> 中的 <span property="v:itemreviewed"> 提取
	if info.Title == "" {
		if matches := titleRegex.FindStringSubmatch(html); len(matches) >= 2 {
			info.Title = strings.TrimSpace(matches[1])
		}
	}
	if info.Title == "" {
		if matches := ogTitleRegex.FindStringSubmatch(html); len(matches) >= 2 {
			info.Title = cleanTitle(matches[1])
		}
	}

	// 兜底解析：如果以上正则都没匹配到关键字段，尝试从 <div id="info"> 中逐行解析
	parseInfoDivFallback(html, info)
	if info.Title == "" && info.Rating == "" && info.Votes == "" && info.PosterURL == "" {
		return nil, apperror.Newf(apperror.Corrupt, "detail page returned no recognized fields for subject %s", subjectID)
	}

	// 计算热度：votes + short_comments + 7天内新片加权
	info.Hotness = computeHotness(info.Votes, info.ShortComments, info.ReleaseDate)

	applog.Info("[Douban] Parsed detail for %s: Title='%s', Rating='%s', Votes='%s', ShortComments='%s', Hotness='%s', Director='%s', Actor='%s', Genre='%s'",
		subjectID, info.Title, info.Rating, info.Votes, info.ShortComments, info.Hotness, truncate(info.Director, 30), truncate(info.Actor, 30), info.Genre)

	return info, nil
}

// computeHotness 计算热度分数
// 公式：votes + short_comments_count + 7天内新片加杈100 + 30天内新片加权50
func computeHotness(votesStr, shortCommentsStr, releaseDateStr string) string {
	hotness := 0
	if v, err := strconv.Atoi(votesStr); err == nil {
		hotness += v
	}
	if sc, err := strconv.Atoi(shortCommentsStr); err == nil {
		hotness += sc
	}
	// 时间加权：解析首播日期判断是否为新片
	if releaseDateStr != "" {
		// 尝试解析日期：格式如 "2026-06-20(中国大陆)" 或 "2026-05-15"
		dateStr := releaseDateStr
		// 取第一个日期
		if idx := strings.Index(dateStr, "("); idx > 0 {
			dateStr = dateStr[:idx]
		}
		dateStr = strings.TrimSpace(dateStr)
		// 尝试多种日期格式
		for _, layout := range []string{"2006-01-02", "2006/01/02", "2006.01.02"} {
			if t, err := time.Parse(layout, dateStr); err == nil {
				days := time.Since(t).Hours() / 24
				if days < 7 {
					hotness += 100
				} else if days < 30 {
					hotness += 50
				}
				break
			}
		}
	}
	return strconv.Itoa(hotness)
}

// parseInfoDivFallback 从 <div id="info"> 中逐行解析，处理嵌套 span 标签等复杂结构
func parseInfoDivFallback(html string, info *DoubanInfo) {
	idx := strings.Index(html, `<div id="info">`)
	if idx < 0 {
		return
	}
	// 找到 info div 的结束位置
	endMarkers := []string{`<div id="interest_sectl">`, `<script type="text/javascript">`}
	endIdx := len(html)
	for _, marker := range endMarkers {
		if i := strings.Index(html[idx:], marker); i > 0 && idx+i < endIdx {
			endIdx = idx + i
		}
	}
	block := html[idx:endIdx]

	// 按 <span class="pl"> 分割，逐个处理每个标签-值对
	plPattern := regexp.MustCompile(`<span\b[^>]*\bclass\s*=\s*["'][^"']*\bpl\b[^"']*["'][^>]*>([^<]+)</span>`)
	plMatches := plPattern.FindAllStringSubmatchIndex(block, -1)

	for i, m := range plMatches {
		if len(m) < 4 {
			continue
		}
		label := strings.TrimSpace(block[m[2]:m[3]])
		// 值的起始位置：当前 span 结束之后
		valueStart := m[1]
		// 值的结束位置：下一个 <span class="pl"> 或 <br> 或下一个 <span
		var valueEnd int
		if i+1 < len(plMatches) {
			valueEnd = plMatches[i+1][0]
		} else {
			valueEnd = len(block)
		}
		rawValue := block[valueStart:valueEnd]

		// 截断到第一个 <br> 或 <script 标签
		if brIdx := strings.Index(rawValue, "<br"); brIdx >= 0 {
			rawValue = rawValue[:brIdx]
		}
		if scriptIdx := strings.Index(rawValue, "<script"); scriptIdx >= 0 {
			rawValue = rawValue[:scriptIdx]
		}

		// 剥离所有 HTML 标签，提取纯文本值
		value := stripHTMLTags(rawValue)
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}

		switch {
		case strings.Contains(label, "导演") && info.Director == "":
			info.Director = value
		case strings.Contains(label, "编剧") && info.Writer == "":
			info.Writer = value
		case strings.Contains(label, "主演") && info.Actor == "":
			info.Actor = value
		case strings.Contains(label, "类型") && info.Genre == "":
			info.Genre = value
		case strings.Contains(label, "制片国家/地区") && info.Country == "":
			info.Country = value
		case strings.Contains(label, "语言") && info.Language == "":
			info.Language = value
		case strings.Contains(label, "首播") && info.ReleaseDate == "":
			info.ReleaseDate = value
		case strings.Contains(label, "集数") && info.EpisodeCount == "":
			info.EpisodeCount = value
		case strings.Contains(label, "季数") && info.SeasonCount == "":
			info.SeasonCount = value
		case strings.Contains(label, "单集片长") && info.Duration == "":
			info.Duration = value
		case strings.Contains(label, "又名") && info.Aka == "":
			info.Aka = value
		case strings.Contains(label, "IMDb") && info.IMDb == "":
			info.IMDb = value
		}
	}
}

// stripHTMLTags 去除字符串中的所有 HTML 标签，返回纯文本
func stripHTMLTags(s string) string {
	// 移除所有 <...> 标签
	tagRegex := regexp.MustCompile(`<[^>]*>`)
	result := tagRegex.ReplaceAllString(s, "")
	// 将多个空白字符压缩为一个空格
	spaceRegex := regexp.MustCompile(`\s+`)
	result = spaceRegex.ReplaceAllString(result, " ")
	return strings.TrimSpace(result)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// FetchDoubanInfo 先搜 subject_id 再抓详情页。
// 这里不再重复查冷却：SearchSubjectID 已经按 global_id 查过本行了，第二次查
// 只能按关键词查，等于给同一条记录套上两套不同的冷却口径。
func FetchDoubanInfo(keyword string, meta SearchMeta) (*DoubanInfo, error) {
	applog.Info("[Douban] Fetching complete info for keyword: %s", keyword)

	subjectID, err := SearchSubjectID(keyword, meta)
	if err != nil {
		applog.Error("[Douban] FetchDoubanInfo failed at search step for '%s': %v", keyword, err)
		return nil, err
	}

	return ParseDetail(subjectID)
}
