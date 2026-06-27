package douban

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

// ======================== 豆瓣热榜 ========================

// ChartVideoItem 热榜视频项（直接展示 + 匹配状态）
type ChartVideoItem struct {
	// 基本信息（立即返回）
	SubjectID   string `json:"subject_id"`
	Title       string `json:"title"`
	PosterURL   string `json:"poster_url"`
	Rating      string `json:"rating"`
	Votes       string `json:"votes"`
	Info        string `json:"info"`
	Year        string `json:"year"`
	Area        string `json:"area"`
	Director    string `json:"director"`
	Actors      string `json:"actors"`
	ReleaseDate string `json:"release_date"`
	GlobalID    int    `json:"global_id"`
	// 匹配状态
	Status    string `json:"status"`     // "matched" | "searching" | "not_found"
	SourceKey string `json:"source_key"` // 匹配到的源 key（matched 时有效）
	VodID     string `json:"vod_id"`     // 匹配到的 vod_id（matched 时有效）
}

// DoubanChartItem 热榜中的单条视频
type DoubanChartItem struct {
	SubjectID string `json:"subject_id"`
	Title     string `json:"title"`
	PosterURL string `json:"poster_url"`
	Rating    string `json:"rating"`
	Votes     string `json:"votes"`
	Info      string `json:"info"` // 摘要行（上映日期/演员等）
}

const chartURL = "https://movie.douban.com/chart"

var (
	chartCacheMu   sync.RWMutex
	chartCacheData []DoubanChartItem
	chartCacheTime time.Time
	chartCacheTTL  = 1 * time.Hour

	// 热榜匹配结果缓存（subjectID -> ChartVideoItem）
	chartMatchMu     sync.RWMutex
	chartMatchCache  = make(map[string]*ChartVideoItem)
	chartSearching   = make(map[string]bool) // 正在搜索中的条目

	// 热榜解析正则
	chartItemRegex   = regexp.MustCompile(`<tr class="item">([\s\S]*?)</tr>`)
	chartLinkRegex   = regexp.MustCompile(`href="https://movie\.douban\.com/subject/(\d+)/?"`)
	chartPosterRegex = regexp.MustCompile(`<img\s+src="([^"]+)"`)
	chartRatingRegex = regexp.MustCompile(`<span class="rating_nums">([\d.]+)</span>`)
	chartVotesRegex  = regexp.MustCompile(`\((\d+)人评价\)`)
	chartTitleRegex  = regexp.MustCompile(`class="pl2"[\s\S]*?<a[^>]*href="https://movie\.douban\.com/subject/\d+/?"[^>]*>\s*([^<\n]+)`)
	chartInfoRegex   = regexp.MustCompile(`<p>([^<]*)</p>`)
)

// fetchChartHTML 热榜专用 HTTP 请求，不走全局 20-60s 限速
func fetchChartHTML(urlStr string) (string, error) {
	applog.Info("[DoubanChart] Fetching URL: %s", urlStr)

	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0")
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read body: %w", err)
	}

	return string(body), nil
}

