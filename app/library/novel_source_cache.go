package library

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/library/matching"
	"cczjVideo/app/model"
)

const novelSourceCooldown = 5 * time.Minute

type NovelCachedSource struct {
	Item      *Item  `json:"item"`
	Identity  string `json:"identity"`
	State     string `json:"state"`
	CheckedAt int64  `json:"checkedAt"`
	ErrorCode string `json:"errorCode"`
	Revision  string `json:"revision"`
	Family    string `json:"family"`
}
type NovelBookSourceCache struct {
	ItemID      int                  `json:"itemId"`
	Candidates  []*NovelCachedSource `json:"candidates"`
	CheckedAt   int64                `json:"checkedAt"`
	NextCheckAt int64                `json:"nextCheckAt"`
	Available   int                  `json:"available"`
	Target      int                  `json:"target"`
}

func (s *Service) cachedNovelSources(ctx context.Context, itemID int) (*NovelBookSourceCache, error) {
	anchor, err := db.GetLibraryItemContext(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if anchor.Kind != "novel" {
		return nil, apperror.New(apperror.Validation, "书源缓存仅适用于小说")
	}
	items, err := db.FindNovelSourceItems(ctx, itemID, matching.Normalize(anchor.Title), matching.Normalize(anchor.Author))
	if err != nil {
		return nil, err
	}
	installed, err := db.GetSourcesByKindContext(ctx, model.SourceKindNovel)
	if err != nil {
		return nil, err
	}
	sources := map[string]*model.Source{}
	names := map[string]string{}
	for _, source := range installed {
		sources[source.SourceKey] = source
		names[source.SourceKey] = source.Name
	}
	ids := make([]int, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.Id)
	}
	checks, err := db.ReadNovelSourceChecks(ctx, ids)
	if err != nil {
		return nil, err
	}
	limits, err := db.ReadNovelSourceLimits(ctx, ids)
	if err != nil {
		return nil, err
	}
	refresh, err := db.ReadNovelSourceRefresh(ctx, itemID)
	if err != nil {
		return nil, err
	}
	out := &NovelBookSourceCache{ItemID: itemID, Target: 3, Candidates: []*NovelCachedSource{}, CheckedAt: refresh.CheckedAt}
	if refresh.CheckedAt > 0 {
		out.NextCheckAt = refresh.CheckedAt + int64(novelSourceCooldown/time.Second)
	}
	seen := map[string]bool{}
	for _, item := range items {
		identity := matching.Identity(anchor.Title, anchor.Author, item.Title, item.Author)
		if identity == "mismatch" || seen[item.SourceKey] {
			continue
		}
		seen[item.SourceKey] = true
		entry := &NovelCachedSource{Item: s.toDTO(model.SourceKindNovel, item, names), Identity: identity, State: "candidate"}
		if item.Id == itemID {
			entry.Identity = "verified"
		}
		if source := sources[item.SourceKey]; source != nil {
			entry.Revision = sourceRevision(source)
			if u, err := url.Parse(source.ApiUrl); err == nil {
				entry.Family = u.Hostname()
			}
			check := checks[item.Id]
			entry.CheckedAt = check.CheckedAt
			entry.ErrorCode = check.ErrorCode
			if check.Revision != "" {
				entry.State = check.State
				if check.Revision != entry.Revision {
					entry.State = "stale"
				} else if entry.State == "read_failed" {
					switch check.ErrorCode {
					case "restricted", "missing", "warning":
						entry.State = "limited"
					default:
						entry.State = "temporary"
					}
				}
			}
			if limits[fmt.Sprintf("%d:%s", item.Id, entry.Revision)] && (entry.State == "sample_verified" || entry.State == "readable") {
				entry.State = "limited"
			}
			if (entry.State == "sample_verified" || entry.State == "readable") && check.CheckedAt < time.Now().Add(-novelSourceCooldown).Unix() && check.ObservedAt < time.Now().Add(-novelSourceCooldown).UnixNano() {
				entry.State = "stale"
			}
			if source.Enabled != 1 {
				entry.State = "paused"
			}
		} else {
			entry.State = "unavailable"
		}
		if !entry.Item.Available {
			entry.State = "unavailable"
		}
		out.Candidates = append(out.Candidates, entry)
		if len(out.Candidates) >= 12 {
			break
		}
	}
	counted := map[string]bool{}
	for _, row := range out.Candidates {
		if row.Identity != "verified" {
			continue
		}
		if row.State != "sample_verified" && row.State != "readable" {
			continue
		}
		family := row.Family
		if family == "" {
			family = row.Item.SourceKey
		}
		if !counted[family] {
			counted[family] = true
			out.Available++
		}
	}
	return out, nil
}

