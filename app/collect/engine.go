package collect

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	appLogger "cczjVideo/app/applog"
)

// Engine 支持暂停/恢复/停止，按页采集，每页间隔 configurable 秒，失败重试
type Engine struct {
	sourceKey  string
	source     *model.Source
	strategy   SourceStrategy // 数据源策略
	onLog      func(msg string)
	onProgress func(current, total int)
	// 新事件：当采集到某页时推送该页的视频名称列表
	onPageNames func(page int, names []string)
	// 采集参数
	mode      model.CollectMode // 采集模式
	timeHours int               // 增量模式的时间窗（小时）

	mu      sync.Mutex
	paused  bool
	stop    bool
	pageGap time.Duration // 页间等待时长
	ctx     context.Context
	// catalogBatch 让整轮采集只载入一遍 global_video 身份，而不是每页重读全表。
	catalogBatch *db.CatalogBatch
}

// EngineOption 引擎可选项
type EngineOption func(*Engine)

// WithCollectMode 设置采集模式
func WithCollectMode(mode model.CollectMode) EngineOption {
	return func(e *Engine) { e.mode = mode }
}

// WithTimeHours 设置增量采集时间窗（小时）
func WithTimeHours(hours int) EngineOption {
	return func(e *Engine) { e.timeHours = hours }
}

// NewEngineV2 额外接收 onPageNames 回调（可选）
func NewEngineV2(
	sourceKey string,
	onLog func(string),
	onProgress func(int, int),
	onPageNames func(int, []string),
	opts ...EngineOption,
) *Engine {
	e := &Engine{
		sourceKey:    sourceKey,
		onLog:        onLog,
		onProgress:   onProgress,
		onPageNames:  onPageNames,
		pageGap:      30 * time.Second,
		mode:         model.CollectModeFull,
		catalogBatch: db.NewCatalogBatch(),
	}
	for _, o := range opts {
		o(e)
	}
	return e
}

// SetContext 注入外部 context（用于等待时感知取消）
func (e *Engine) SetContext(ctx context.Context) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.ctx = ctx
}

// SetPageGap 设置页间等待时长（覆盖默认 30s）
func (e *Engine) SetPageGap(gap time.Duration) {
	if gap <= 0 {
		gap = 30 * time.Second
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pageGap = gap
}

// currentPageGap 读当前页间隔。采集循环每页之间都要读它，而调度器可能在别的
// 线程改设置，裸读是一次真数据竞争。
func (e *Engine) currentPageGap() time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.pageGap
}

func (e *Engine) log(msg string) { e.logAt(appLogger.InfoAt, 1, msg) }

// logWarn keeps a failure visible in the live progress panel while the file log
// still records it at warn severity.
func (e *Engine) logWarn(msg string) { e.logAt(appLogger.WarnAt, 1, msg) }

func (e *Engine) logAt(emit func(extra int, format string, args ...interface{}), extra int, msg string) {
	// 所有引擎日志同步写入 applog（文件）；+1 跳过本函数这一帧，让位置指回调用方
	emit(extra+1, "%s", "[collect:"+e.sourceKey+"] "+msg)
	if e.onLog != nil {
		e.onLog(msg)
	}
}

// Pause 暂停采集
func (e *Engine) Pause() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = true
}

// Resume 恢复采集
func (e *Engine) Resume() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.paused = false
}

// Stop 停止采集
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.stop = true
}

// IsPaused 返回当前是否处于暂停状态
func (e *Engine) IsPaused() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.paused
}

// IsStopped 返回当前是否已请求停止
func (e *Engine) IsStopped() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.stop
}

// waitPaused 如果处于暂停状态则阻塞等待，直到恢复或停止或超时
// 被外部取消也直接返回 true（停止）
func (e *Engine) waitPaused() bool {
	for {
		e.mu.Lock()
		paused := e.paused
		stop := e.stop
		e.mu.Unlock()

		if stop {
			return true
		}
		if !paused {
			return false
		}

		// 暂停中，每隔 500ms 检查一次状态
		select {
		case <-time.After(500 * time.Millisecond):
			continue
		case <-e.done():
			return true
		}
	}
}