// FetchDoubanChart 获取豆瓣热门影视榜（带 1 小时缓存）
func FetchDoubanChart() ([]DoubanChartItem, error) {
	chartCacheMu.RLock()
	if chartCacheData != nil && time.Since(chartCacheTime) < chartCacheTTL {
		items := chartCacheData
		chartCacheMu.RUnlock()
		applog.Debug("[DoubanChart] 命中缓存 (%d 条, 缓存时间: %s)", len(items), chartCacheTime.Format("15:04:05"))
		return items, nil
	}
	chartCacheMu.RUnlock()

	chartCacheMu.Lock()
	defer chartCacheMu.Unlock()

	// 双重检查
	if chartCacheData != nil && time.Since(chartCacheTime) < chartCacheTTL {
		return chartCacheData, nil
	}

	applog.Info("[DoubanChart] 缓存过期或为空，开始抓取热榜...")

	html, err := fetchChartHTML(chartURL)
	if err != nil {
		applog.Error("[DoubanChart] 抓取失败: %v", err)
		if chartCacheData != nil {
			applog.Info("[DoubanChart] 降级返回旧缓存 (%d 条)", len(chartCacheData))
			return chartCacheData, nil
		}
		return nil, fmt.Errorf("抓取豆瓣热榜失败: %w", err)
	}

	items := parseDoubanChart(html)
	if len(items) == 0 {
		applog.Warn("[DoubanChart] 解析结果为空 (HTML len=%d)", len(html))
		if chartCacheData != nil {
			return chartCacheData, nil
		}
		return nil, fmt.Errorf("解析豆瓣热榜失败: 未找到任何条目")
	}

	chartCacheData = items
	chartCacheTime = time.Now()
	applog.Info("[DoubanChart] 抓取完成: %d 条热榜数据", len(items))

	// 异步：入库 + 更新热度 + 搜索源站
	go func() {
		upsertChartItems(items)
		go updateChartHotness(items)
		go asyncMatchChartItems(items)
	}()

	return items, nil
}

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

