package douban

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
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
	// Avoid immediately repeating the same blocked request when startup
	// preload and the Home view ask for the chart close together.
	chartFailureTime    time.Time
	chartFailureBackoff = 5 * time.Minute

	// 热榜匹配结果缓存（subjectID -> ChartVideoItem）
	chartMatchMu    sync.RWMutex
	chartMatchCache = make(map[string]*ChartVideoItem)
	chartSearching  = make(map[string]bool) // 正在搜索中的条目

	// 热榜解析正则
	chartItemRegex   = regexp.MustCompile(`<tr class="item">([\s\S]*?)</tr>`)
	chartLinkRegex   = regexp.MustCompile(`href="https://movie\.douban\.com/subject/(\d+)/?"`)
	chartPosterRegex = regexp.MustCompile(`<img\s+src="([^"]+)"`)
	chartRatingRegex = regexp.MustCompile(`<span class="rating_nums">([\d.]+)</span>`)
	chartVotesRegex  = regexp.MustCompile(`\((\d+)人评价\)`)
	chartTitleRegex  = regexp.MustCompile(`class="pl2"[\s\S]*?<a[^>]*href="https://movie\.douban\.com/subject/\d+/?"[^>]*>\s*([^<\n]+)`)
	chartInfoRegex   = regexp.MustCompile(`<p>([^<]*)</p>`)

	// 热榜匹配并发上限：每个条目都会对全部源发一次远程搜索，不限流就是一次风暴。
	chartMatchSlots   = make(chan struct{}, 3)
	chartMatchRunning atomic.Bool
)

// fetchChartHTML 热榜专用 HTTP 请求：静默期和全局限速闸门都必须走，
// 等不起就直接跳过这次刷新，首页继续用旧热榜。
func fetchChartHTML(urlStr string) (string, error) {
	if left := remainingBlock(); left > 0 {
		applog.Warn("[DoubanChart] 反爬静默中（剩余 %s），跳过热榜请求", left.Round(time.Second))
		return "", fmt.Errorf("douban 反爬静默中，剩余 %s", left.Round(time.Minute))
	}
	// 热榜以前只尊重静默期、不限速，理由是「一小时缓存 + 失败退避，请求量极低」。
	// 但缓存一过期撞上批量补全的间隙，就是一次裸请求插在两条搜索之间——豆瓣按 IP
	// 计数，不认得谁是谁。
	if _, ok := awaitDoubanSlot("热榜", uiWaitBudget); !ok {
		return "", fmt.Errorf("douban 限速中，跳过热榜刷新")
	}

	applog.Info("[DoubanChart] Fetching URL: %s", urlStr)

	doc, location, err := chartGet(urlStr)
	if isSecChallengeURL(location) {
		// 和详情页是同一道题：解掉再重试，别把能过的请求记成封禁。
		if serr := solveDoubanChallenge(location, urlStr); serr != nil {
			applog.Warn("[DoubanChart] 自动通过验证失败：%v", serr)
			noteAntiCrawl()
			return "", err
		}
		doc, location, err = chartGet(urlStr)
	}
	if err != nil {
		if isDoubanChallengeURL(location) {
			applog.Warn("[DoubanChart] 命中验证跳转 -> %s", location)
			noteAntiCrawl()
		}
		return "", err
	}
	noteDoubanSuccess()
	return doc, nil
}

