package danmaku

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/workflight"
)

type cachedPayload struct {
	raw           []byte
	cached, stale bool
	warning       string
}
type segmentPayload struct {
	Items   []Item `json:"items"`
	Skipped int    `json:"skipped"`
}

type Service struct {
	store                               *Store
	providers                           map[string]Provider
	reads                               *workflight.Group[cachedPayload]
	matches                             *workflight.Group[MatchResult]
	matchSlots                          chan struct{}
	matchRuns, fallbacks, cooldownHits  atomic.Uint64
	now                                 func() time.Time
	hits, fetches, stale, cacheFailures atomic.Uint64
}

type Stats struct {
	CacheHits      uint64 `json:"cache_hits"`
	Fetches        uint64 `json:"fetches"`
	StaleFallbacks uint64 `json:"stale_fallbacks"`
	CacheFailures  uint64 `json:"cache_failures"`
	MatchRuns      uint64 `json:"match_runs"`
	Fallbacks      uint64 `json:"fallbacks"`
	CooldownHits   uint64 `json:"cooldown_hits"`
}

func (s *Service) Stats() Stats {
	return Stats{CacheHits: s.hits.Load(), Fetches: s.fetches.Load(), StaleFallbacks: s.stale.Load(), CacheFailures: s.cacheFailures.Load(), MatchRuns: s.matchRuns.Load(), Fallbacks: s.fallbacks.Load(), CooldownHits: s.cooldownHits.Load()}
}
func NewService(store *Store, client *Client, owner context.Context) *Service {
	return &Service{store: store, providers: map[string]Provider{"bilibili": &bilibiliProvider{client: client}, "qq": &qqProvider{client: client}, "iqiyi": &iqiyiProvider{client: client}}, reads: workflight.New[cachedPayload](owner, 64), matches: workflight.New[MatchResult](owner, 16), matchSlots: make(chan struct{}, 3), now: time.Now}
}
func (s *Service) Close(ctx context.Context) error {
	return errors.Join(s.matches.Close(ctx), s.reads.Close(ctx))
}
func (s *Service) provider(ctx context.Context, name string) (Provider, error) {
	p := s.providers[name]
	if p == nil {
		return nil, apperror.New(apperror.Unsupported, "不支持的弹幕平台")
	}
	settings, err := s.store.SourceSettings(ctx)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取弹幕源设置失败")
	}
	if !settings.Enabled(name) {
		return nil, apperror.New(apperror.PermissionDenied, "该弹幕源已停用，请在弹幕源管理中启用")
	}
	return p, nil
}
func (s *Service) Search(ctx context.Context, name, query string) ([]Candidate, error) {
	p, err := s.provider(ctx, name)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" || len(query) > 300 {
		return nil, apperror.New(apperror.Validation, "请输入影片名称")
	}
	return cachedDiscovery(ctx, s, name, "search", query, 200, func(ctx context.Context) ([]Candidate, error) { return p.Search(ctx, query) })
}
func (s *Service) Episodes(ctx context.Context, name, ref string) ([]Episode, error) {
	p, err := s.provider(ctx, name)
	if err != nil {
		return nil, err
	}
	if len(ref) > 2048 {
		return nil, apperror.New(apperror.Validation, "单集地址过长")
	}
	return cachedDiscovery(ctx, s, name, "episodes", ref, 5000, func(ctx context.Context) ([]Episode, error) { return p.Episodes(ctx, ref) })
}
func (s *Service) Binding(ctx context.Context, key string) (*Binding, error) {
	b, err := s.store.Binding(ctx, key)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取弹幕绑定失败")
	}
	return b, nil
}
func (s *Service) Bind(ctx context.Context, b Binding) error {
	if err := ValidateBinding(b); err != nil {
		return err
	}
	// Both normalized matches and manual corrections must resolve a concrete
	// remote pool before publishing a binding. Array positions are not identities.
	p, err := s.provider(ctx, b.Episode.Provider)
	if err != nil {
		return err
	}
	if _, err = s.loadIndex(ctx, p, b.Episode, 1); err != nil {
		return err
	}
	if err = s.store.SaveBinding(ctx, b); err != nil {
		return apperror.Wrap(apperror.Storage, err, "弹幕绑定保存失败，请重试")
	}
	return nil
}
func (s *Service) Unbind(ctx context.Context, key string) error {
	if len(key) > 512 {
		return apperror.New(apperror.Validation, "单集标识无效")
	}
	if err := s.store.Unbind(ctx, key); err != nil {
		return apperror.Wrap(apperror.Storage, err, "解除弹幕绑定失败")
	}
	return nil
}

