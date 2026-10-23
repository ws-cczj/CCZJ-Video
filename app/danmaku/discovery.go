package danmaku

import (
	"context"
	"encoding/json"
	"fmt"
)

func discoveryCacheKey(provider, kind, identity string) string {
	if provider == "bilibili" && kind == "episodes" {
		// v2 omitted preview badges, making same-number previews indistinguishable.
		return cacheKey(Episode{Provider: provider, EpisodeID: identity}, "discovery-v3:"+kind)
	}
	return cacheKey(Episode{Provider: provider, EpisodeID: identity}, "discovery-v2:"+kind)
}

// Discovery is reused across local episodes and player sessions. It shares the
// segment cache's one-day freshness, thirty-day eviction, capacity and owned
// single-flight cancellation; a cache hit never consumes an upstream slot.
func cachedDiscovery[T Candidate | Episode](ctx context.Context, s *Service, provider, kind, identity string, limit int, fetch func(context.Context) ([]T, error)) ([]T, error) {
	key := discoveryCacheKey(provider, kind, identity)
	decode := func(raw []byte) ([]T, error) {
		if len(raw) > MaxPayload {
			return nil, fmt.Errorf("discovery cache exceeds payload budget")
		}
		var rows []T
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		if rows == nil || len(rows) > limit {
			return nil, fmt.Errorf("invalid discovery cache")
		}
		for _, row := range rows {
			switch value := any(row).(type) {
			case Candidate:
				if value.Provider != provider || value.Ref == "" || len(value.Ref) > 2048 || len(value.Title) > 1024 {
					return nil, fmt.Errorf("invalid cached work identity")
				}
			case Episode:
				if value.Provider != provider || value.EpisodeID == "" || len(value.EpisodeID) > 128 || len(value.Title) > 2048 || value.DurationMS < 0 || value.DurationMS > 86400000 {
					return nil, fmt.Errorf("invalid cached episode identity")
				}
			}
		}
		return rows, nil
	}
	payload, err := s.load(ctx, key, 1, func(raw []byte) error { _, err := decode(raw); return err }, func(ctx context.Context) ([]byte, error) {
		rows, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		if rows == nil {
			rows = []T{}
		}
		if len(rows) > limit {
			return nil, schemaError(fmt.Errorf("discovery result exceeds budget"))
		}
		return json.Marshal(rows)
	})
	if err != nil {
		return nil, err
	}
	return decode(payload.raw)
}