// chartGet 发一次热榜 GET。location 非空表示被重定向，交给调用方判断能否解题。
func chartGet(urlStr string) (doc string, location string, err error) {
	req, err := http.NewRequest("GET", urlStr, nil)
	if err != nil {
		return "", "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0")
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("failed to fetch: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", resp.Header.Get("Location"), fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read body: %w", err)
	}

	return string(body), "", nil
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
	if !chartFailureTime.IsZero() && time.Since(chartFailureTime) < chartFailureBackoff {
		if len(chartCacheData) > 0 {
			return chartCacheData, nil
		}
		if fallback := loadPersistedChartItems(); len(fallback) > 0 {
			chartCacheData = fallback
			return fallback, nil
		}
		return nil, fmt.Errorf("豆瓣热榜暂不可用，等待重试冷却结束")
	}

	// 双重检查
	if chartCacheData != nil && time.Since(chartCacheTime) < chartCacheTTL {
		return chartCacheData, nil
	}

	applog.Info("[DoubanChart] 缓存过期或为空，开始抓取热榜...")

	html, err := fetchChartHTML(chartURL)
	if err != nil {
		applog.Error("[DoubanChart] 抓取失败: %v", err)
		chartFailureTime = time.Now()
		if chartCacheData != nil {
			applog.Info("[DoubanChart] 降级返回旧缓存 (%d 条)", len(chartCacheData))
			return chartCacheData, nil
		}
		if fallback := loadPersistedChartItems(); len(fallback) > 0 {
			chartCacheData = fallback
			applog.Info("[DoubanChart] 降级返回数据库热榜缓存 (%d 条)", len(fallback))
			return fallback, nil
		}
		return nil, fmt.Errorf("抓取豆瓣热榜失败: %w", err)
	}

	items := parseDoubanChart(html)
	if len(items) == 0 {
		applog.Warn("[DoubanChart] 解析结果为空 (HTML len=%d)", len(html))
		chartFailureTime = time.Now()
		if chartCacheData != nil {
			return chartCacheData, nil
		}
		if fallback := loadPersistedChartItems(); len(fallback) > 0 {
			chartCacheData = fallback
			applog.Info("[DoubanChart] 解析为空，降级返回数据库热榜缓存 (%d 条)", len(fallback))
			return fallback, nil
		}
		return nil, fmt.Errorf("解析豆瓣热榜失败: 未找到任何条目")
	}

	chartCacheData = items
	chartCacheTime = time.Now()
	chartFailureTime = time.Time{}
	applog.Info("[DoubanChart] 抓取完成: %d 条热榜数据", len(items))

	// 异步：入库 + 更新热度 + 搜索源站。启动预加载和首页可能几乎同时触发抓取，
	// 因此同一时刻只跑一轮匹配，后到的一轮直接放弃（缓存与匹配结果已共享）。
	go func() {
		upsertChartItems(items)
		go updateChartHotness(items)
		if !chartMatchRunning.CompareAndSwap(false, true) {
			applog.Info("[DoubanChart] 已有匹配在跑，跳过本轮")
			return
		}
		defer chartMatchRunning.Store(false)
		asyncMatchChartItems(items)
	}()

	return items, nil
}

func loadPersistedChartItems() []DoubanChartItem {
	rows, err := db.GetCachedDoubanChartVideos(20)
	if err != nil {
		applog.Warn("[DoubanChart] 读取数据库热榜缓存失败: %v", err)
		return nil
	}
	return chartItemsFromGlobalRows(rows)
}

