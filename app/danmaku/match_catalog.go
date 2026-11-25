package danmaku

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

func workIdentity(request MatchRequest) string {
	raw, _ := json.Marshal([]string{normalizedTitle(request.Title), metadataYear(request.Year), mediaKind(request.Kind)})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// The player key stores mode, source, source-local work ID, and episode.
// Only numbered playback records with that exact stored parent may share a catalogue.
func playbackWorkPrefix(key string) string {
	var parts []json.RawMessage
	if json.Unmarshal([]byte(key), &parts) != nil || len(parts) != 4 || string(parts[0]) != `"video"` {
		return ""
	}
	for _, part := range parts[1:3] {
		var value string
		if json.Unmarshal(part, &value) != nil || strings.TrimSpace(value) == "" {
			return ""
		}
	}
	end := len(key) - len(parts[3]) - 1
	if end < 1 || key[end-1] != ',' || key[end:len(key)-1] != string(parts[3]) {
		return ""
	}
	return key[:end]
}

// A successful read identifies a source and catalogue for this local work.
// The remote episode is a witness used to validate a later catalogue lookup.
type catalogHint struct {
	Work    Candidate
	Witness Episode
}

func catalogHintKey(request MatchRequest, provider string) string {
	prefix := playbackWorkPrefix(request.LocalKey)
	if prefix == "" {
		return ""
	}
	return cacheKey(Episode{Provider: provider, EpisodeID: prefix}, "catalog-witness:"+workIdentity(request))
}

func (s *Service) catalogHint(ctx context.Context, request MatchRequest, provider string) *catalogHint {
	key := catalogHintKey(request, provider)
	if key == "" {
		return nil
	}
	raw, _, err := s.store.Cached(ctx, key, s.now())
	var hint catalogHint
	if err != nil || len(raw) == 0 || len(raw) > 8192 || json.Unmarshal(raw, &hint) != nil {
		return nil
	}
	if hint.Work.Provider != provider || hint.Work.Ref == "" || len(hint.Work.Ref) > 2048 ||
		len(hint.Work.Title) > 1024 || workMatchScore(hint.Work, request) < 0 || hint.Witness.Provider != provider ||
		ValidateBinding(Binding{LocalKey: request.LocalKey, Episode: hint.Witness, Confirmed: true}) != nil {
		return nil
	}
	return &hint
}

func (s *Service) rememberCatalog(ctx context.Context, request MatchRequest, result *providerMatch) {
	if result == nil || result.binding == nil || result.binding.work == nil ||
		result.window == nil || len(result.window.Items) == 0 || result.window.Stale {
		return
	}
	b := result.binding
	key := catalogHintKey(request, b.Episode.Provider)
	if key == "" {
		return
	}
	raw, err := json.Marshal(catalogHint{Work: b.work.Candidate, Witness: b.Episode})
	if err == nil && len(raw) <= 8192 {
		if err = s.store.Put(ctx, key, raw, s.now()); err != nil && ctx.Err() == nil {
			s.cacheFailures.Add(1)
			applog.WarnFields("danmaku catalogue cache write failed", applog.Fields{"provider": b.Episode.Provider, "code": apperror.Storage})
		}
	}
}

// A confirmed binding for this exact local episode can go straight to its pool.
func (s *Service) verifiedEpisodeBinding(ctx context.Context, request MatchRequest, provider string) (*Binding, error) {
	prior, err := s.Binding(ctx, request.LocalKey)
	if err != nil || prior == nil || prior.work == nil || request.DurationMS <= 0 {
		return nil, err
	}
	if prior.Episode.Provider != provider || prior.work.Candidate.Provider != provider ||
		prior.work.Identity != workIdentity(request) || workMatchScore(prior.work.Candidate, request) < 0 ||
		prior.Episode.DurationMS <= 0 || matchingEpisode([]Episode{prior.Episode}, request) == nil {
		return nil, nil
	}
	return prior, nil
}

func (s *Service) verifiedCatalog(ctx context.Context, request MatchRequest, provider string) (*Candidate, *Episode, int, error) {
	prefix := playbackWorkPrefix(request.LocalKey)
	if prefix == "" || request.DurationMS <= 0 {
		return nil, nil, 0, nil
	}
	if _, numbered := episodeOrdinal(request.EpisodeLabel); !numbered {
		return nil, nil, 0, nil
	}
	anchors, err := s.store.catalogAnchors(ctx, prefix, provider)
	if err != nil {
		return nil, nil, 0, apperror.Wrap(apperror.Storage, err, "读取已验证弹幕目录失败")
	}
	identity := workIdentity(request)
	var work *Candidate
	var anchor Episode
	for _, binding := range anchors {
		var candidate Candidate
		if binding.work != nil {
			candidate = binding.work.Candidate
			if binding.work.Identity != identity || candidate.Provider != provider || workMatchScore(candidate, request) < 0 {
				continue
			}
		} else {
			// Older Bilibili PGC bindings already carry a season ID and episode CID.
			// Uploader videos and old bindings without a catalogue identity are excluded.
			e := binding.Episode
			if provider != "bilibili" || !decimalID.MatchString(e.SeriesID) || !strings.HasPrefix(e.PageURL, "https://www.bilibili.com/bangumi/play/ep") {
				continue
			}
			candidate = Candidate{Provider: provider, Ref: "ss" + e.SeriesID, Title: request.Title, Year: request.Year, Kind: request.Kind}
		}
		if work != nil && candidate.Ref != work.Ref {
			return nil, nil, 0, nil
		}
		if work == nil {
			work, anchor = &candidate, binding.Episode
		}
	}
	if work == nil {
		hint := s.catalogHint(ctx, request, provider)
		if hint == nil {
			return nil, nil, 0, nil
		}
		work, anchor = &hint.Work, hint.Witness
	}
	episodes, err := s.Episodes(ctx, provider, work.Ref)
	if err != nil {
		return nil, nil, 0, err
	}
	anchorFound := false
	for _, e := range episodes {
		if e.Provider == anchor.Provider && e.SeriesID == anchor.SeriesID && e.EpisodeID == anchor.EpisodeID {
			anchorFound = true
			break
		}
	}
	episode := matchingEpisode(episodes, request)
	if !anchorFound || episode == nil || episode.DurationMS <= 0 ||
		(anchor.SeriesID != "" && episode.SeriesID != anchor.SeriesID) {
		matchLog(ctx, "danmaku stored catalogue rejected", applog.Fields{"provider": provider, "ref": work.Ref, "anchor_found": anchorFound})
		return nil, nil, 0, nil
	}
	matchLog(ctx, "danmaku stored catalogue reused", applog.Fields{"provider": provider, "ref": work.Ref, "episodes": len(episodes)})
	return work, episode, len(episodes), nil
}

func (s *Store) catalogAnchors(ctx context.Context, prefix, provider string) ([]Binding, error) {
	db, err := s.db()
	if err != nil {
		return nil, err
	}
	// A comma is the last byte of the prefix; the next byte value closes this
	// primary-key range without using a wildcard or scanning other local works.
	upper := prefix[:len(prefix)-1] + "-"
	rows, err := db.QueryContext(ctx, `SELECT local_key,payload FROM danmaku_bindings
		WHERE local_key>=? AND local_key<?
		  AND CASE WHEN json_valid(CAST(payload AS TEXT)) THEN json_extract(CAST(payload AS TEXT),'$.episode.provider') END=?
		ORDER BY updated_at DESC,local_key LIMIT 16`, prefix, upper, provider)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []Binding
	for rows.Next() {
		var key string
		var raw []byte
		if err := rows.Scan(&key, &raw); err != nil {
			return nil, err
		}
		if len(raw) <= 8192 {
			if binding, err := decodeBinding(raw, key); err == nil && binding.Episode.Provider == provider {
				result = append(result, *binding)
			}
		}
	}
	return result, rows.Err()
}

func catalogueCanRetry(failed *MatchAttempt) bool {
	if failed == nil {
		return false
	}
	switch failed.Code {
	case "WORK_SEARCH_EMPTY", "WORK_TITLE_MISMATCH", "WORK_OFFICIAL_MISSING", "NO_WORK_MATCH":
		return true
	default:
		return false
	}
}

func catalogueFailureStopsSearch(err error) bool {
	return errors.Is(err, context.Canceled) || apperror.Is(err, apperror.Storage) || apperror.Is(err, apperror.Internal)
}
