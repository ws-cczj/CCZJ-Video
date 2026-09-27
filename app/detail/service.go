package detail

import (
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
	Video    *model.Video
	Episodes []*model.Episode
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
}

var Default = New(90 * time.Second)

func New(ttl time.Duration) *Service {
	return &Service{cache: map[string]cacheEntry{}, flights: map[string]*call{}, ttl: ttl, maxEntries: 256, maxBytes: 32 << 20, retryDelay: func(n int) time.Duration { return time.Duration(n) * 100 * time.Millisecond }}
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

// GetByGlobal is the canonical detail operation. The legacy source+vod helper
// below only adapts callers that have not yet moved their route state.
func (s *Service) GetByGlobal(sourceKey string, globalID int64, refresh ...bool) (*Result, error) {
	return s.GetByGlobalContext(context.Background(), sourceKey, globalID, refresh...)
}
func (s *Service) GetByGlobalContext(ctx context.Context, sourceKey string, globalID int64, refresh ...bool) (*Result, error) {
	if strings.TrimSpace(sourceKey) == "" || globalID <= 0 {
		return nil, fmt.Errorf("source_key and global_id are required")
	}
	catalog, err := db.GetCatalogItemByGlobalID(sourceKey, globalID)
	if err != nil {
		return nil, fmt.Errorf("catalog lookup: %w", err)
	}
	return s.getCatalogContext(ctx, catalog, len(refresh) > 0 && refresh[0])
}
func (s *Service) Get(sourceKey, vodID string, refresh ...bool) (*Result, error) {
	return s.GetContext(context.Background(), sourceKey, vodID, refresh...)
}
func (s *Service) GetContext(ctx context.Context, sourceKey, vodID string, refresh ...bool) (*Result, error) {
	catalog, err := db.GetCatalogItem(sourceKey, vodID)
	if err != nil {
		return nil, fmt.Errorf("catalog lookup: %w", err)
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
	if !force {
		if e, ok := s.cache[key]; ok && time.Now().Before(e.expires) {
			s.mu.Unlock()
			return e.result, nil
		}
	}
	if active := s.flights[key]; active != nil {
		s.mu.Unlock()
		select {
		case <-active.done:
			return active.result, active.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	active := &call{done: make(chan struct{})}
	s.flights[key] = active
	s.mu.Unlock()
	// WithoutCancel: this fetch is shared with every follower below. Cancelling it
	// because the caller who happened to arrive first navigated away would fail a
	// request that is already paid for and still wanted by the others.
	active.result, active.err = s.fetchContext(context.WithoutCancel(ctx), catalog)
	s.mu.Lock()
	if active.err == nil {
		s.putCache(key, active.result)
	}
	delete(s.flights, key)
	close(active.done)
	s.mu.Unlock()
	return active.result, active.err
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
func (s *Service) putCache(key string, result *Result) {
	now, size := time.Now(), resultSize(result)
	if old, ok := s.cache[key]; ok {
		s.cacheBytes -= old.bytes
	}
	s.cache[key] = cacheEntry{result: result, expires: now.Add(s.ttl), created: now, bytes: size}
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
			return nil, fmt.Errorf("source strategy unavailable")
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
			return &Result{video, collect.ParseEpisodes(video.VodPlayUrl, video.VodId, nil), catalog}, nil
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