// NovelBookSources returns durable choices immediately. Refresh is a cancellable
// background operation, shared across consumers; the five-minute clock survives restart.
func (s *Service) NovelBookSources(ctx context.Context, itemID int, refresh bool) (*NovelBookSourceCache, error) {
	if !refresh {
		return s.cachedNovelSources(ctx, itemID)
	}
	return s.sourceReads.Do(ctx, fmt.Sprint(itemID), func(owner context.Context) (*NovelBookSourceCache, error) {
		out, err := s.cachedNovelSources(owner, itemID)
		if err != nil {
			return nil, err
		}
		clock, err := db.ReadNovelSourceRefresh(owner, itemID)
		if err != nil {
			return nil, err
		}
		due := clock.CheckedAt <= time.Now().Add(-novelSourceCooldown).Unix()
		if !due && clock.Generation <= clock.CompletedGeneration {
			return out, nil
		}
		work, cancel := context.WithTimeout(owner, time.Minute)
		defer cancel()
		probe := func(rows []*NovelCachedSource) error {
			sort.SliceStable(rows, func(i, j int) bool { return rows[i].CheckedAt < rows[j].CheckedAt })
			for index := 0; index < len(rows); index++ {
				row := rows[index]
				if work.Err() != nil {
					return work.Err()
				}
				if row.State == "paused" || row.State == "unavailable" || row.CheckedAt > time.Now().Add(-novelSourceCooldown).Unix() && row.State != "stale" {
					continue
				}
				call, release, err := s.priority.background(work)
				if err != nil {
					return err
				}
				err = s.probeNovelSource(call, row)
				release()
				if err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				if err != nil && owner.Err() == nil && work.Err() == nil {
					if !waitReading(work, time.Second) {
						return work.Err()
					}
					// Foreground reads may cancel this attempt, but do not finish
					// the book's refresh or skip the interrupted candidate.
					index--
				}
			}
			return nil
		}
		if due {
			if err := probe(out.Candidates); err != nil {
				return nil, err
			}
		}
		out, err = s.cachedNovelSources(owner, itemID)
		if err != nil {
			return nil, err
		}
		cursor := ""
		anchor, err := db.GetLibraryItemContext(owner, itemID)
		if err != nil {
			return nil, err
		}
		for round := 0; round < 3 && out.Available < out.Target && work.Err() == nil; round++ {
			call, release, callErr := s.priority.background(work)
			if callErr != nil {
				return nil, callErr
			}
			found, searchErr := s.searchPageStream(call, model.SourceKindNovel, anchor.Title, cursor, nil, false)
			release()
			if searchErr != nil {
				if apperror.CodeOf(searchErr) == apperror.Storage {
					return nil, searchErr
				}
				if errors.Is(searchErr, context.Canceled) && work.Err() == nil {
					if waitReading(work, time.Second) {
						round--
						continue
					}
				}
				return nil, searchErr
			}
			cursor = found.NextCursor
			out, err = s.cachedNovelSources(owner, itemID)
			if err != nil {
				return nil, err
			}
			if err := probe(out.Candidates); err != nil {
				return nil, err
			}
			out, err = s.cachedNovelSources(owner, itemID)
			if err != nil {
				return nil, err
			}
			if cursor == "" {
				break
			}
		}
		if owner.Err() != nil {
			return nil, owner.Err()
		}
		if work.Err() != nil {
			return nil, work.Err()
		}
		if err := db.SaveNovelSourceRefresh(owner, itemID, clock.Generation); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "保存书源复核时间失败")
		}
		return s.cachedNovelSources(owner, itemID)
	})
}

func (s *Service) probeNovelSource(ctx context.Context, row *NovelCachedSource) error {
	started := time.Now().UnixNano()
	call, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	check := db.NovelSourceCheck{ItemID: row.Item.ID, Revision: row.Revision, ObservedAt: started, State: "sample_verified"}
	resolved, err := s.resolveContext(call, row.Item.SourceKey, model.SourceKindNovel)
	if err != nil {
		return err
	}
	if resolved.version != row.Revision {
		return nil
	}
	item, err := db.GetLibraryItemContext(call, row.Item.ID)
	if err != nil {
		return err
	}
	detail, err := resolved.pipeline.Detail(call, resolved.source.ApiUrl, requestURL(resolved, item.RemoteURL, item.RemoteID))
	if err == nil && detail == nil {
		err = apperror.New(apperror.Corrupt, "未取得作品详情")
	}
	if err == nil && matching.Identity(row.Item.Title, row.Item.Author, detail.Title, detail.Author) == "mismatch" {
		err = apperror.New(apperror.Conflict, "来源作品身份已变化")
	}
	if err == nil {
		chapters, tocErr := resolved.pipeline.Toc(call, resolved.source.ApiUrl, detail)
		err = tocErr
		if err == nil {
			found := false
			for i, chapter := range chapters {
				if chapter == nil || chapter.IsVolume {
					continue
				}
				found = true
				next := ""
				if i+1 < len(chapters) && chapters[i+1] != nil {
					next = chapters[i+1].URL
				}
				content, readErr := resolved.pipeline.Content(call, resolved.source.ApiUrl, chapter, next)
				err = readErr
				if err == nil && (content == nil || len(content.Paragraphs) == 0) {
					err = apperror.New(apperror.NotFound, "样章没有正文")
				}
				if err == nil && (content.Truncated || content.Garbled || content.FontObfuscated) {
					err = apperror.New(apperror.Corrupt, "样章正文异常")
				}
				break
			}
			if !found {
				err = apperror.New(apperror.NotFound, "来源未返回可读取章节")
			}
		}
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		check.ErrorCode = string(apperror.Public(err).Code)
		switch apperror.CodeOf(err) {
		case apperror.Cancelled, apperror.Storage, apperror.Internal:
			return err
		case apperror.AuthenticationRequired, apperror.PermissionDenied:
			check.State = "limited"
		case apperror.Timeout, apperror.RateLimited, apperror.Unavailable:
			check.State = "temporary"
		default:
			check.State = "invalid"
		}
	}
	check.CheckedAt = time.Now().Unix()
	if err := db.SaveNovelSourceCheck(ctx, check); err != nil {
		return apperror.Wrap(apperror.Storage, err, "保存作品来源复核失败")
	}
	return nil
}
