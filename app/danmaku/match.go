package danmaku

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

type MatchRequest struct {
	LocalKey     string `json:"localKey"`
	Title        string `json:"title"`
	Year         string `json:"year"`
	Kind         string `json:"kind"`
	EpisodeLabel string `json:"episodeLabel"`
	DurationMS   int64  `json:"durationMs"`
	PositionMS   int64  `json:"positionMs"`
	FreshDays    int    `json:"freshDays"`
}
type MatchAttempt struct {
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	Code      string `json:"code"`
	RetryAt   int64  `json:"retryAt"`
	Stage     string `json:"stage,omitempty"`
	Detail    string `json:"detail,omitempty"`
	ElapsedMS int64  `json:"elapsedMs,omitempty"`
}
type MatchResult struct {
	Binding  *Binding       `json:"binding"`
	Window   *Window        `json:"window"`
	Attempts []MatchAttempt `json:"attempts"`
	TraceID  string         `json:"traceId,omitempty"`
}
type providerMatch struct {
	index   int
	attempt MatchAttempt
	binding *Binding
	window  *Window
	err     error
}

const matchPolicy = "episode-priority-v1"

func matchFingerprint(request MatchRequest) string {
	raw, _ := json.Marshal([]string{normalizedTitle(request.Title), metadataYear(request.Year), mediaKind(request.Kind), strings.TrimSpace(request.EpisodeLabel)})
	sum := sha256.Sum256(raw)
	return matchPolicy + ":" + hex.EncodeToString(sum[:])
}

