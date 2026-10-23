package danmaku

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cczjVideo/app/applog"
)

// Historical generic failures can be explained from already-owned metadata.
// This path never invokes a provider and never clears or extends its cooldown.
func (s *Service) explainCachedFailure(ctx context.Context, request MatchRequest, attempt *MatchAttempt) {
	if attempt.Code != "NO_WORK_MATCH" && attempt.Code != "NO_EPISODE_MATCH" {
		return
	}
	raw, _, err := s.store.Cached(ctx, discoveryCacheKey(attempt.Provider, "search", strings.TrimSpace(request.Title)), s.now())
	var rows []Candidate
	if err != nil || len(raw) == 0 || len(raw) > MaxPayload || json.Unmarshal(raw, &rows) != nil || rows == nil || len(rows) > 200 {
		return
	}
	for _, row := range rows {
		if row.Provider != attempt.Provider || row.Ref == "" {
			return
		}
	}
	matchLog(ctx, "danmaku match cached candidates", applog.Fields{"provider": attempt.Provider, "candidates": candidateSummary(rows), "stored_code": attempt.Code})
	work := matchingWork(rows, request)
	if attempt.Code == "NO_WORK_MATCH" && work == nil {
		attempt.Code, attempt.Detail = workMatchFailure(rows, request)
		attempt.Stage = "work_cache"
		attempt.Detail = "cached_evidence=true; " + attempt.Detail
		return
	}
	if work == nil {
		return
	}
	if attempt.Code == "NO_EPISODE_MATCH" {
		raw, _, err = s.store.Cached(ctx, discoveryCacheKey(attempt.Provider, "episodes", work.Ref), s.now())
		var episodes []Episode
		if err != nil || len(raw) == 0 || len(raw) > MaxPayload || json.Unmarshal(raw, &episodes) != nil || episodes == nil || len(episodes) > 5000 {
			return
		}
		for _, row := range episodes {
			if row.Provider != attempt.Provider || row.EpisodeID == "" {
				return
			}
		}
		matchLog(ctx, "danmaku match cached episode candidates", applog.Fields{"provider": attempt.Provider, "count": len(episodes), "episodes": episodeSummary(episodes), "stored_code": attempt.Code})
		if matchingEpisode(episodes, request) == nil {
			attempt.Code, attempt.Detail = episodeMatchFailure(episodes, request)
			attempt.Stage = "episode_cache"
			attempt.Detail = "cached_evidence=true; " + attempt.Detail
		}
	}
}

type matchTraceKey struct{}

func matchLog(ctx context.Context, message string, fields applog.Fields) {
	fields["mode"] = "video"
	fields["operation"] = "danmaku.match"
	fields["match_id"], _ = ctx.Value(matchTraceKey{}).(string)
	applog.InfoFields(message, fields)
}

func candidateSummary(rows []Candidate) string {
	parts := []string{}
	for _, row := range rows[:min(5, len(rows))] {
		title := []rune(plainTitle(row.Title))
		parts = append(parts, fmt.Sprintf("%s / %s / %s", string(title[:min(80, len(title))]), row.Year, row.Kind))
	}
	return strings.Join(parts, "; ")
}

func episodeSummary(rows []Episode) string {
	parts := []string{}
	for _, row := range rows[:min(5, len(rows))] {
		title := []rune(plainTitle(row.Title))
		number := []rune(row.Number)
		parts = append(parts, fmt.Sprintf("%s / %s / %dms", string(number[:min(40, len(number))]), string(title[:min(80, len(title))]), row.DurationMS))
	}
	return strings.Join(parts, "; ")
}

// Keep rejection reasons separate from upstream transport failures. The compact
// facts are safe to show in the player; raw I/O errors belong only in native logs.
func workMatchFailure(rows []Candidate, request MatchRequest) (string, string) {
	named, eligible := 0, 0
	for _, row := range rows {
		if workTitleSimilarity(request.Title, row.Title) >= 800 {
			named++
		}
		if workMatchScore(row, request) >= 0 {
			eligible++
		}
	}
	detail := fmt.Sprintf("policy=%s; candidates=%d; similar_title=%d; eligible=%d", matchPolicy, len(rows), named, eligible)
	switch {
	case len(rows) == 0:
		return "WORK_SEARCH_EMPTY", detail
	default:
		return "WORK_TITLE_MISMATCH", detail
	}
}

func episodeMatchFailure(rows []Episode, request MatchRequest) (string, string) {
	n, numbered := episodeOrdinal(request.EpisodeLabel)
	movie := !numbered && matchMovie.MatchString(request.EpisodeLabel)
	sameNumber, special, specialNumber := 0, 0, 0
	for _, row := range rows {
		label := row.Number
		if label == "" {
			label = row.Title
		}
		remote, ok := episodeOrdinal(label)
		if matchExtra.MatchString(row.Title) {
			special++
			if numbered && ok && n == remote {
				specialNumber++
			}
			continue
		}
		if (!movie && (!numbered || !ok || n != remote)) || (movie && len(rows) != 1) {
			continue
		}
		sameNumber++
	}
	detail := fmt.Sprintf("policy=%s; episodes=%d; same_number=%d; special_rejected=%d; special_same_number=%d; local_duration_ms=%d", matchPolicy, len(rows), sameNumber, special, specialNumber, request.DurationMS)
	switch {
	case len(rows) == 0:
		return "EPISODE_EMPTY", detail
	case !numbered && !movie:
		return "EPISODE_LABEL_UNSUPPORTED", detail
	case sameNumber == 0 && specialNumber > 0:
		return "EPISODE_SPECIAL_REJECTED", detail
	case special == len(rows):
		return "EPISODE_SPECIAL_ONLY", detail
	case sameNumber == 0:
		return "EPISODE_NUMBER_MISMATCH", detail
	default:
		return "EPISODE_NUMBER_MISMATCH", detail
	}
}
