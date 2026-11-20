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
	FreshDays    int    `json:"freshDays"` // Retained for callers compiled with the previous cache policy.
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

const matchPolicy = "episode-priority-v4"

const (
	matchParallelism  = 3
	matchProbeTimeout = 12 * time.Second
	// Leave time for queued probes, cancellation and publishing the result.
	MatchRequestTimeout = time.Duration((len(builtinProviders)+matchParallelism-1)/matchParallelism)*matchProbeTimeout + 7*time.Second
)

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
	request.FreshDays = 0
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
	order := settings.orderedProviders()
	result = MatchResult{Attempts: []MatchAttempt{}, TraceID: traceID}
	identity := sha256.Sum256([]byte(request.LocalKey))
	matchLog(ctx, "danmaku match started", applog.Fields{"media_id": hex.EncodeToString(identity[:8]), "title": request.Title, "year": request.Year, "kind": request.Kind, "episode": request.EpisodeLabel, "duration_ms": request.DurationMS, "providers": order})
	if len(order) == 0 {
		return result, nil
	}
	batches := (len(order) + matchParallelism - 1) / matchParallelism
	workCtx, cancel := context.WithTimeout(ctx, time.Duration(batches)*matchProbeTimeout+2*time.Second)
	defer cancel()
	values := make(chan providerMatch, len(order))
	var workers sync.WaitGroup
	for _, name := range order {
		result.Attempts = append(result.Attempts, MatchAttempt{Provider: name, Status: "not_used"})
	}
	start := func(i int) {
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
		}(i, order[i])
	}
	// Queue lower-priority sources until a probe slot becomes available.
	started := min(cap(s.matchSlots), len(order))
	for i := 0; i < started; i++ {
		start(i)
	}
	// Always join the owned probes before publishing the match or closing storage.
	defer func() { cancel(); workers.Wait() }()
	received := make([]*providerMatch, len(order))
	next := 0
	selected := -1
	usable := false
