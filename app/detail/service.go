package detail

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

type Error struct {
	SourceKey   string       `json:"source_key"`
	GlobalID    int64        `json:"global_id"`
	SourceVodID string       `json:"source_vod_id"`
	Attempts    int          `json:"attempts"`
	Retryable   bool         `json:"retryable"`
	Message     string       `json:"message"`
	Fallback    *model.Video `json:"-"`
}

func (e *Error) Error() string { return e.Message }

type Result struct {
	Video *model.Video
	// Episodes 是 Lines 首条线路的集表，保留给只认单线路的旧调用方；界面应按 Lines 选线路。
	Episodes []*model.Episode
	Lines    []*model.PlayLine
	Catalog  *db.CatalogItem
}
type cacheEntry struct {
	result  *Result
	expires time.Time
	created time.Time
	bytes   int64
}
type call struct {
	done   chan struct{}
	result *Result
	err    error
}
type Service struct {
	mu         sync.Mutex
	cache      map[string]cacheEntry
	flights    map[string]*call
	ttl        time.Duration
	maxEntries int
	maxBytes   int64
	cacheBytes int64
	retryDelay func(int) time.Duration
	fetchFn    func(*db.CatalogItem) (*Result, error)
	requestFn  func(*db.CatalogItem) (*model.Video, error)
	// 读法与取数结果的计数，跟着缓存一起活在 mu 下：它们是同一段临界区里的事件，
	// 单开一组原子变量只会让「命中率」和「条目数」来自两个时刻。
	hits      int64
	staleHits int64
	misses    int64
	fetchOK   int64
	fetchFail int64
}

var Default = New(90 * time.Second)

// backgroundFetchTimeout 只加在「没人在等」的那次刷新上：后台刷新如果卡在慢源上无限期
// 占着这个 key 的单飞，后面的读就再也触发不了刷新，旧数据会一直顶着。等结果的调用方
// 不套这个期限（改造前就没有），否则慢源会被我们掐断成失败。
const backgroundFetchTimeout = 2 * time.Minute

// SettingFreshness 决定「多旧的详情还值得直接用」，也就是后台静默刷新的频率。
// 它不改变界面等待：过期条目一律先回旧数据，刷新在后台补缓存。
const SettingFreshness = "detail_cache_freshness"

// FreshnessTTL 把设置值翻成时长。前端设置项用的就是这三个字面量，未知值归标准档。
func FreshnessTTL(raw string) time.Duration {
	switch strings.TrimSpace(raw) {
	case "fast":
		return 30 * time.Second
	case "saver":
		return 10 * time.Minute
	default:
		return 90 * time.Second
	}
}

func New(ttl time.Duration) *Service {
	return &Service{cache: map[string]cacheEntry{}, flights: map[string]*call{}, ttl: ttl, maxEntries: 256, maxBytes: 32 << 20, retryDelay: func(n int) time.Duration { return time.Duration(n) * 100 * time.Millisecond }}
}

// ttlNow 每次落缓存都重新读设置，所以改档位立刻生效、不需要重启。库里读不到或值是空的
// （包括没开库的单测）就退回构造值，让 New(ttl) 仍然是测试可控的。
func (s *Service) ttlNow() time.Duration {
	raw, err := db.GetSetting(SettingFreshness)
	if err != nil || strings.TrimSpace(raw) == "" {
		return s.ttl
	}
	return FreshnessTTL(raw)
}

// InvalidateSource is called after source configuration changes so a cached
// detail can never outlive its request strategy.
func (s *Service) InvalidateSource(sourceKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for key, entry := range s.cache {
		if strings.HasPrefix(key, sourceKey+":") {
			delete(s.cache, key)
			s.cacheBytes -= entry.bytes
		}
	}
}

// InvalidateVideo drops the cached detail of one specific title. The key is the
// identity-normalised sourceKey:global_id, so callers that only hold a vod_id have
// to resolve it through cache.InvalidateVideo.
func (s *Service) InvalidateVideo(sourceKey string, globalID int64) {
	if globalID <= 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	key := fmt.Sprintf("%s:%d", sourceKey, globalID)
	if entry, ok := s.cache[key]; ok {
		delete(s.cache, key)
		s.cacheBytes -= entry.bytes
	}
}

// Stats reports what the cache holds and how it has been read, for the
// diagnostics panel.
type Stats struct {
	Entries int   `json:"entries"`
	Bytes   int64 `json:"bytes"`
	// Hits 是「新鲜命中」：直接用缓存，一次网络都不发。
	Hits int64 `json:"hits"`
	// StaleHits 是过期命中：旧数据先交出去，刷新在后台跑。它按命中算，因为界面没有等。
	StaleHits int64 `json:"stale_hits"`
	// Misses 是手里没东西、调用方必须等一次取数的读。用户主动点的强制刷新也算在这里：
	// 它确实让界面等了一趟网络，诊断页不该把它读成命中。
	Misses    int64 `json:"misses"`
	FetchOK   int64 `json:"fetch_ok"`
	FetchFail int64 `json:"fetch_fail"`
}