// Match probes at most three enabled providers concurrently. Priority is source
// order, never completion time; only the chosen concrete pool is persisted.
func (s *Service) Match(ctx context.Context, request MatchRequest) (MatchResult, error) {
	if strings.TrimSpace(request.LocalKey) == "" || len(request.LocalKey) > 512 || strings.TrimSpace(request.Title) == "" || len(request.Title) > 300 || len(request.Year) > 32 || len(request.Kind) > 128 || len(request.EpisodeLabel) > 256 || request.DurationMS < 0 || request.DurationMS > 86400000 || request.PositionMS < 0 || request.PositionMS > 86400000 {
		return MatchResult{}, apperror.New(apperror.Validation, "弹幕匹配信息无效")
	}
	settings, err := s.store.SourceSettings(ctx)
	if err != nil {
		return MatchResult{}, apperror.Wrap(apperror.Storage, err, "读取弹幕源配置失败")
	}
	identity, _ := json.Marshal(struct {
		Request  MatchRequest
		Settings SourceSettings
	}{request, settings})
	sum := sha256.Sum256(identity)
	return s.matches.Do(ctx, hex.EncodeToString(sum[:]), func(ctx context.Context) (MatchResult, error) { return s.match(ctx, request, settings) })
}
func (s *Service) match(ctx context.Context, request MatchRequest, settings SourceSettings) (result MatchResult, err error) {
	s.matchRuns.Add(1)
	traceID := apperror.NewRequestID()
	ctx = context.WithValue(ctx, matchTraceKey{}, traceID)
	began := time.Now()
	defer func() {
		provider := ""
		if result.Binding != nil {
			provider = result.Binding.Episode.Provider
		}
		matchLog(ctx, "danmaku match completed", applog.Fields{"provider": provider, "duration_ms": time.Since(began).Milliseconds(), "code": apperror.CodeOf(err), "matched": result.Binding != nil})
	}()
	order := []string{}
	for _, name := range []string{settings.DefaultProvider, "bilibili", "qq", "iqiyi"} {
		if !settings.Enabled(name) {
			continue
		}
		found := false
		for _, prior := range order {
			if prior == name {
				found = true
			}
		}
		if !found {
			order = append(order, name)
		}
	}
	result = MatchResult{Attempts: []MatchAttempt{}, TraceID: traceID}
	identity := sha256.Sum256([]byte(request.LocalKey))
	matchLog(ctx, "danmaku match started", applog.Fields{"media_id": hex.EncodeToString(identity[:8]), "title": request.Title, "year": request.Year, "kind": request.Kind, "episode": request.EpisodeLabel, "duration_ms": request.DurationMS, "providers": order})
	if len(order) == 0 {
		return result, nil
	}
	workCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	values := make(chan providerMatch, len(order))
	var workers sync.WaitGroup
	for i, name := range order {
		result.Attempts = append(result.Attempts, MatchAttempt{Provider: name, Status: "not_used"})
		workers.Add(1)
		go func(index int, provider string) {
			value := providerMatch{index: index, attempt: MatchAttempt{Provider: provider, Status: "error"}}
			defer func() {
				if recovered := recover(); recovered != nil {
					value.err = apperror.Wrap(apperror.Internal, fmt.Errorf("danmaku provider panic: %v", recovered), "弹幕匹配失败")
					value.attempt.Code = string(apperror.Internal)
				}
				values <- value
				workers.Done()
			}()
			value = s.matchProvider(workCtx, request, provider)
			value.index = index
		}(i, name)
	}
	// Always join the owned probes before publishing the match or closing storage.
	defer func() { cancel(); workers.Wait() }()
	received := make([]*providerMatch, len(order))
	next := 0
	selected := -1
	for count := 0; count < len(order); count++ {
		select {
		case <-ctx.Done():
			return MatchResult{}, ctx.Err()
		case value := <-values:
			copy := value
			received[value.index] = &copy
			result.Attempts[value.index] = value.attempt
			for next < len(order) && received[next] != nil {
				if received[next].binding != nil {
					selected = next
					break
				}
				next++
			}
		}
		if selected >= 0 {
			break
		}
	}
	if selected >= 0 {
		cancel()
		workers.Wait()
		for len(values) > 0 {
			value := <-values
			result.Attempts[value.index] = value.attempt
		}
		if err := ctx.Err(); err != nil {
			return MatchResult{}, err
		}
		chosen := received[selected]
		if err := s.store.SaveBinding(ctx, *chosen.binding); err != nil {
			return MatchResult{}, apperror.Wrap(apperror.Storage, err, "保存弹幕匹配失败")
		}
		result.Binding = chosen.binding
		result.Window = chosen.window
		if selected > 0 {
			s.fallbacks.Add(1)
		}
	} else {
		for _, value := range received {
			if value != nil && value.err != nil && (apperror.Is(value.err, apperror.Storage) || apperror.Is(value.err, apperror.Internal)) {
				return MatchResult{}, value.err
			}
		}
	}
	return result, nil
}
func (s *Service) matchProvider(ctx context.Context, request MatchRequest, name string) (value providerMatch) {
	value = providerMatch{attempt: MatchAttempt{Provider: name, Status: "no_match", Stage: "cooldown"}}
	began := time.Now()
	var diagnosticErr error
	defer func() {
		value.attempt.ElapsedMS = time.Since(began).Milliseconds()
		fields := applog.Fields{"provider": name, "stage": value.attempt.Stage, "status": value.attempt.Status, "code": value.attempt.Code, "retry_at": value.attempt.RetryAt, "duration_ms": value.attempt.ElapsedMS, "detail": value.attempt.Detail}
		if diagnosticErr == nil {
			diagnosticErr = value.err
		}
		if diagnosticErr != nil && value.attempt.Status != "cancelled" {
			fields["cause"] = apperror.Diagnostic(diagnosticErr)
		}
		matchLog(ctx, "danmaku match provider", fields)
	}()
	fingerprint := matchFingerprint(request)
	failed, err := s.store.matchFailure(ctx, request.LocalKey, name, fingerprint, s.now())
	if err != nil {
		value.err = apperror.Wrap(apperror.Storage, err, "读取弹幕匹配冷却失败")
		value.attempt.Status = "error"
		value.attempt.Code = string(apperror.Storage)
		return value
	}
	if failed != nil {
		s.cooldownHits.Add(1)
		value.attempt = *failed
		value.attempt.Stage = "cooldown"
		s.explainCachedFailure(ctx, request, &value.attempt)
		return value
	}
	select {
	case s.matchSlots <- struct{}{}:
		defer func() { <-s.matchSlots }()
	case <-ctx.Done():
		value.attempt.Status = "cancelled"
		return value
	}
	probeCtx, stop := context.WithTimeout(ctx, 12*time.Second)
	binding, window, code, err := s.probeMatch(probeCtx, request, name, &value.attempt)
	diagnosticErr = err
	stop()
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		value.attempt.Status = "cancelled"
		return value
	}
	if err != nil {
		if apperror.Is(err, apperror.Storage) || apperror.Is(err, apperror.Internal) {
			value.err = err
			value.attempt.Status = "error"
			value.attempt.Code = string(apperror.CodeOf(err))
			return value
		}
		var failure *apperror.Error
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			code = string(apperror.Timeout)
		case errors.As(err, &failure):
			code = string(failure.Code)
		default:
			code = string(apperror.Unavailable)
		}
	}
	if binding != nil && err == nil {
		value.binding = binding
		value.window = window
		value.attempt.Status = "available"
		return value
	}
	if code == "" {
		code = "NO_MATCH"
	}
	value.attempt.Code = code
	value.attempt.RetryAt = s.now().Add(24 * time.Hour).Unix()
	if err = s.store.saveMatchFailure(ctx, request.LocalKey, name, fingerprint, code, s.now()); err != nil {
		value.err = apperror.Wrap(apperror.Storage, err, "保存弹幕匹配冷却失败")
		value.attempt.Status = "error"
	}
	return value
}
func (s *Service) probeMatch(ctx context.Context, request MatchRequest, name string, attempt *MatchAttempt) (*Binding, *Window, string, error) {
	attempt.Stage = "search"
	rows, err := s.Search(ctx, name, request.Title)
	if err != nil {
		return nil, nil, "", err
	}
	work := matchingWork(rows, request)
	attempt.Stage = "work"
	matchLog(ctx, "danmaku match candidates", applog.Fields{"provider": name, "count": len(rows), "candidates": candidateSummary(rows)})
	if work == nil {
		code, detail := workMatchFailure(rows, request)
		attempt.Detail = detail
		return nil, nil, code, nil
	}
	attempt.Stage = "episodes"
	matchLog(ctx, "danmaku match work selected", applog.Fields{"provider": name, "title": work.Title, "year": work.Year, "kind": work.Kind, "ref": work.Ref, "policy": matchPolicy, "title_similarity": workTitleSimilarity(request.Title, work.Title), "score": workMatchScore(*work, request)})
	episodes, err := s.Episodes(ctx, name, work.Ref)
	if err != nil {
		return nil, nil, "", err
	}
	episode := matchingEpisode(episodes, request)
	attempt.Stage = "episode"
	matchLog(ctx, "danmaku match episode candidates", applog.Fields{"provider": name, "count": len(episodes), "episodes": episodeSummary(episodes)})
	if episode == nil {
		code, detail := episodeMatchFailure(episodes, request)
		attempt.Detail = detail
		return nil, nil, code, nil
	}
	matchLog(ctx, "danmaku match episode selected", applog.Fields{"provider": name, "episodes": len(episodes), "episode_id": episode.EpisodeID, "title": episode.Title, "number": episode.Number, "source_duration_ms": episode.DurationMS, "local_duration_ms": request.DurationMS, "policy": matchPolicy})
	binding := &Binding{LocalKey: request.LocalKey, Episode: *episode, Confirmed: true}
	prior, err := s.Binding(ctx, request.LocalKey)
	if err != nil {
		return nil, nil, "", err
	}
	if prior != nil && prior.Episode.Provider == episode.Provider && prior.Episode.EpisodeID == episode.EpisodeID {
		binding.OffsetMS = prior.OffsetMS
	}
	if err = ValidateBinding(*binding); err != nil {
		return nil, nil, "INVALID_EPISODE", err
	}
	attempt.Stage = "window"
	window, err := s.readBoundWindow(ctx, binding, request.PositionMS, request.FreshDays)
	if err != nil {
		return nil, nil, "", err
	}
	for _, warning := range window.Warnings {
		if warning == "pool_empty" {
			return nil, nil, "EMPTY_POOL", nil
		}
		if warning == "segment_not_found" {
			return nil, nil, "SEGMENT_NOT_FOUND", nil
		}
	}
	attempt.Stage = "complete"
	attempt.Detail = fmt.Sprintf("policy=%s; similarity=%d/1000; number=%s; source_duration_ms=%d; local_duration_ms=%d; items=%d; cached=%t; stale=%t", matchPolicy, workTitleSimilarity(request.Title, work.Title), episode.Number, episode.DurationMS, request.DurationMS, len(window.Items), window.Cached, window.Stale)
	return binding, window, "", nil
}