func cacheKey(e Episode, part string) string {
	raw, _ := json.Marshal([]string{DecoderVersion, e.Provider, e.EpisodeID, e.AID, part})
	sum := sha256.Sum256(raw)
	return e.Provider + ":" + hex.EncodeToString(sum[:])
}
func (s *Service) load(ctx context.Context, key string, days int, validate func([]byte) error, fetch func(context.Context) ([]byte, error)) (cachedPayload, error) {
	days = max(1, min(30, days))
	return s.reads.Do(ctx, fmt.Sprintf("%s:fresh-%d", key, days), func(ctx context.Context) (cachedPayload, error) {
		now := s.now()
		raw, at, err := s.store.Cached(ctx, key, now)
		cacheErr := err
		if len(raw) > 0 && validate(raw) != nil {
			raw = nil
		}
		if len(raw) > 0 && err == nil && now.Before(at.Add(time.Duration(days)*24*time.Hour)) {
			s.hits.Add(1)
			return cachedPayload{raw: raw, cached: true}, nil
		}
		s.fetches.Add(1)
		fresh, err := fetch(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return cachedPayload{}, ctx.Err()
			}
			if len(raw) > 0 && now.Before(at.AddDate(0, 0, 30)) {
				s.stale.Add(1)
				return cachedPayload{raw: raw, cached: true, stale: true, warning: "cached_source_unavailable"}, nil
			}
			return cachedPayload{}, err
		}
		if err = validate(fresh); err != nil {
			return cachedPayload{}, schemaError(err)
		}
		result := cachedPayload{raw: fresh}
		if err = s.store.Put(ctx, key, fresh, now); err != nil || cacheErr != nil {
			result.warning = "cache_write_failed"
			s.cacheFailures.Add(1)
			applog.WarnFields("danmaku cache unavailable", applog.Fields{"mode": "video", "code": apperror.Storage, "error": errors.Join(err, cacheErr)})
		}
		return result, nil
	})
}
func decodeIndex(raw []byte) (Index, error) {
	var index Index
	if err := json.Unmarshal(raw, &index); err != nil {
		return index, err
	}
	if index.Segments == nil || len(index.Segments) > 10000 {
		return index, fmt.Errorf("invalid cached segment index")
	}
	var previous int64 = -1
	for _, segment := range index.Segments {
		if segment.Key == "" || len(segment.Key) > 200 || segment.StartMS < 0 || segment.StartMS < previous || segment.EndMS <= segment.StartMS || segment.EndMS > 25*60*60*1000 {
			return index, fmt.Errorf("invalid segment boundary")
		}
		previous = segment.StartMS
	}
	return index, nil
}
func decodeSegment(raw []byte) (segmentPayload, error) {
	var result segmentPayload
	if err := json.Unmarshal(raw, &result); err != nil {
		return result, err
	}
	if result.Items == nil || len(result.Items) > MaxSegmentItems || result.Skipped < 0 {
		return result, fmt.Errorf("invalid cached segment")
	}
	for _, item := range result.Items {
		if !validItem(item) || item.Color > 0xffffff {
			return result, fmt.Errorf("invalid cached item")
		}
	}
	return result, nil
}