func (s *Service) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Entries: len(s.cache), Bytes: s.cacheBytes, Hits: s.hits, StaleHits: s.staleHits, Misses: s.misses, FetchOK: s.fetchOK, FetchFail: s.fetchFail}
}

// GetByGlobalContext is the canonical detail operation. The source+vod helper
// below only adapts callers that have not yet moved their route state.
//
// 没有不带 ctx 的同名包装：详情取数挂在应用生命周期上下文上才有取消语义，
// 一个偷偷用 context.Background() 的入口等于把这条约束藏起来。
func (s *Service) GetByGlobalContext(ctx context.Context, sourceKey string, globalID int64, refresh ...bool) (*Result, error) {
	if strings.TrimSpace(sourceKey) == "" || globalID <= 0 {
		return nil, apperror.New(apperror.Validation, "source_key and global_id are required")
	}
	catalog, err := db.GetCatalogItemByGlobalID(sourceKey, globalID)
	if err != nil {
		return nil, apperror.Wrap(apperror.NotFound, err, "catalog lookup")
	}
	return s.getCatalogContext(ctx, catalog, len(refresh) > 0 && refresh[0])
}
func (s *Service) GetContext(ctx context.Context, sourceKey, vodID string, refresh ...bool) (*Result, error) {
	catalog, err := db.GetCatalogItem(sourceKey, vodID)
	if err != nil {
		return nil, apperror.Wrap(apperror.NotFound, err, "catalog lookup")
	}
	return s.getCatalogContext(ctx, catalog, len(refresh) > 0 && refresh[0])
}
func (s *Service) getCatalog(catalog *db.CatalogItem, refresh ...bool) (*Result, error) {
	return s.getCatalogContext(context.Background(), catalog, refresh...)
}
func (s *Service) getCatalogContext(ctx context.Context, catalog *db.CatalogItem, refresh ...bool) (*Result, error) {
	force := len(refresh) > 0 && refresh[0]
	key := fmt.Sprintf("%s:%d", catalog.SourceKey, catalog.GlobalID)

	s.mu.Lock()
	cached, has := s.cache[key]
	switch {
	case !force && has && time.Now().Before(cached.expires):
		s.hits++
		s.mu.Unlock()
		return cached.result, nil
	case !force && has && cached.result != nil:
		// 过期不是等待的理由：旧数据先交出去让界面立刻有内容，同时让一次后台刷新去补缓存。
		// 刷新结果只在下一次读时被看见，所以用户看到的是「先出画面、变了才换」，没有转圈。
		s.staleHits++
		s.startFlight(key, catalog, ctx, backgroundFetchTimeout)
		s.mu.Unlock()
		return cached.result, nil
	}
	s.misses++
	active := s.startFlight(key, catalog, ctx, 0)
	s.mu.Unlock()
	select {
	case <-active.done:
		return active.result, active.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// startFlight 在锁内登记一次共享取数：已经有人在跑就复用，绝不重开第二趟。
func (s *Service) startFlight(key string, catalog *db.CatalogItem, ctx context.Context, timeout time.Duration) *call {
	if active := s.flights[key]; active != nil {
		return active
	}
	active := &call{done: make(chan struct{})}
	s.flights[key] = active
	go s.runFlight(key, catalog, ctx, active, timeout)
	return active
}

// runFlight 独立跑完一次取数并收尾：写结果、从 flights 摘掉、关闭 done。HTTP 全程在锁外，
// 锁只在「落缓存 + 摘飞单」时拿一次。
func (s *Service) runFlight(key string, catalog *db.CatalogItem, ctx context.Context, active *call, timeout time.Duration) {
	// WithoutCancel: this fetch is shared with every follower. Cancelling it
	// because the caller who happened to arrive first navigated away would fail a
	// request that is already paid for and still wanted by the others.
	runCtx := context.WithoutCancel(ctx)
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(runCtx, timeout)
		defer cancel()
	}
	active.result, active.err = s.fetchContext(runCtx, catalog)
	// 新鲜度时长在拿缓存锁之前读：它落在 SQLite 上，别把一次数据库查询塞进互斥锁里。
	ttl := s.ttlNow()
	s.mu.Lock()
	if active.err == nil {
		s.fetchOK++
		s.putCache(key, active.result, ttl)
	} else if stale, ok := s.cache[key]; ok && stale.result != nil {
		s.fetchFail++
		// 后台刷新失败不该把还能顶用的旧数据一起拖走：给它续一个窗口，免得之后每次读
		// 都撞一次坏请求。等结果的调用方照旧拿到错误，行为与改造前一致。
		stale.expires = time.Now().Add(ttl)
		s.cache[key] = stale
	} else {
		s.fetchFail++
	}
	delete(s.flights, key)
	s.mu.Unlock()
	close(active.done)
}

func resultSize(r *Result) int64 {
	if r == nil || r.Video == nil {
		return 0
	}
	v := r.Video
	n := len(v.VodName) + len(v.VodPic) + len(v.VodRemarks) + len(v.VodContent) + len(v.VodActor) + len(v.VodDirector) + len(v.VodPlayUrl) + len(v.VodDownUrl)
	for _, ep := range r.Episodes {
		if ep != nil {
			n += len(ep.EpName) + len(ep.EpUrl) + 32
		}
	}
	return int64(n + 256)
}
func (s *Service) putCache(key string, result *Result, ttl time.Duration) {
	now, size := time.Now(), resultSize(result)
	if old, ok := s.cache[key]; ok {
		s.cacheBytes -= old.bytes
	}
	s.cache[key] = cacheEntry{result: result, expires: now.Add(ttl), created: now, bytes: size}
	s.cacheBytes += size
	for len(s.cache) > s.maxEntries || s.cacheBytes > s.maxBytes {
		var oldest string
		var at time.Time
		for k, e := range s.cache {
			if oldest == "" || e.created.Before(at) {
				oldest, at = k, e.created
			}
		}
		e := s.cache[oldest]
		delete(s.cache, oldest)
		s.cacheBytes -= e.bytes
	}
}
func (s *Service) fetch(catalog *db.CatalogItem) (*Result, error) {
	return s.fetchContext(context.Background(), catalog)
}
func (s *Service) fetchContext(ctx context.Context, catalog *db.CatalogItem) (*Result, error) {
	if s.fetchFn != nil {
		return s.fetchFn(catalog)
	}
	var request func() (*model.Video, error)
	if s.requestFn != nil {
		request = func() (*model.Video, error) { return s.requestFn(catalog) }
	} else {
		source, err := db.GetSourceByKey(catalog.SourceKey)
		if err != nil {
			return nil, err
		}
		strategy := collect.CreateStrategyFromSource(source)
		if strategy == nil {
			return nil, apperror.New(apperror.Unavailable, "source strategy unavailable")
		}
		request = func() (*model.Video, error) {
			return collect.FetchVideoDetailWithURLContext(ctx, strategy.BuildDetailUrl(catalog.SourceVodID), strategy.GetFieldMapping())
		}
	}
	var last error
	retryable := true
	attempts := 0
	for attempt := 1; attempt <= 3; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		attempts = attempt
		video, fetchErr := request()
		if fetchErr == nil && video != nil {
			if video.VodId.String() == "" {
				video.VodId = model.FlexibleString(catalog.SourceVodID)
			}
			video.GlobalId = catalog.GlobalID
			// 源站详情不带豆瓣字段，从 global_video 回填（评论入口依赖 vod_douban_id）。
			db.EnrichVideoWithDoubanByGlobalID(video, catalog.GlobalID)
			lines := collect.ParsePlayLines(video.VodPlayUrl, video.VodPlayFrom, video.VodId)
			return &Result{Video: video, Episodes: firstLineEpisodes(lines), Lines: lines, Catalog: catalog}, nil
		}
		last = fetchErr
		retryable = isRetryable(fetchErr)
		if !retryable || attempt == 3 {
			break
		}
		delay := s.retryDelay(attempt)
		var httpErr *collect.HTTPError
		if errors.As(fetchErr, &httpErr) && httpErr.RetryAfter > delay {
			delay = httpErr.RetryAfter
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	// Return a safe catalog-only degradation: no historical details or URLs.
	fallback := &model.Video{VodId: model.FlexibleString(catalog.SourceVodID), GlobalId: catalog.GlobalID, VodName: catalog.VodName, VodPic: catalog.VodPic, VodRemarks: catalog.VodRemarks, VodYear: catalog.VodYear, VodArea: catalog.VodArea, TypeName: catalog.TypeName, TypeId: model.FlexibleString(catalog.SourceTypeID)}
	db.EnrichVideoWithDoubanByGlobalID(fallback, catalog.GlobalID)
	return &Result{Video: fallback, Catalog: catalog}, &Error{SourceKey: catalog.SourceKey, GlobalID: catalog.GlobalID, SourceVodID: catalog.SourceVodID, Attempts: attempts, Retryable: retryable, Message: fmt.Sprintf("detail request failed: %v", last), Fallback: fallback}
}

// firstLineEpisodes 取首条线路的集表，填进 Result.Episodes 兼容只认单线路的调用方。
func firstLineEpisodes(lines []*model.PlayLine) []*model.Episode {
	if len(lines) == 0 {
		return nil
	}
	return lines[0].Episodes
}

func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	var httpErr *collect.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode != 400 && httpErr.StatusCode != 401 && httpErr.StatusCode != 403 && httpErr.StatusCode != 404
	}
	return true
}