func upsertChartItems(items []DoubanChartItem) {
	var existingCount int
	if err := db.DB().Get(&existingCount, "SELECT COUNT(*) FROM global_video"); err != nil {
		applog.Warn("[DoubanChart] 数据库查询失败: %v", err)
		return
	}
	applog.Info("[DoubanChart] 开始入库 %d 条热榜数据 (当前 global_video 总数: %d)", len(items), existingCount)

	newCount := 0
	updateCount := 0
	for _, item := range items {
		if item.SubjectID == "" || item.Title == "" {
			continue
		}
		rating := item.Rating
		if !isValidRating(rating) {
			rating = "" // 无效评分（0.0 等）置空，不写入数据库
		}
		year, area, releaseDate, cast := parseInfoFull(item.Info)
		var director, actor string
		if cast != "" {
			parts := strings.SplitN(cast, " / ", 2)
			director = parts[0]
			if len(parts) > 1 {
				actor = parts[1]
			}
		}

		// 1) 尝试归一化匹配已有记录（避免重复创建）
		globalID, err := db.GetOrCreateGlobalID(item.Title, 0)
		if err != nil {
			applog.Warn("[DoubanChart] GetOrCreateGlobalID 失败 title=%s: %v, 直接新增", item.Title, err)
			globalID = 0
		}

		if globalID <= 0 {
			// 2) 匹配不上 → 说明数据库中没有这条数据，直接新增（使用 INSERT OR IGNORE 避免 UNIQUE 冲突）
			_, insertErr := db.DB().Exec(
				`INSERT OR IGNORE INTO global_video (vod_name, year, area, director, actor, release_date, douban_id, douban_score, douban_votes, pic, updated_at)
				 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
				item.Title, year, area, director, actor, releaseDate, item.SubjectID, rating, item.Votes, item.PosterURL)
			if insertErr != nil {
				applog.Warn("[DoubanChart] INSERT 新增失败 title=%s: %v", item.Title, insertErr)
				continue
			}
			// 通过归一化查询获取已存在或新插入的 ID
			var newGlobalID int64
			db.DB().Get(&newGlobalID, `SELECT id FROM global_video WHERE vod_name = ? LIMIT 1`, item.Title)
			if newGlobalID <= 0 {
				applog.Warn("[DoubanChart] INSERT 后未找到记录 title=%s", item.Title)
				continue
			}
			globalID = newGlobalID
			newCount++
			applog.Info("[DoubanChart] 新增 global_video: id=%d title=%s year=%s area=%s director=%s", globalID, item.Title, year, area, director)
		} else {
			// 已有记录，补充所有可解析的字段（只填空缺值）
			_, err = db.DB().Exec(`UPDATE global_video SET 
				douban_id = CASE WHEN douban_id = '' THEN ? ELSE douban_id END,
				douban_score = CASE WHEN ? != '' THEN ? ELSE douban_score END,
				douban_votes = CASE WHEN ? != '' THEN ? ELSE douban_votes END,
				pic = CASE WHEN pic = '' AND ? != '' THEN ? ELSE pic END,
				year = CASE WHEN year = '' AND ? != '' THEN ? ELSE year END,
				area = CASE WHEN area = '' AND ? != '' THEN ? ELSE area END,
				director = CASE WHEN director = '' AND ? != '' THEN ? ELSE director END,
				actor = CASE WHEN actor = '' AND ? != '' THEN ? ELSE actor END,
				release_date = CASE WHEN release_date = '' AND ? != '' THEN ? ELSE release_date END,
				updated_at = CURRENT_TIMESTAMP
				WHERE id = ?`,
				item.SubjectID,
				rating, rating,
				item.Votes, item.Votes,
				item.PosterURL, item.PosterURL,
				year, year,
				area, area,
				director, director,
				actor, actor,
				releaseDate, releaseDate,
				globalID)
			if err != nil {
				applog.Warn("[DoubanChart] 更新豆瓣字段失败 id=%d title=%s: %v", globalID, item.Title, err)
			} else {
				updateCount++
			}
		}
	}

	var totalCount int
	db.DB().Get(&totalCount, "SELECT COUNT(*) FROM global_video")
	applog.Info("[DoubanChart] 入库完成: 新增 %d + 更新 %d = 处理 %d/%d 条 (global_video: %d → %d)",
		newCount, updateCount, newCount+updateCount, len(items), existingCount, totalCount)
}



// asyncMatchChartItems 并行匹配源站数据
func asyncMatchChartItems(items []DoubanChartItem) {
	sources, err := db.GetEnabledSources()
	if err != nil {
		applog.Warn("[DoubanChart] 获取采集源失败: %v", err)
	}

	// 诊断日志：打印所有源状态，便于排查为什么 sources=0
	allSources, allErr := db.GetAllSources()
	if allErr != nil {
		applog.Warn("[DoubanChart] 查询所有源失败: %v", allErr)
	} else {
		for _, s := range allSources {
			applog.Info("[DoubanChart] 源状态: key=%s name=%s enabled=%d apiUrl=%s", s.SourceKey, s.Name, s.Enabled, s.ApiUrl[:min(len(s.ApiUrl), 60)])
		}
		applog.Info("[DoubanChart] 源统计: 总共 %d 个, 启用 %d 个", len(allSources), len(sources))
	}

	if len(sources) == 0 {
		// Fallback: 没有显式启用的源时，使用所有源（避免因前端保存时 enabled 丢失导致完全无法匹配）
		allSources, allErr := db.GetAllSources()
		if allErr != nil || len(allSources) == 0 {
			applog.Info("[DoubanChart] 没有可用的采集源，无法匹配视频资源。请在[源管理]中添加至少一个采集源。")
			markAllNotFound(items)
			return
		}
		applog.Info("[DoubanChart] 没有显式启用的采集源，自动使用全部 %d 个源进行匹配（建议在源管理中启用常用源）", len(allSources))
		sources = allSources
	}

	// 获取用户设置的默认源，将其排到最前面优先匹配
	defaultKey, _ := db.GetSetting("default_source_key")
	if defaultKey != "" {
		reordered := make([]*model.Source, 0, len(sources))
		for _, s := range sources {
			if s.SourceKey == defaultKey {
				reordered = append([]*model.Source{s}, reordered...)
			} else {
				reordered = append(reordered, s)
			}
		}
		sources = reordered
		applog.Info("[DoubanChart] 默认源 %s 已优先排序", defaultKey)
	}

	applog.Info("[DoubanChart] 开始源站匹配: %d 个热榜条目, %d 个采集源", len(items), len(sources))

	var wg sync.WaitGroup
	matched := 0
	for _, item := range items {
		if item.SubjectID == "" || item.Title == "" {
			continue
		}
		wg.Add(1)
		go func(ci DoubanChartItem) {
			defer wg.Done()
			matchChartItemToSource(ci, sources)
		}(item)
	}
	wg.Wait()
	// 统计匹配结果
	chartMatchMu.RLock()
	for _, ci := range items {
		if cached, ok := chartMatchCache[ci.SubjectID]; ok && cached.Status == "matched" {
			matched++
		}
	}
	chartMatchMu.RUnlock()
	applog.Info("[DoubanChart] 源站匹配完成: 成功 %d/%d", matched, len(items))
}

// matchChartItemToSource 匹配单个热榜条目到源站（本地搜索 + 源站搜索）
func matchChartItemToSource(item DoubanChartItem, sources []*model.Source) {
	title := item.Title

	// 标记为搜索中
	chartMatchMu.Lock()
	if existing, ok := chartMatchCache[item.SubjectID]; ok && existing.Status == "matched" {
		chartMatchMu.Unlock()
		return
	}
	chartSearching[item.SubjectID] = true
	chartMatchMu.Unlock()

	// 1. 先查本地数据库（通过视频名称在各源表中搜索）
	for _, src := range sources {
		vodID, found := db.SearchVideoInSourceTable(src.SourceKey, title)
		if found {
			applog.Info("[DoubanChart] 本地匹配成功: %s -> %s/%s", title, src.SourceKey, vodID)
			saveMatchResult(item, src.SourceKey, vodID)
			return
		}
	}
	applog.Debug("[DoubanChart] 本地无匹配，尝试源站搜索: %s (%d个源)", title, len(sources))

	// 2. 本地无数据，尝试从第一个可用源站搜索
	for _, src := range sources {
		vodID, found := searchAndCollectFromSource(src, title)
		if found {
			saveMatchResult(item, src.SourceKey, vodID)
			return
		}
	}

	// 3. 源站也无数据
	chartMatchMu.Lock()
	delete(chartSearching, item.SubjectID)
	built := buildChartItem(item, "not_found", "", "")
	chartMatchCache[item.SubjectID] = &built
	chartMatchMu.Unlock()
	applog.Debug("[DoubanChart] 源站无数据: %s", title)
}

// searchAndCollectFromSource 从源站搜索视频并入库，返回匹配的 vod_id
func searchAndCollectFromSource(src *model.Source, title string) (string, bool) {
	applog.Info("[DoubanChart] 开始源站搜索 src=%s apiUrl=%s title=%s", src.SourceKey, src.ApiUrl, title)
	// 直接通过源站 API 搜索（不走事件通知）
	advCfg := src.GetAdvConfig()
	opts := collect.FetchOptions{
		Limit:        5,
		Keyword:      title,
		FieldMapping: advCfg.FieldMapping,
	}
	page, err := collect.FetchPageWithOpts(src.ApiUrl, 1, opts)
	if err != nil {
		applog.Warn("[DoubanChart] 源站搜索失败 src=%s title=%s: %v", src.SourceKey, title, err)
		return "", false
	}
	if page == nil || len(page.List) == 0 {
		total := 0
		if page != nil { total = page.Total.Int() }
		applog.Info("[DoubanChart] 源站无结果 src=%s title=%s (total=%d)", src.SourceKey, title, total)
		return "", false
	}
	applog.Info("[DoubanChart] 源站搜索结果 src=%s title=%s: 找到 %d 条", src.SourceKey, title, len(page.List))

	// 查找名称匹配的结果
	for _, v := range page.List {
		if v == nil || v.VodName == "" {
			continue
		}
		applog.Debug("[DoubanChart] 候选视频: vod_id=%s vod_name=%q (匹配目标: %q)", v.VodId, v.VodName, title)
		// 名称匹配（精确或高相似度）
		if v.VodName == title || strings.Contains(v.VodName, title) || strings.Contains(title, v.VodName) {
			applog.Info("[DoubanChart] 名称匹配: %q 匹配 %q (src=%s)", title, v.VodName, src.SourceKey)
			// 获取详情并入库
			detail, detailErr := collect.FetchVideoDetail(src.ApiUrl, v.VodId.String())
			if detailErr == nil && detail != nil {
				if detail.VodActor != "" { v.VodActor = detail.VodActor }
				if detail.VodDirector != "" { v.VodDirector = detail.VodDirector }
				if detail.VodContent != "" { v.VodContent = detail.VodContent }
				if detail.VodPic != "" { v.VodPic = detail.VodPic }
				if detail.VodPlayUrl != "" { v.VodPlayUrl = detail.VodPlayUrl }
			} else if detailErr != nil {
				applog.Warn("[DoubanChart] 获取详情失败 vod_id=%s: %v (使用列表数据继续)", v.VodId, detailErr)
			}
			v.VodContent = collect.CleanHTML(v.VodContent)
			v.VodContent = collect.CompressTextField(v.VodContent)
			v.VodActor = collect.CleanHTML(v.VodActor)
			v.VodActor = collect.CompressTextField(v.VodActor)
			v.VodDirector = collect.CleanHTML(v.VodDirector)
			v.VodDirector = collect.CompressTextField(v.VodDirector)
			v.VodPlayUrl = collect.CompressTextField(v.VodPlayUrl)

			if err := db.EnsureVideoTable(src.SourceKey); err != nil {
				applog.Error("[DoubanChart] 创建视频表失败 src=%s: %v", src.SourceKey, err)
				return "", false
			}
			if err := db.MergeVideoDetails(src.SourceKey, []*model.Video{v}); err != nil {
				applog.Error("[DoubanChart] 入库失败 src=%s title=%s: %v", src.SourceKey, v.VodName, err)
				return "", false
			}
			applog.Info("[DoubanChart] 源站采集入库成功: %s -> %s/%s", title, src.SourceKey, v.VodId.String())
			return v.VodId.String(), true
		}
	}
	applog.Debug("[DoubanChart] 源站有结果但无名称匹配 src=%s title=%s", src.SourceKey, title)
	return "", false
}

// saveMatchResult 保存匹配结果到缓存
func saveMatchResult(item DoubanChartItem, sourceKey, vodID string) {
	built := buildChartItem(item, "matched", sourceKey, vodID)
	chartMatchMu.Lock()
	chartMatchCache[item.SubjectID] = &built
	delete(chartSearching, item.SubjectID)
	chartMatchMu.Unlock()
	applog.Debug("[DoubanChart] 匹配成功: %s -> %s/%s", item.Title, sourceKey, vodID)
}

// markAllNotFound 标记所有条目为 not_found
func markAllNotFound(items []DoubanChartItem) {
	chartMatchMu.Lock()
	defer chartMatchMu.Unlock()
	for _, item := range items {
		built := buildChartItem(item, "not_found", "", "")
		chartMatchCache[item.SubjectID] = &built
	}
}

// GetChartVideos 获取热榜视频列表（立即返回，带匹配状态）
func GetChartVideos() ([]ChartVideoItem, error) {
	// 1. 获取热榜数据
	chartItems, err := FetchDoubanChart()
	if err != nil {
		return nil, fmt.Errorf("获取热榜失败: %w", err)
	}
	if len(chartItems) == 0 {
		return nil, nil
	}

	// 2. 构建返回结果（从缓存中获取匹配状态）
	chartMatchMu.RLock()
	defer chartMatchMu.RUnlock()

	var result []ChartVideoItem
	for _, ci := range chartItems {
		if cached, ok := chartMatchCache[ci.SubjectID]; ok {
			result = append(result, *cached)
		} else {
			result = append(result, buildChartItem(ci, "searching", "", ""))
		}
	}

	return result, nil
}

// buildChartItem 从 DoubanChartItem 构建完整的 ChartVideoItem
func buildChartItem(ci DoubanChartItem, status, sourceKey, vodID string) ChartVideoItem {
	year, area, releaseDate, cast := parseInfoFull(ci.Info)
	var director, actors string
	if cast != "" {
		parts := strings.SplitN(cast, " / ", 2)
		director = parts[0]
		if len(parts) > 1 {
			actors = parts[1]
		}
	}
	return ChartVideoItem{
		SubjectID:   ci.SubjectID,
		Title:       ci.Title,
		PosterURL:   ci.PosterURL,
		Rating:      ci.Rating,
		Votes:       ci.Votes,
		Info:        ci.Info,
		Year:        year,
		Area:        area,
		Director:    director,
		Actors:      actors,
		ReleaseDate: releaseDate,
		GlobalID:    db.GetGlobalIDByDoubanSubject(ci.SubjectID),
		Status:      status,
		SourceKey:   sourceKey,
		VodID:       vodID,
	}
}

// ResolveChartVideo 用户点击时解析热榜视频（实时检查匹配状态）
func ResolveChartVideo(subjectID string) (*ChartVideoItem, error) {
	if subjectID == "" {
		return nil, fmt.Errorf("subject_id 不能为空")
	}

	chartMatchMu.RLock()
	cached, ok := chartMatchCache[subjectID]
	searching := chartSearching[subjectID]
	chartMatchMu.RUnlock()

	if ok && cached.Status == "matched" {
		return cached, nil
	}
	if searching {
		// 正在搜索中
		return &ChartVideoItem{SubjectID: subjectID, Status: "searching"}, nil
	}

	// 尝试实时匹配
	sources, err := db.GetEnabledSources()
	if err != nil || len(sources) == 0 {
		return &ChartVideoItem{SubjectID: subjectID, Status: "not_found"}, nil
	}

	// 查找热榜缓存中的标题
	chartCacheMu.RLock()
	var title string
	for _, ci := range chartCacheData {
		if ci.SubjectID == subjectID {
			title = ci.Title
			break
		}
	}
	chartCacheMu.RUnlock()

	if title == "" {
		return &ChartVideoItem{SubjectID: subjectID, Status: "not_found"}, nil
	}

	// 实时搜索各源
	for _, src := range sources {
		vodID, found := db.SearchVideoInSourceTable(src.SourceKey, title)
		if found {
			item := &ChartVideoItem{
				SubjectID: subjectID,
				Title:     title,
				Status:    "matched",
				SourceKey: src.SourceKey,
				VodID:     vodID,
				GlobalID:  db.GetGlobalIDByDoubanSubject(subjectID),
			}
			// 更新缓存
			chartMatchMu.Lock()
			chartMatchCache[subjectID] = item
			chartMatchMu.Unlock()
			return item, nil
		}
	}

	return &ChartVideoItem{SubjectID: subjectID, Title: title, Status: "not_found"}, nil
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

// updateChartHotness 异步更新热榜条目的热度到数据库
func updateChartHotness(items []DoubanChartItem) {
	for _, item := range items {
		if item.SubjectID == "" {
			continue
		}

		hotness := 100

		releaseDate := parseReleaseDate(item.Info)
		if !releaseDate.IsZero() {
			days := time.Since(releaseDate).Hours() / 24
			switch {
			case days < 7:
				hotness += 200
			case days < 30:
				hotness += 100
			case days < 90:
				hotness += 50
			default:
				hotness += 20
			}
		}

		if v, err := strconv.Atoi(item.Votes); err == nil {
			hotness += v
		}

		if r, err := strconv.ParseFloat(item.Rating, 64); err == nil {
			hotness += int(r * 20)
		}

		globalID := db.GetGlobalIDByDoubanSubject(item.SubjectID)
		if globalID > 0 {
			if err := db.UpdateDoubanHotness(globalID, strconv.Itoa(hotness)); err != nil {
				applog.Debug("[DoubanChart] 更新热度失败 (subject=%s): %v", item.SubjectID, err)
			}
		}
	}
}

// ClearChartCache 清除热榜缓存
func ClearChartCache() {
	chartCacheMu.Lock()
	chartCacheData = nil
	chartCacheTime = time.Time{}
	chartCacheMu.Unlock()

	chartMatchMu.Lock()
	chartMatchCache = make(map[string]*ChartVideoItem)
	chartSearching = make(map[string]bool)
	chartMatchMu.Unlock()

	applog.Info("[DoubanChart] 缓存已清除")
}