func chartItemsFromGlobalRows(rows []db.GlobalVideoRow) []DoubanChartItem {
	items := make([]DoubanChartItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, DoubanChartItem{
			SubjectID: row.DoubanId,
			Title:     row.VodName,
			PosterURL: row.Pic,
			Rating:    row.DoubanScore,
			Votes:     row.DoubanVotes,
			Info:      strings.Trim(strings.Join([]string{row.ReleaseDate, row.Area}, " / "), " /"),
		})
	}
	return items
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

	rows := make([]db.ChartDoubanUpdate, 0, len(items))
	for _, item := range items {
		if item.SubjectID == "" || item.Title == "" {
			continue
		}
		rating := item.Rating
		if !isValidRating(rating) {
			rating = "" // 无效评分（0.0 等）置空，不写入数据库
		}
		year, area, releaseDate, cast := parseInfoFull(item.Info)
		_ = cast
		rows = append(rows, db.ChartDoubanUpdate{
			SubjectID:   item.SubjectID,
			Title:       item.Title,
			Year:        year,
			Area:        area,
			ReleaseDate: releaseDate,
			Rating:      rating,
			Votes:       item.Votes,
			PosterURL:   item.PosterURL,
		})
	}

	// 身份判定只有 db 包里一份实现：命中已有条目或新建一条，然后补齐字段。
	// 以前这里在解析失败后自己写一条 INSERT OR IGNORE，绕开了归一化列，
	// 热榜和采集因此能为同一部片各留一行。
	newCount, updateCount, err := db.UpsertChartItems(rows)
	if err != nil {
		applog.Warn("[DoubanChart] 热榜入库失败: %v", err)
		return
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
	// 整轮匹配共用一个身份批次：每个条目只入库一条，逐个重读 global_video 全表
	// 是这轮匹配里最贵的一段。批次自带互斥，多个 goroutine 并发用是安全的。
	batch := db.NewCatalogBatch()
	for _, item := range items {
		if item.SubjectID == "" || item.Title == "" {
			continue
		}
		chartMatchSlots <- struct{}{}
		wg.Add(1)
		go func(ci DoubanChartItem) {
			defer wg.Done()
			defer func() { <-chartMatchSlots }()
			matchChartItemToSource(ci, sources, batch)
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
func matchChartItemToSource(item DoubanChartItem, sources []*model.Source, batch *db.CatalogBatch) {
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
		vodID, found := searchAndCollectFromSource(src, title, batch)
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
func searchAndCollectFromSource(src *model.Source, title string, batch *db.CatalogBatch) (string, bool) {
	applog.Info("[DoubanChart] 开始源站搜索 src=%s apiUrl=%s title=%s", src.SourceKey, src.ApiUrl, title)
	// 直接通过源站 API 搜索（不走事件通知）
	strategy := collect.CreateStrategyFromSource(src)
	if strategy == nil {
		applog.Warn("[DoubanChart] 源站策略不可用 src=%s", src.SourceKey)
		return "", false
	}
	page, err := collect.FetchSearchPage(strategy, title, 1)
	if err != nil {
		applog.Warn("[DoubanChart] 源站搜索失败 src=%s title=%s: %v", src.SourceKey, title, err)
		return "", false
	}
	if page == nil || len(page.List) == 0 {
		total := 0
		if page != nil {
			total = page.Total.Int()
		}
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
			// A chart match is a search result: persist its catalog projection only.
			v.VodContent, v.VodActor, v.VodDirector = "", "", ""
			v.VodPlayUrl, v.VodDownUrl, v.VodPlayFrom = "", "", ""
			if err := db.UpsertCatalogItemsBatch(batch, src.SourceKey, []*model.Video{v}); err != nil {
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
	hotnessBySubject := make(map[string]string, len(items))
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

		hotnessBySubject[item.SubjectID] = strconv.Itoa(hotness)
	}
	if updated, err := db.UpdateDoubanHotnessBatch(hotnessBySubject); err != nil {
		applog.Debug("[DoubanChart] 批量更新热度失败: %v", err)
	} else if updated > 0 {
		applog.Debug("[DoubanChart] 热度已更新 %d 条", updated)
	}
}

// ClearChartCache 清除热榜缓存：抓回来的榜单正文 + 它与本库的匹配结果。
// 正文这一份是有代价的（豆瓣限流），所以只有用户主动"清除缓存"才走到这里。
func ClearChartCache() {
	chartCacheMu.Lock()
	chartCacheData = nil
	chartCacheTime = time.Time{}
	chartCacheMu.Unlock()

	ClearChartMatchCache()
	applog.Info("[DoubanChart] 缓存已清除")
}

// ClearChartMatchCache 只丢掉"热榜条目 → 本库影片"的匹配结果，不动榜单正文。
// 采集/删除/改源改变的是我们库里的行，正文仍是豆瓣侧的内容；清正文会白白多付一次
// 抓取和一轮限流。
func ClearChartMatchCache() {
	chartMatchMu.Lock()
	chartMatchCache = make(map[string]*ChartVideoItem)
	chartMatchMu.Unlock()
}

// ChartMatchCount 返回匹配缓存的条目数，供诊断台确认失效是否真的生效。
func ChartMatchCount() int {
	chartMatchMu.RLock()
	defer chartMatchMu.RUnlock()
	return len(chartMatchCache)
}
