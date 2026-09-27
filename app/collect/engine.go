package collect

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
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

func NewEngine(sourceKey string, onLog func(string), onProgress func(int, int), opts ...EngineOption) *Engine {
	e := &Engine{
		sourceKey:  sourceKey,
		onLog:      onLog,
		onProgress: onProgress,
		pageGap:    30 * time.Second,
		mode:       model.CollectModeFull,
	}
	for _, o := range opts {
		o(e)
	}
	return e
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
		sourceKey:   sourceKey,
		onLog:       onLog,
		onProgress:  onProgress,
		onPageNames: onPageNames,
		pageGap:     30 * time.Second,
		mode:        model.CollectModeFull,
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
	e.pageGap = gap
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
		res, err := doFetchContext(ctx, apiUrl, opts.FieldMapping)
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
	return nil, fmt.Errorf("第 %d 页采集失败，已重试 %d 次: %w", page, maxRetries, lastErr)
}

func (e *Engine) Run() (int, error) {
	e.log("开始采集: " + e.sourceKey)
	startTime := time.Now()

	src, err := db.GetSourceByKey(e.sourceKey)
	if err != nil {
		return 0, fmt.Errorf("获取采集源配置失败: %w", err)
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

	// 模式优先级：Engine 上设置 > AdvConfig 中的
	hours := e.timeHours
	if hours <= 0 && advCfg.CollectHours > 0 {
		hours = advCfg.CollectHours
	}

	mode := e.mode
	if mode == "" || mode == model.CollectModeFull {
		mode = model.CollectModeFull
	}

	modeLabel := "全量采集"
	if mode == model.CollectModeIncremental {
		modeLabel = "增量采集"
		if hours > 0 {
			opts.Hours = hours
			modeLabel = fmt.Sprintf("增量采集(%d小时)", hours)
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
		return 0, nil
	}
	// 处理暂停（启动前可能已被用户暂停）
	if e.waitPaused() {
		e.log("采集已被停止")
		return 0, nil
	}

	// 拉取第 1 页（带重试）
	var firstPage *FetchResult
	if e.strategy != nil {
		listUrl := e.strategy.BuildListUrl(1, opts)
		opts.FieldMapping = e.strategy.GetFieldMapping()
		firstPage, err = fetchPageWithRetry(e.context(), listUrl, 1, opts, e.log)
	} else {
		firstPage, err = fetchPageWithRetry(e.context(), src.ApiUrl, 1, opts, e.log)
	}
	if err != nil {
		return 0, err
	}

	pc := firstPage.Pagecount.Int()
	total := firstPage.Total.Int()
	e.log(fmt.Sprintf("共 %d 页, 总数 %d", pc, total))

	// The upstream total is only an estimate. Progress reporting must reflect what
	// actually landed, so failures to write a page are counted and surfaced
	// instead of being swallowed into a successful-looking run.
	var saved int
	var saveFailures []int

	if e.onProgress != nil {
		e.onProgress(1, pc)
	}

	// 推送第 1 页的视频名列表
	e.emitPageNames(1, firstPage.List)

	processed := e.processVideos(firstPage.List)
	if err := e.saveVideos(processed); err != nil {
		e.logWarn(fmt.Sprintf("保存第1页失败: %v", err))
		saveFailures = append(saveFailures, 1)
	} else {
		saved += len(processed)
	}

	// 单次采集模式：只采集第1页就停止（用于测试或快速预览）
	if mode == model.CollectModeOnce {
		e.log(fmt.Sprintf("单次采集完成, 共 %d 条视频", saved))
		return saved, saveFailureError(saveFailures, saved)
	}

	// 后续页循环：pageGap 间隔 + 暂停检查 + 停止检查 + 重试
	for p := 2; p <= pc; p++ {
		// pageGap 间隔（可被暂停/停止打断）
		if !e.sleepInterruptible(e.pageGap) {
			e.log("采集已被停止")
			break
		}

		// 暂停等待
		if e.waitPaused() {
			e.log("采集已被停止")
			break
		}

		// 检查停止（在请求前检查，但一旦开始请求就完成当前页）
		if e.IsStopped() {
			e.log("收到停止信号，采集终止")
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
			e.log(fmt.Sprintf("跳过第%d页: %v", p, err))
			continue
		}

		if e.onProgress != nil {
			e.onProgress(p, pc)
		}
		e.emitPageNames(p, page.List)

		processed := e.processVideos(page.List)
		if err := e.saveVideos(processed); err != nil {
			e.logWarn(fmt.Sprintf("保存第%d页失败: %v", p, err))
			saveFailures = append(saveFailures, p)
		} else {
			saved += len(processed)
		}

		// 当前页完成后检查停止，不继续下一页
		if e.IsStopped() {
			e.log("当前页已保存完成，收到停止信号，采集终止")
			break
		}
	}

	e.log(fmt.Sprintf("采集完成, 耗时 %v, 已入库 %d 条（源站总数 %d）", time.Since(startTime), saved, total))
	return saved, saveFailureError(saveFailures, saved)
}

// saveFailureError reports pages whose catalog write failed. Partial success is
// still reported as an error: silently dropping whole pages is how a source ends
// up looking fully collected while the library is missing data.
func saveFailureError(pages []int, saved int) error {
	if len(pages) == 0 {
		return nil
	}
	strs := make([]string, len(pages))
	for i, p := range pages {
		strs[i] = strconv.Itoa(p)
	}
	return fmt.Errorf("%d 页保存失败: %s（已入库 %d 条）", len(pages), strings.Join(strs, ","), saved)
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
	// only when a user opens the detail view.
	if err := db.UpsertCatalogItems(e.sourceKey, videos); err != nil {
		return fmt.Errorf("upsert catalog: %w", err)
	}

	// 将源数据中携带的豆瓣信息存入全局 douban_info 表
	db.SaveDoubanInfoFromBatch(videos)

	return nil
}

func ParseEpisodes(playUrl string, vodId model.FlexibleString, source *model.Source) []*model.Episode {
	if playUrl == "" {
		return nil
	}
	decoded := playUrl

	vid := strings.TrimSpace(vodId.String())

	var episodes []*model.Episode
	parts := strings.Split(decoded, "#")
	for _, part := range parts {
		epParts := strings.SplitN(part, "$", 2)
		epName := epParts[0]
		epUrl := ""
		if len(epParts) == 2 {
			epUrl = epParts[1]
		}
		epNum := len(episodes) + 1
		episodes = append(episodes, &model.Episode{
			VodId:  model.FlexibleString(vid),
			EpNum:  epNum,
			EpName: epName,
			EpUrl:  epUrl,
		})
	}
	return episodes
}