collect:
	for count := 0; count < len(order); count++ {
		select {
		case <-ctx.Done():
			return MatchResult{}, ctx.Err()
		case <-workCtx.Done():
			break collect
		case value := <-values:
			copy := value
			received[value.index] = &copy
			result.Attempts[value.index] = value.attempt
			usable = usable || value.binding != nil
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
		if !usable && started < len(order) && workCtx.Err() == nil {
			start(started)
			started++
		}
	}
	if selected >= 0 || workCtx.Err() != nil {
		timedOut := errors.Is(workCtx.Err(), context.DeadlineExceeded)
		cancel()
		workers.Wait()
		for len(values) > 0 {
			value := <-values
			result.Attempts[value.index] = value.attempt
			received[value.index] = &value
		}
		if err := ctx.Err(); err != nil {
			return MatchResult{}, err
		}
		if timedOut {
			for i, value := range received {
				if value == nil {
					result.Attempts[i].Code = string(apperror.Timeout)
				} else if selected < 0 && value.binding != nil {
					selected = i
				}
			}
		}
	}
	if selected >= 0 {
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
		if ctx.Err() != nil && (value.err != nil || value.attempt.Status == "cancelled") {
			value.err = nil
			value.attempt.Status = "cancelled"
			value.attempt.Code = ""
			value.attempt.RetryAt = 0
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				value.attempt.Code = string(apperror.Timeout)
			}
		}
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
	if failed != nil && (!catalogueCanRetry(failed) || playbackWorkPrefix(request.LocalKey) == "" || request.DurationMS <= 0) {
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
	probeCtx, stop := context.WithTimeout(ctx, matchProbeTimeout)
	binding, window, code, err := s.probeMatch(probeCtx, request, name, &value.attempt, failed)
	diagnosticErr = err
	stop()
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || apperror.Is(err, apperror.Cancelled) {
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
	if failed != nil && value.attempt.Stage == "cooldown" && err == nil {
		s.cooldownHits.Add(1)
		s.explainCachedFailure(ctx, request, &value.attempt)
		return value
	}
	if err == nil && value.attempt.RetryAt != 0 {
		if value.attempt.Status == "cooling_down" {
			s.cooldownHits.Add(1)
		}
		return value
	}
	if code == "" {
		code = "NO_MATCH"
	}
	value.attempt.Code = code
	now := s.now()
	retryAt := now.Add(matchCooldown(code, diagnosticErr))
	value.attempt.RetryAt = retryAt.Unix()
	if err = s.store.saveMatchFailure(ctx, request.LocalKey, name, fingerprint, code, now, retryAt); err != nil {
		value.err = apperror.Wrap(apperror.Storage, err, "保存弹幕匹配冷却失败")
		value.attempt.Status = "error"
	}
	return value
}
func (s *Service) probeMatch(ctx context.Context, request MatchRequest, name string, attempt *MatchAttempt, failed *MatchAttempt) (*Binding, *Window, string, error) {
	work, episode, count, catalogErr := s.verifiedCatalog(ctx, request, name)
	if catalogErr != nil {
		matchLog(ctx, "danmaku stored catalogue unavailable", applog.Fields{"provider": name, "code": apperror.CodeOf(catalogErr)})
		if failed != nil || ctx.Err() != nil || catalogueFailureStopsSearch(catalogErr) {
			attempt.Stage = "episodes"
			return nil, nil, "", catalogErr
		}
	}
	if episode != nil {
		return s.matchEpisodeWindow(ctx, request, work, episode, count, attempt)
	}
	if failed != nil {
		*attempt = *failed
		attempt.Stage = "cooldown"
		return nil, nil, failed.Code, nil
	}
	attempt.Stage = "search"
	search, err := s.matchSearch(ctx, request, name)
	if err != nil {
		return nil, nil, "", err
	}
	rows, work := search.rows, search.work
	if search.attempt.Status != "cooling_down" {
		matchLog(ctx, "danmaku match candidates", applog.Fields{"provider": name, "count": len(rows), "candidates": candidateSummary(rows)})
	}
	if work == nil {
		*attempt = search.attempt
		return nil, nil, attempt.Code, nil
	}
	attempt.Stage = "episodes"
	matchLog(ctx, "danmaku match work selected", applog.Fields{"provider": name, "title": work.Title, "year": work.Year, "kind": work.Kind, "ref": work.Ref, "policy": matchPolicy, "title_similarity": candidateTitleSimilarity(*work, request.Title), "score": workMatchScore(*work, request)})
	episodes, err := s.Episodes(ctx, name, work.Ref)
	if err != nil {
		return nil, nil, "", err
	}
	episode = matchingEpisode(episodes, request)
	attempt.Stage = "episode"
	matchLog(ctx, "danmaku match episode candidates", applog.Fields{"provider": name, "count": len(episodes), "episodes": episodeSummary(episodes)})
	if episode == nil {
		code, detail := episodeMatchFailure(episodes, request)
		if missingEpisode(code) {
			if next := fallbackWork(rows, request, work); next != nil {
				matchLog(ctx, "danmaku checking alternate work", applog.Fields{"provider": name, "ref": next.Ref, "first_result": code})
				attempt.Stage = "episodes"
				episodes, err = s.Episodes(ctx, name, next.Ref)
				if err != nil {
					return nil, nil, "", err
				}
				work = next
				attempt.Stage = "episode"
				episode = matchingEpisode(episodes, request)
				if episode == nil {
					code, detail = episodeMatchFailure(episodes, request)
				}
			}
		}
		if episode == nil {
			attempt.Detail = detail
			return nil, nil, code, nil
		}
	}
	return s.matchEpisodeWindow(ctx, request, work, episode, len(episodes), attempt)
}

func (s *Service) matchEpisodeWindow(ctx context.Context, request MatchRequest, work *Candidate, episode *Episode, count int, attempt *MatchAttempt) (*Binding, *Window, string, error) {
	matchLog(ctx, "danmaku match episode selected", applog.Fields{"provider": episode.Provider, "episodes": count, "episode_id": episode.EpisodeID, "title": episode.Title, "number": episode.Number, "source_duration_ms": episode.DurationMS, "local_duration_ms": request.DurationMS, "policy": matchPolicy})
	binding := &Binding{LocalKey: request.LocalKey, Episode: *episode, Confirmed: true, work: &matchedWork{Candidate: *work, Identity: workIdentity(request)}}
	prior, err := s.Binding(ctx, request.LocalKey)
	if err != nil {
		return nil, nil, "", err
	}
	if prior != nil && prior.Episode.Provider == episode.Provider && prior.Episode.EpisodeID == episode.EpisodeID &&
		(!seriesScopedProvider(episode.Provider) || prior.Episode.SeriesID == episode.SeriesID) {
		binding.OffsetMS = prior.OffsetMS
	}
	if err = ValidateBinding(*binding); err != nil {
		return nil, nil, "INVALID_EPISODE", err
	}
	attempt.Stage = "window"
	window, err := s.readBoundWindow(ctx, binding, request.PositionMS)
	if err != nil {
		return nil, nil, "", err
	}
	if code := window.matchFailure(); code != "" {
		return nil, nil, code, nil
	}
	attempt.Stage = "complete"
	attempt.Detail = fmt.Sprintf("policy=%s; similarity=%d/1000; number=%s; source_duration_ms=%d; local_duration_ms=%d; items=%d; cached=%t; stale=%t", matchPolicy, candidateTitleSimilarity(*work, request.Title), episode.Number, episode.DurationMS, request.DurationMS, len(window.Items), window.Cached, window.Stale)
	return binding, window, "", nil
}

func missingEpisode(code string) bool {
	switch code {
	case "EPISODE_EMPTY", "EPISODE_NUMBER_MISMATCH", "EPISODE_SPECIAL_REJECTED", "EPISODE_SPECIAL_ONLY":
		return true
	default:
		return false
	}
}