func (e *Engine) context() context.Context {
	e.mu.Lock()
	ctx := e.ctx
	e.mu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (e *Engine) done() <-chan struct{} {
	ctx := e.context()
	return ctx.Done()
}

// fetchPageWithRetry 带重试的按页请求
// 最大重试次数 3，指数退避：5s / 10s / 20s
func fetchPageWithRetry(ctx context.Context, apiUrl string, page int, opts FetchOptions, logFn func(string)) (*FetchResult, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if logFn != nil {
			logFn(fmt.Sprintf("请求第 %d 页 (第 %d 次尝试)", page, attempt))
		}
		// apiUrl is already operation-specific and must not be rebuilt here.
		res, err := doFetchShape(ctx, apiUrl, opts.FieldMapping, opts.Response)
		if err == nil {
			return res, nil
		}
		lastErr = err
		if logFn != nil {
			logFn(fmt.Sprintf("第 %d 页 第 %d 次请求失败: %v", page, attempt, err))
		}
		if attempt < maxRetries {
			backoff := time.Duration(5*(1<<uint(attempt-1))) * time.Second
			if logFn != nil {
				logFn(fmt.Sprintf("等待 %v 后重试...", backoff))
			}
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return nil, apperror.Wrap(apperror.Unavailable, lastErr, fmt.Sprintf("第 %d 页采集失败，已重试 %d 次", page, maxRetries))
}

// RunStats 记录一次采集真正做完了什么。源站返回的总页数/总数只是估计，
// 只有 Saved 与失败页码列表可信。
type RunStats struct {
	Saved          int     `json:"saved"`
	PagesFetched   int     `json:"pages_fetched"`
	PagesTotal     int     `json:"pages_total"`
	SourceTotal    int     `json:"source_total"`
	FetchFailures  []int   `json:"fetch_failures"`
	SaveFailures   []int   `json:"save_failures"`
	EmptyPages     []int   `json:"empty_pages"`
	Stopped        bool    `json:"stopped"`
	ElapsedSeconds float64 `json:"elapsed_seconds"`
}

// unfinished 返回是否整页没能落地。缺页就是缺数据，不能报成成功。
func (s *RunStats) unfinished() bool {
	return len(s.FetchFailures) > 0 || len(s.SaveFailures) > 0
}

// CollectError 表示采集跑完了但有整页没取到或没写进去。它和致命错误不同：
// 已经写入了部分数据，界面必须显示"部分失败"而不是绿色完成。
type CollectError struct {
	Stats *RunStats
}

func (e *CollectError) Error() string {
	parts := make([]string, 0, 2)
	if n := len(e.Stats.FetchFailures); n > 0 {
		parts = append(parts, fmt.Sprintf("%d 页取页失败: %s", n, joinPages(e.Stats.FetchFailures)))
	}
	if n := len(e.Stats.SaveFailures); n > 0 {
		parts = append(parts, fmt.Sprintf("%d 页入库失败: %s", n, joinPages(e.Stats.SaveFailures)))
	}
	return fmt.Sprintf("%s（已入库 %d 条）", strings.Join(parts, "；"), e.Stats.Saved)
}

// ErrorKind 给前端一个稳定的机器可读分类："" | "fetch" | "save" | "partial" | "fatal"。
func ErrorKind(err error) string {
	var ce *CollectError
	if errors.As(err, &ce) {
		switch {
		case len(ce.Stats.FetchFailures) > 0 && len(ce.Stats.SaveFailures) > 0:
			return "partial"
		case len(ce.Stats.FetchFailures) > 0:
			return "fetch"
		default:
			return "save"
		}
	}
	if err != nil {
		return "fatal"
	}
	return ""
}

func joinPages(pages []int) string {
	strs := make([]string, len(pages))
	for i, p := range pages {
		strs[i] = strconv.Itoa(p)
	}
	return strings.Join(strs, ",")
}

// Run 执行采集。返回的 RunStats 始终可用（含耗时、落库条数与失败页码）；
// error 为 nil 表示全部落地，*CollectError 表示有整页没落地，其他错误表示致命失败。
func (e *Engine) Run() (*RunStats, error) {
	e.log("开始采集: " + e.sourceKey)
	startTime := time.Now()
	stats := &RunStats{}
	// finish 里要按最终生效的模式/窗口/游标决定是否推进水位线，先声明再赋值。
	var (
		mode   model.CollectMode
		hours  int
		cursor db.CollectCursor
	)

	// finish 统一收口：耗时和"部分失败"错误只在这一处生成，避免某个返回点漏报。
	finish := func(err error) (*RunStats, error) {
		stats.ElapsedSeconds = time.Since(startTime).Seconds()
		if err != nil {
			return stats, err
		}
		if stats.unfinished() {
			return stats, &CollectError{Stats: stats}
		}
		e.settleCursor(mode, hours, cursor, startTime, stats)
		return stats, nil
	}

	src, err := db.GetSourceByKey(e.sourceKey)
	if err != nil {
		return finish(apperror.Wrap(apperror.Storage, err, "获取采集源配置失败"))
	}
	e.source = src

	// 初始化数据源策略
	e.strategy = CreateStrategyFromSource(src)
	if e.strategy != nil {
		e.log(fmt.Sprintf("[策略初始化] 策略名称: %s", e.strategy.GetStrategyName()))
	}

	// 组装采集参数
	advCfg := src.GetAdvConfig()
	opts := FetchOptions{
		Limit:        advCfg.CollectLimit,
		FieldMapping: advCfg.FieldMapping,
	}

	// 模式优先级：Engine 上设置 > AdvConfig 中的 > per-source 水位线推算
	hours = e.timeHours
	if hours <= 0 && advCfg.CollectHours > 0 {
		hours = advCfg.CollectHours
	}

	mode = e.mode
	if mode == "" || mode == model.CollectModeFull {
		mode = model.CollectModeFull
	}

	if cursor, err = db.GetCollectCursor(e.sourceKey); err != nil {
		// 读不到游标按"从未成功采集过"处理：这次窗口放宽到全量，宁可多爬不可漏爬。
		e.logWarn(fmt.Sprintf("读取增量水位线失败，本次按全量窗口处理: %v", err))
		cursor = db.CollectCursor{SourceKey: e.sourceKey}
	}
	if err := db.MarkCollectAttempt(e.sourceKey, startTime); err != nil {
		e.logWarn(fmt.Sprintf("记录采集时刻失败: %v", err))
	}

	if mode == model.CollectModeIncremental && hours <= 0 {
		if h, full := db.IncrementalWindow(cursor, startTime); !full {
			hours = h
		}
	}

	modeLabel := "全量采集"
	if mode == model.CollectModeIncremental {
		modeLabel = "增量采集"
		if hours > 0 {
			opts.Hours = hours
			modeLabel = fmt.Sprintf("增量采集(%d小时)", hours)
			if cursor.CoveredUntilUnix > 0 {
				e.log(fmt.Sprintf("水位线: 已覆盖到 %s", time.Unix(cursor.CoveredUntilUnix, 0).Format("2006-01-02 15:04:05")))
			}
		} else {
			// 没有 h 参数就等于不限时间窗：这次"增量"实际会爬完所有页，
			// 必须说清楚，否则用户以为只补了最近几小时。
			e.logWarn("增量采集未指定时间窗，本次将抓取全部页（等同全量）")
		}
	} else if mode == model.CollectModeOnce {
		modeLabel = "单次采集"
	}

	e.log("采集模式: " + modeLabel)

	optsHint := ""
	if opts.Limit > 0 {
		optsHint += fmt.Sprintf(" limit=%d", opts.Limit)
	}
	if opts.Hours > 0 {
		optsHint += fmt.Sprintf(" h=%d", opts.Hours)
	}
	if optsHint != "" {
		e.log("采集参数:" + optsHint)
	}

	// 检查停止
	if e.IsStopped() {
		e.log("采集已被停止")
		stats.Stopped = true
		return finish(nil)
	}
	// 处理暂停（启动前可能已被用户暂停）
	if e.waitPaused() {
		e.log("采集已被停止")
		stats.Stopped = true
		return finish(nil)
	}

	// 拉取第 1 页（带重试）
	var firstPage *FetchResult
	if e.strategy != nil {
		// The strategy owns both the field names and, for declarative sources,
		// the envelope they arrive in; the fetcher decodes with what it is given.
		opts.FieldMapping = e.strategy.GetFieldMapping()
		opts.Response = shapeOfStrategy(e.strategy)
		listUrl := e.strategy.BuildListUrl(1, opts)
		firstPage, err = fetchPageWithRetry(e.context(), listUrl, 1, opts, e.log)
	} else {
		firstPage, err = fetchPageWithRetry(e.context(), src.ApiUrl, 1, opts, e.log)
	}
	if err != nil {
		// 第 1 页取不到就是致命失败：这次运行一条数据都没开始写。
		return finish(err)
	}

	pc := firstPage.Pagecount.Int()
	total := firstPage.Total.Int()
	// The loop below only ever runs to pc, and a declared envelope that carries
	// no pagecount decodes to 0 — so a source whose paging field is missing,
	// misnamed or non-numeric collects the first page and stops instead of
	// paging forever.
	e.log(fmt.Sprintf("共 %d 页, 总数 %d", pc, total))
	stats.PagesTotal = pc
	stats.SourceTotal = total
	stats.PagesFetched++

	if e.onProgress != nil {
		e.onProgress(1, pc)
	}

	// 推送第 1 页的视频名列表
	e.emitPageNames(1, firstPage.List)

	e.commitPage(stats, 1, firstPage.List)

	// 单次采集模式：只采集第1页就停止（用于测试或快速预览）
	if mode == model.CollectModeOnce {
		e.log(fmt.Sprintf("单次采集完成, 共 %d 条视频", stats.Saved))
		return finish(nil)
	}

	// 后续页循环：pageGap 间隔 + 暂停检查 + 停止检查 + 重试
	for p := 2; p <= pc; p++ {
		// pageGap 间隔（可被暂停/停止打断）
		if !e.sleepInterruptible(e.currentPageGap()) {
			e.log("采集已被停止")
			stats.Stopped = true
			break
		}

		// 暂停等待
		if e.waitPaused() {
			e.log("采集已被停止")
			stats.Stopped = true
			break
		}

		// 检查停止（在请求前检查，但一旦开始请求就完成当前页）
		if e.IsStopped() {
			e.log("收到停止信号，采集终止")
			stats.Stopped = true
			break
		}

		e.log(fmt.Sprintf("采集第 %d/%d 页", p, pc))
		var page *FetchResult
		if e.strategy != nil {
			listUrl := e.strategy.BuildListUrl(p, opts)
			page, err = fetchPageWithRetry(e.context(), listUrl, p, opts, e.log)
		} else {
			page, err = fetchPageWithRetry(e.context(), src.ApiUrl, p, opts, e.log)
		}
		if err != nil {
			// 取页失败必须计入结果：只记日志就 continue，会让整源全页 500 的
			// 运行以"采集完成"收场。
			e.logWarn(fmt.Sprintf("跳过第%d页: %v", p, err))
			stats.FetchFailures = append(stats.FetchFailures, p)
			continue
		}
		stats.PagesFetched++

		if e.onProgress != nil {
			e.onProgress(p, pc)
		}
		e.emitPageNames(p, page.List)

		if len(page.List) == 0 {
			// 源站声称还有这么多页却回空列表：通常是被限流或分页参数失效。
			e.logWarn(fmt.Sprintf("第%d页返回空列表（源站声称共%d页）", p, pc))
			stats.EmptyPages = append(stats.EmptyPages, p)
		}
		e.commitPage(stats, p, page.List)

		// 当前页完成后检查停止，不继续下一页
		if e.IsStopped() {
			e.log("当前页已保存完成，收到停止信号，采集终止")
			stats.Stopped = true
			break
		}
	}

	summary := fmt.Sprintf("采集结束, 耗时 %v, 已入库 %d 条（源站总数 %d）", time.Since(startTime), stats.Saved, total)
	if len(stats.FetchFailures) > 0 || len(stats.SaveFailures) > 0 {
		summary += fmt.Sprintf("，取页失败 %d 页 / 入库失败 %d 页", len(stats.FetchFailures), len(stats.SaveFailures))
	}
	e.log(summary)
	return finish(nil)
}

// commitPage 处理并写入一页，把结果记进 stats。
func (e *Engine) commitPage(stats *RunStats, page int, list []*model.Video) {
	processed := e.processVideos(list)
	if err := e.saveVideos(processed); err != nil {
		e.logWarn(fmt.Sprintf("保存第%d页失败: %v", page, err))
		stats.SaveFailures = append(stats.SaveFailures, page)
		return
	}
	stats.Saved += len(processed)
}

// settleCursor 只有真正覆盖完一次窗口才推进水位线。
//
// 有缺页、被中途停止、单次采样都不推进：下一次运行会重扫同一段时间，缺的数据
// 才有机会补回来。窗口左端如果还没够到旧水位线（用户手改了更小的时间窗）时也不
// 推进，否则中间那段缺口会被当作"已采集"永久跳过。
func (e *Engine) settleCursor(mode model.CollectMode, hours int, cursor db.CollectCursor, windowEnd time.Time, stats *RunStats) {
	if mode == model.CollectModeOnce || stats.Stopped {
		return
	}
	if hours > 0 && cursor.CoveredUntilUnix > 0 {
		windowStart := windowEnd.Add(-time.Duration(hours) * time.Hour)
		if windowStart.After(time.Unix(cursor.CoveredUntilUnix, 0)) {
			e.logWarn(fmt.Sprintf("本次窗口(%d小时)未覆盖到水位线，保持原值不推进", hours))
			return
		}
	}
	if err := db.AdvanceCollectCursor(e.sourceKey, windowEnd); err != nil {
		e.logWarn(fmt.Sprintf("推进增量水位线失败: %v", err))
	}
}

// sleepInterruptible 睡眠期间如果被停止则返回 false，否则正常返回 true
func (e *Engine) sleepInterruptible(d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return true
		}
		// 每 500ms 检查一次是否停止/暂停
		chunk := 500 * time.Millisecond
		if remaining < chunk {
			chunk = remaining
		}
		select {
		case <-time.After(chunk):
			if e.IsStopped() {
				return false
			}
			// 同时支持暂停：若暂停则阻塞直到恢复或停止
			if e.IsPaused() {
				// 扣除已经等待的时间，继续从暂停中恢复
				if e.waitPaused() {
					return false
				}
			}
		case <-e.done():
			return false
		}
	}
}