func (s *Service) loadIndex(ctx context.Context, p Provider, e Episode, days int) (cachedPayload, error) {
	return s.load(ctx, cacheKey(e, "index"), days, func(raw []byte) error { _, err := decodeIndex(raw); return err }, func(ctx context.Context) ([]byte, error) {
		index, err := p.Index(ctx, e)
		if err != nil {
			return nil, err
		}
		return json.Marshal(index)
	})
}

// ReadWindow fetches only the current timeline window, never an entire episode.
// Positive offset delays source comments in the local video. Cached timestamps
// remain in source time and are never permanently shifted.
func (s *Service) ReadWindow(ctx context.Context, key string, positionMS int64, freshDays int) (*Window, error) {
	binding, err := s.Binding(ctx, key)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, apperror.New(apperror.NotFound, "当前单集尚未匹配弹幕")
	}
	return s.readBoundWindow(ctx, binding, positionMS, freshDays)
}

func (s *Service) readBoundWindow(ctx context.Context, binding *Binding, positionMS int64, freshDays int) (*Window, error) {
	if positionMS < 0 || positionMS > 24*60*60*1000 {
		return nil, apperror.New(apperror.Validation, "播放位置无效")
	}
	e := binding.Episode
	p, err := s.provider(ctx, e.Provider)
	if err != nil {
		return nil, err
	}
	sourcePosition := positionMS - binding.OffsetMS
	start := max(int64(0), sourcePosition-10000)
	end := max(int64(0), sourcePosition+75000)
	result := &Window{Provider: e.Provider, EpisodeID: e.EpisodeID, StartMS: start, EndMS: end, Items: []Item{}, Warnings: []string{}, Cached: true}
	if sourcePosition < -75000 {
		return result, nil
	}
	loaded, err := s.loadIndex(ctx, p, e, freshDays)
	if err != nil {
		return nil, err
	}
	index, _ := decodeIndex(loaded.raw)
	if len(index.Segments) == 0 {
		result.Warnings = append(result.Warnings, "pool_empty")
	}
	result.Cached = loaded.cached
	result.Stale = loaded.stale
	if loaded.warning != "" {
		result.Warnings = append(result.Warnings, loaded.warning)
	}
	seen := map[string]bool{}
	loadedCount := 0
	for _, segment := range index.Segments {
		if segment.EndMS < start || segment.StartMS > end {
			continue
		}
		loadedCount++
		if loadedCount > 16 {
			return nil, schemaError(fmt.Errorf("too many segments in time window"))
		}
		value, err := s.load(ctx, cacheKey(e, segment.Key), freshDays, func(raw []byte) error { _, err := decodeSegment(raw); return err }, func(ctx context.Context) ([]byte, error) {
			items, skipped, err := p.Segment(ctx, e, segment)
			if err != nil {
				return nil, err
			}
			return json.Marshal(segmentPayload{Items: items, Skipped: skipped})
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if apperror.Is(err, apperror.NotFound) {
				result.Warnings = append(result.Warnings, "segment_not_found")
				continue
			}
			return nil, err
		}
		payload, _ := decodeSegment(value.raw)
		result.Cached = result.Cached && value.cached
		result.Stale = result.Stale || value.stale
		result.Skipped += payload.Skipped
		if value.warning != "" {
			result.Warnings = append(result.Warnings, value.warning)
		}
		for _, item := range payload.Items {
			if item.TimeMS < start || item.TimeMS > end {
				continue
			}
			identity := item.ID
			if identity == "" {
				identity = fmt.Sprintf("%d:%d:%d:%s", item.TimeMS, item.Mode, item.Color, item.Text)
			}
			if seen[identity] {
				continue
			}
			seen[identity] = true
			result.Items = append(result.Items, item)
			if len(result.Items) >= 12000 {
				result.Warnings = append(result.Warnings, "density_limited")
				result.Items = sorted(result.Items)
				return result, nil
			}
		}
	}
	result.Items = sorted(result.Items)
	return result, nil
}
