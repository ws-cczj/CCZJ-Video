package danmaku

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"
)

func discoveryCacheKey(provider, kind, identity string) string {
	if provider == "bilibili" && kind == "episodes" {
		// v2 omitted preview badges, making same-number previews indistinguishable.
		return cacheKey(Episode{Provider: provider, EpisodeID: identity}, "discovery-v3:"+kind)
	}
	return cacheKey(Episode{Provider: provider, EpisodeID: identity}, "discovery-v2:"+kind)
}

// Discovery shares the short-lived cache and in-flight reads across episodes.
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
	shortLived := func(raw []byte) time.Duration {
		if bytes.Equal(raw, []byte("[]")) {
			return noMatchCooldown
		}
		if provider == "bilibili" && kind == "search" {
			var rows []Candidate
			if json.Unmarshal(raw, &rows) == nil && bilibiliOfficialMissing(rows) {
				return noMatchCooldown
			}
		}
		return 0
	}
	trace, _ := ctx.Value(matchTraceKey{}).(string)
	payload, err := s.load(ctx, key, func(raw []byte) error { _, err := decode(raw); return err }, func(ctx context.Context) ([]byte, error) {
		if trace != "" {
			ctx = context.WithValue(ctx, matchTraceKey{}, trace)
		}
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
	}, discoveryFreshness, shortLived)
	if err != nil {
		return nil, err
	}
	return decode(payload.raw)
}