func (e *Engine) emitPageNames(page int, list []*model.Video) {
	if e.onPageNames == nil {
		return
	}
	names := make([]string, 0, len(list))
	for _, v := range list {
		if v == nil {
			continue
		}
		if v.VodName != "" {
			names = append(names, v.VodName)
		}
	}
	e.onPageNames(page, names)
}

func (e *Engine) processVideos(list []*model.Video) []*model.Video {
	var valid []*model.Video
	for _, v := range list {
		if v == nil {
			continue
		}
		if v.VodName == "" || v.TypeName == "" {
			continue
		}

		// 清理字段值前后的反引号和其他包裹字符（某些源站会在字段值前后加反引号）
		v.VodPic = cleanField(v.VodPic)
		v.VodRemarks = cleanField(v.VodRemarks)
		v.VodYear = cleanField(v.VodYear)
		v.VodArea = cleanField(v.VodArea)
		v.VodLang = cleanField(v.VodLang)
		// A list response is durable only as a catalog projection. Never retain
		// full detail or playback fields during collection.
		v.VodContent, v.VodActor, v.VodDirector = "", "", ""
		v.VodPlayUrl, v.VodDownUrl, v.VodPlayFrom = "", "", ""

		valid = append(valid, v)
	}
	return valid
}

func (e *Engine) saveVideos(videos []*model.Video) error {
	if len(videos) == 0 {
		return nil
	}

	// 过滤掉被禁用采集的类型
	var filtered []*model.Video
	for _, v := range videos {
		if v == nil || v.TypeName == "" {
			continue
		}
		if db.IsTypeCollectEnabled(v.TypeName) {
			filtered = append(filtered, v)
		} else {
			e.log(fmt.Sprintf("[类型过滤] 跳过禁用采集类型: %s (%s)", v.VodName, v.TypeName))
		}
	}
	videos = filtered

	if len(videos) == 0 {
		return nil
	}

	// Catalog collection must not fan out into detail requests. List payloads
	// are projected directly into the durable catalog; playback data is fetched
	// only when a user opens the detail view. 源数据里携带的豆瓣字段也由同一条
	// 页事务写进 global_video，不再另跑一趟。
	if err := db.UpsertCatalogItemsBatch(e.catalogBatch, e.sourceKey, videos); err != nil {
		return fmt.Errorf("upsert catalog: %w", err)
	}

	return nil
}

