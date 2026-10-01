package douban

import (
	"strings"
	"sync"

	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
)

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
		// allSources 上面诊断日志已经取过一次，这里直接复用：查同一张表两遍没有意义，
		// 取失败时它的长度为 0，会走到下面的「没有可用的采集源」分支。
		if len(allSources) == 0 {
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
