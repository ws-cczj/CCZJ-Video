package douban

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
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

// fetchChartPage 是「取回热榜页面」这一步的替身位：stale-first 的行为必须能在不真打豆瓣的
// 前提下验证，所以抓取入口留一个可替换的包级变量。
var fetchChartPage = fetchChartHTML

// fetchChartHTML 热榜专用 HTTP 请求：静默期和全局限速闸门都必须走，
// 等不起就直接跳过这次刷新，首页继续用旧热榜。
func fetchChartHTML(urlStr string) (string, error) {
	if left := remainingBlock(); left > 0 {
		applog.Warn("[DoubanChart] 反爬静默中（剩余 %s），跳过热榜请求", left.Round(time.Second))
		return "", apperror.Newf(apperror.Unavailable, "douban 反爬静默中，剩余 %s", left.Round(time.Minute))
	}
	// 热榜以前只尊重静默期、不限速，理由是「一小时缓存 + 失败退避，请求量极低」。
	// 但缓存一过期撞上批量补全的间隙，就是一次裸请求插在两条搜索之间——豆瓣按 IP
	// 计数，不认得谁是谁。
	if _, ok := awaitDoubanSlot("热榜", uiWaitBudget); !ok {
		return "", apperror.New(apperror.Unavailable, "douban 限速中，跳过热榜刷新")
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
		return "", "", apperror.Wrap(apperror.Unavailable, err, "failed to create request")
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/149.0.0.0 Safari/537.36 Edg/149.0.0.0")
	req.Header.Set("Referer", "https://movie.douban.com/")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", apperror.Wrap(apperror.Unavailable, err, "failed to fetch")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", resp.Header.Get("Location"), apperror.Newf(apperror.Unavailable, "HTTP %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", apperror.Wrap(apperror.Unavailable, err, "failed to read body")
	}

	return string(body), "", nil
}

// chartStaleCeiling 是「一份旧榜单还值得直接摆上首页」的年龄上限。豆瓣限速可能连着几轮都
// 抢不到槽位，而热榜本身变化很慢——首页宁可显示昨天的榜单，也不要空着等一次网络。超过这个
// 年龄就当没有，回到「手里没东西、只能等一次实抓」的老路。
const chartStaleCeiling = 7 * 24 * time.Hour

// chartFetchMu 只把真正的抓取串起来。它和 chartCacheMu 分开是有意的：以前抓取一路握着缓存
// 写锁，于是一份过期榜单的读要跟着在限速闸门后面等上半分钟；现在读缓存从不被抓取挡住。
var chartFetchMu sync.Mutex

// FetchDoubanChart 获取豆瓣热门影视榜。有能看的旧榜单就立刻返回，过期只决定「要不要在后台
// 补一轮」，绝不把等待压到调用方（首页轮播、启动预加载）身上。
func FetchDoubanChart() ([]DoubanChartItem, error) {
	items, at, ok := chartDisplayable()
	if ok && time.Since(at) < chartCacheTTL {
		chartCounters.hits.Add(1)
		applog.Debug("[DoubanChart] 命中缓存 (%d 条, 缓存时间: %s)", len(items), at.Format("15:04:05"))
		return items, nil
	}
	if ok {
		// 先出旧榜单，后台悄悄补：抓不到（限速排队、反爬静默、解析为空）就继续用旧的。
		chartCounters.staleHits.Add(1)
		go func() { _, _ = refreshChart(false) }()
		return items, nil
	}
	chartCounters.misses.Add(1)
	return refreshChart(true)
}

// chartDisplayable 取出一份还能摆上首页的榜单：内存优先，再看落库快照。快照读回来就填进内存，
// 免得后续每次读都戳一遍 settings。
func chartDisplayable() ([]DoubanChartItem, time.Time, bool) {
	if items, at, ok := chartMemory(); ok && time.Since(at) < chartStaleCeiling {
		return items, at, true
	}
	items, at, ok := loadChartSnapshot()
	if !ok {
		return nil, time.Time{}, false
	}
	chartCacheMu.Lock()
	chartCacheData, chartCacheTime = items, at
	chartCacheMu.Unlock()
	return items, at, true
}

// chartMemory 读内存里那份榜单。没数据、或者时间戳不明（降级路径塞进来的）都算不可展示。
func chartMemory() ([]DoubanChartItem, time.Time, bool) {
	chartCacheMu.RLock()
	defer chartCacheMu.RUnlock()
	if len(chartCacheData) == 0 || chartCacheTime.IsZero() {
		return nil, time.Time{}, false
	}
	return chartCacheData, chartCacheTime, true
}

func chartStaleData() []DoubanChartItem {
	chartCacheMu.RLock()
	defer chartCacheMu.RUnlock()
	return chartCacheData
}

// setChartCacheFallback 只塞数据、不动时间戳：来源不明的数据库缓存可以继续兜底，但不能
// 冒充一份「刚抓来的新鲜榜单」把后面的刷新都免掉。
func setChartCacheFallback(items []DoubanChartItem) {
	chartCacheMu.Lock()
	chartCacheData = items
	chartCacheMu.Unlock()
}

func noteChartFailure() {
	chartCacheMu.Lock()
	chartFailureTime = time.Now()
	chartCacheMu.Unlock()
}

// refreshChart 抓一趟热榜。wait=true 是调用方手里什么都没有、只能等到底；wait=false 是后台
// 静默刷新——已经有一趟在跑就跳过这一轮，绝不排队。抓取本身在 fetchChartHTML 里还有一次
// 「等不起就放弃」，所以豆瓣限速期间首页只是继续用旧榜单，不会积压请求。
func refreshChart(wait bool) ([]DoubanChartItem, error) {
	if wait {
		chartFetchMu.Lock()
	} else if !chartFetchMu.TryLock() {
		chartCounters.skipped.Add(1)
		applog.Debug("[DoubanChart] 已有一轮刷新在跑，跳过本轮后台刷新")
		return nil, nil
	}
	defer chartFetchMu.Unlock()
	// 排队期间另一趟可能刚把榜单补齐，别重复打豆瓣。
	if items, at, ok := chartMemory(); ok && time.Since(at) < chartCacheTTL {
		chartCounters.skipped.Add(1)
		return items, nil
	}
	return fetchAndStoreChart()
}

// fetchAndStoreChart 真正抓一趟并落缓存与快照，由 refreshChart 在 chartFetchMu 之下调用。
func fetchAndStoreChart() ([]DoubanChartItem, error) {
	chartCacheMu.RLock()
	failureAt, cooldownStale := chartFailureTime, chartCacheData
	chartCacheMu.RUnlock()
	if !failureAt.IsZero() && time.Since(failureAt) < chartFailureBackoff {
		chartCounters.skipped.Add(1)
		if len(cooldownStale) > 0 {
			return cooldownStale, nil
		}
		if fallback := loadPersistedChartItems(); len(fallback) > 0 {
			setChartCacheFallback(fallback)
			return fallback, nil
		}
		return nil, apperror.New(apperror.Unavailable, "豆瓣热榜暂不可用，等待重试冷却结束")
	}

	applog.Info("[DoubanChart] 缓存过期或为空，开始抓取热榜...")

	html, err := fetchChartPage(chartURL)
	if err != nil {
		chartCounters.fetchFail.Add(1)
		applog.Error("[DoubanChart] 抓取失败: %v", err)
		noteChartFailure()
		if stale := chartStaleData(); len(stale) > 0 {
			applog.Info("[DoubanChart] 降级返回旧缓存 (%d 条)", len(stale))
			return stale, nil
		}
		if fallback := loadPersistedChartItems(); len(fallback) > 0 {
			setChartCacheFallback(fallback)
			applog.Info("[DoubanChart] 降级返回数据库热榜缓存 (%d 条)", len(fallback))
			return fallback, nil
		}
		return nil, apperror.Wrap(apperror.Unavailable, err, "抓取豆瓣热榜失败")
	}

	items := parseDoubanChart(html)
	if len(items) == 0 {
		chartCounters.fetchFail.Add(1)
		applog.Warn("[DoubanChart] 解析结果为空 (HTML len=%d)", len(html))
		noteChartFailure()
		if stale := chartStaleData(); len(stale) > 0 {
			return stale, nil
		}
		if fallback := loadPersistedChartItems(); len(fallback) > 0 {
			setChartCacheFallback(fallback)
			applog.Info("[DoubanChart] 解析为空，降级返回数据库热榜缓存 (%d 条)", len(fallback))
			return fallback, nil
		}
		return nil, apperror.New(apperror.Corrupt, "解析豆瓣热榜失败: 未找到任何条目")
	}

	chartCacheMu.Lock()
	chartCacheData, chartCacheTime = items, time.Now()
	chartFailureTime = time.Time{}
	fetchedAt := chartCacheTime
	chartCacheMu.Unlock()
	chartCounters.fetchOK.Add(1)
	saveChartSnapshot(items, fetchedAt)
	applog.Info("[DoubanChart] 抓取完成: %d 条热榜数据", len(items))

	// 异步：入库 + 更新热度 + 搜索源站。启动预加载和首页可能几乎同时触发抓取，
	// 因此同一时刻只跑一轮匹配，后到的一轮直接放弃（缓存与匹配结果已共享）。
	go func() {
		// 这一趟脱离了调用方的请求，句柄可能根本还没开（或已经关掉）。sqlx 在 nil 句柄上
		// 是 panic 而不是 error，而在裸 goroutine 里 panic 会带走整个进程，所以先确认在位。
		if db.DB() == nil {
			return
		}
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

// chartSnapshotKey 把一次成功抓取的热榜原样存进 settings。
//
// 热榜缓存原先只有内存一份，进程一重启就归零，于是"每次打开应用必然打豆瓣一次"——
// 一小时 TTL 只对同一个进程内有效。豆瓣按 IP 计数，落库之后 TTL 才真的跨启动生效。
const chartSnapshotKey = "douban_chart_snapshot"

type chartSnapshot struct {
	FetchedAt int64             `json:"fetched_at"`
	Items     []DoubanChartItem `json:"items"`
}

// loadChartSnapshot 读回上次抓到的热榜。判定的是「还能不能摆上首页」，不是「新不新鲜」：
// 新鲜与否由 FetchDoubanChart 拿 chartCacheTTL 比，过期的这份会先展示、后台再去补。
// 缺数据、读坏、老过 chartStaleCeiling 才算未命中——那种榜单已经没有参考价值了。
func loadChartSnapshot() ([]DoubanChartItem, time.Time, bool) {
	raw, err := db.GetSetting(chartSnapshotKey)
	if err != nil || raw == "" {
		return nil, time.Time{}, false
	}
	var snap chartSnapshot
	if json.Unmarshal([]byte(raw), &snap) != nil || snap.FetchedAt <= 0 || len(snap.Items) == 0 {
		return nil, time.Time{}, false
	}
	at := time.Unix(snap.FetchedAt, 0)
	if time.Since(at) >= chartStaleCeiling {
		return nil, time.Time{}, false
	}
	return snap.Items, at, true
}

// saveChartSnapshot 写快照。失败只记日志：缓存没落成，下次顶多多抓一趟。
func saveChartSnapshot(items []DoubanChartItem, at time.Time) {
	raw, err := json.Marshal(chartSnapshot{FetchedAt: at.Unix(), Items: items})
	if err != nil {
		return
	}
	if err := db.SetSetting(chartSnapshotKey, string(raw)); err != nil {
		applog.Warn("[DoubanChart] 热榜快照落库失败（下次启动会重新抓取）: %v", err)
	}
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

// GetChartVideos 获取热榜视频列表（立即返回，带匹配状态）
func GetChartVideos() ([]ChartVideoItem, error) {
	// 1. 获取热榜数据
	chartItems, err := FetchDoubanChart()
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, "获取热榜失败")
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
		return nil, apperror.New(apperror.Validation, "subject_id 不能为空")
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
	// 落库快照也要一起丢，否则清完下一次启动又把它读回来，等于没清。
	_ = db.SetSetting(chartSnapshotKey, "")

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