// playLineSep 是 maccms 并列多条播放线路的分隔符。
const playLineSep = "$$$"

// ParsePlayLines 把 vod_play_url 拆成若干条播放线路，并按同序分段配上 vod_play_from 的线路名。
//
// $$$ 必须在 # 之前切：只切 # 的旧实现会把「$$$下一条线路的第一集$地址」一起当成上一条
// 末集的地址，播放器拿到的是一个拼错的 URL，而且线路名只能靠界面直出原始 vod_play_from。
func ParsePlayLines(playUrl, playFrom string, vodId model.FlexibleString) []*model.PlayLine {
	if strings.TrimSpace(playUrl) == "" {
		return nil
	}
	vid := strings.TrimSpace(vodId.String())
	blocks := splitSegments(playUrl)
	names := splitSegments(playFrom)
	lines := make([]*model.PlayLine, 0, len(blocks))
	for i, block := range blocks {
		episodes := parseLineEpisodes(block, vid)
		if len(episodes) == 0 {
			continue
		}
		name := ""
		if i < len(names) {
			name = names[i]
		}
		lines = append(lines, &model.PlayLine{Index: len(lines), Name: name, Episodes: episodes})
	}
	return lines
}

// splitSegments 按 $$$ 切并丢掉空段：源站常在末尾多写一个 $$$，留着会得到一条没有集的线路。
func splitSegments(value string) []string {
	if value == "" {
		return nil
	}
	raw := strings.Split(value, playLineSep)
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// parseLineEpisodes 解析一条线路内的集表。不含 $ 但本身是绝对地址的段按纯地址处理（不少
// 电影源直接给一条 m3u8），此时集名留空，由界面按序号显示。
func parseLineEpisodes(block, vid string) []*model.Episode {
	var episodes []*model.Episode
	used := make(map[int]bool)
	for _, raw := range strings.Split(block, "#") {
		part := strings.TrimSpace(raw)
		if part == "" {
			continue
		}
		name, url := part, ""
		if idx := strings.Index(part, "$"); idx >= 0 {
			name, url = strings.TrimSpace(part[:idx]), strings.TrimSpace(part[idx+1:])
		} else if strings.HasPrefix(part, "http://") || strings.HasPrefix(part, "https://") {
			name, url = "", part
		}
		num := episodeNumber(name)
		// 同号多半是源站把「第1集」和「第1集 番外」混排，位置序号更可信。
		if num <= 0 || used[num] {
			num = len(episodes) + 1
			for used[num] {
				num++
			}
		}
		used[num] = true
		episodes = append(episodes, &model.Episode{
			VodId:  model.FlexibleString(vid),
			EpNum:  num,
			EpName: name,
			EpUrl:  url,
		})
	}
	return episodes
}
