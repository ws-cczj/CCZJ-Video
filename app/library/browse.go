package library

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

const browseCacheTTL = 30 * time.Minute

// NovelBrowsePage contains an ordered remote shelf, with cache provenance.
type NovelBrowsePage struct {
	ChannelID  string  `json:"channelId"`
	SourceName string  `json:"sourceName"`
	Items      []*Item `json:"items"`
	NextCursor string  `json:"nextCursor"`
	FetchedAt  int64   `json:"fetchedAt"`
	FromCache  bool    `json:"fromCache"`
	Stale      bool    `json:"stale"`
	Warning    string  `json:"warning"`
}

type browseCursor struct {
	SourceKey string `json:"sourceKey"`
	Version   string `json:"version"`
	Channel   string `json:"channel"`
	Offset    int    `json:"offset"`
}

// NovelBrowseChannels exposes implemented source routes, never local-library genres.
func (s *Service) NovelBrowseChannels() []provider.BrowseChannel {
	return provider.NovelBrowseChannels()
}

// defaultBrowseSource provisions the bundled, compiled text provider on first
// entry. Existing disabled/configured sources are respected instead of replaced.
func (s *Service) defaultBrowseSource(ctx context.Context) (*resolvedSource, error) {
	s.browseMu.Lock()
	defer s.browseMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sources, err := db.GetSourcesByKind(model.SourceKindNovel)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取小说浏览源失败")
	}
	var disabled bool
	for _, row := range sources {
		doc, parseErr := provider.ParseDocument(row.StrategyConfig, "novel")
		if parseErr != nil || doc.Adapter != "fanqie-web" {
			continue
		}
		if row.Enabled != 1 || row.AutoDisabledAt != 0 {
			disabled = true
			continue
		}
		return s.resolve(row.SourceKey, model.SourceKindNovel)
	}
	if disabled {
		return nil, apperror.New(apperror.Unavailable, "小说浏览源已停用，可在内容源中重新启用")
	}
	var bundled []struct {
		Name     string          `json:"name"`
		URL      string          `json:"apiUrl"`
		Document json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(bundledSourceCatalog, &bundled); err != nil {
		return nil, apperror.Wrap(apperror.Internal, err, "内置浏览源配置无效")
	}
	for _, row := range bundled {
		doc, parseErr := provider.ParseDocument(string(row.Document), "novel")
		if parseErr != nil || doc.Adapter != "fanqie-web" {
			continue
		}
		source, addErr := s.AddSource(model.SourceKindNovel, row.Name, row.URL, string(row.Document))
		if addErr != nil {
			return nil, addErr
		}
		return s.resolve(source.SourceKey, model.SourceKindNovel)
	}
	return nil, apperror.New(apperror.Unsupported, "当前没有可用的小说浏览源")
}

// BrowseNovels fetches real source shelves without a keyword or personal history.
// Only seven first-page snapshots are retained; each is scoped by source revision.
func (s *Service) BrowseNovels(ctx context.Context, channel, cursor string, refresh bool) (*NovelBrowsePage, error) {
	release := s.priority.begin()
	defer release()
	valid := false
	for _, row := range s.NovelBrowseChannels() {
		valid = valid || row.ID == channel
	}
	if !valid {
		return nil, apperror.New(apperror.Validation, "小说浏览分类无效")
	}
	resolved, err := s.defaultBrowseSource(ctx)
	if err != nil {
		return nil, err
	}
	offset := 0
	if cursor != "" {
		if len(cursor) > 1024 {
			return nil, apperror.New(apperror.Validation, "浏览分页标识无效")
		}
		raw, decodeErr := base64.RawURLEncoding.DecodeString(cursor)
		var position browseCursor
		if decodeErr != nil || json.Unmarshal(raw, &position) != nil || position.Offset < 1 || position.Offset > 10000 {
			return nil, apperror.New(apperror.Validation, "浏览分页标识无效")
		}
		if position.Channel != channel || position.SourceKey != resolved.source.SourceKey || position.Version != resolved.version {
			return nil, apperror.New(apperror.Conflict, "浏览来源或分类已变化，请重新打开书目")
		}
		offset = position.Offset
	}
	cacheKey := "novel.browse.v1." + channel
	var cached *NovelBrowsePage
	if offset == 0 {
		raw, readErr := db.GetSetting(cacheKey)
		if readErr != nil {
			return nil, apperror.Wrap(apperror.Storage, readErr, "读取书目缓存失败")
		}
		cached = decodeBrowseCache(raw, resolved.source.SourceKey, resolved.version, channel)
		if cached != nil {
			refs := make([]db.LibraryRef, 0, len(cached.Items))
			for _, item := range cached.Items {
				refs = append(refs, db.LibraryRef{Kind: "novel", SourceKey: item.SourceKey, RemoteID: item.RemoteID})
			}
			rows, err := db.GetLibraryItemsByRefs(ctx, refs)
			if err != nil {
				return nil, apperror.Wrap(apperror.Storage, err, "读取缓存书目失败")
			}
			byID := make(map[int]*model.LibraryItem, len(rows))
			for _, row := range rows {
				byID[row.Id] = row
			}
			for index, item := range cached.Items {
				row := byID[item.ID]
				if row == nil || row.RemoteID != item.RemoteID || row.LifecycleState != "active" {
					cached = nil
					break
				}
				// Rebuild proxy URLs from current rows; a restored cache must not
				// retain a previous host or reuse an unrelated local numeric ID.
				cached.Items[index] = s.toDTO(model.SourceKindNovel, row, map[string]string{row.SourceKey: resolved.source.Name})
			}
		}
		if cached != nil && !refresh && time.Since(time.Unix(cached.FetchedAt, 0)) < browseCacheTTL {
			cached.FromCache = true
			return cached, nil
		}
	}
	found, err := resolved.pipeline.Browse(ctx, channel, offset)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cached != nil {
			cached.FromCache, cached.Stale = true, true
			cached.Warning = "暂时无法更新，正在展示上次保存的书目"
			return cached, nil
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rows := make([]*model.LibraryItem, 0, len(found.Items))
	for _, item := range found.Items {
		rows = append(rows, &model.LibraryItem{RemoteID: item.RemoteID, RemoteURL: item.URL,
			Title: item.Title, Author: item.Author, Cover: item.Cover, Summary: item.Summary,
			Genres: item.Genres, Status: item.Status, Latest: item.Latest, Words: item.Words})
	}
	ids, err := db.UpsertLibraryItems(model.SourceKindNovel, resolved.source.SourceKey, rows, true)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "保存浏览书目失败")
	}
	out := &NovelBrowsePage{ChannelID: channel, SourceName: resolved.source.Name, Items: []*Item{}, FetchedAt: time.Now().Unix()}
	for index, row := range rows {
		if index >= len(ids) || ids[index] == 0 {
			continue
		}
		row.Id, row.Kind, row.SourceKey, row.LifecycleState = ids[index], "novel", resolved.source.SourceKey, "active"
		out.Items = append(out.Items, s.toDTO(model.SourceKindNovel, row, map[string]string{row.SourceKey: resolved.source.Name}))
	}
	if found.HasNext {
		raw, marshalErr := json.Marshal(browseCursor{SourceKey: resolved.source.SourceKey, Version: resolved.version, Channel: channel, Offset: found.NextOffset})
		if marshalErr != nil {
			return nil, fmt.Errorf("encode browse continuation: %w", marshalErr)
		}
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	if offset == 0 {
		// IDs are persisted separately; decoding verifies each cached source before reuse.
		raw, marshalErr := json.Marshal(struct {
			Version string `json:"version"`
			*NovelBrowsePage
		}{Version: resolved.version, NovelBrowsePage: out})
		if marshalErr != nil {
			return nil, fmt.Errorf("encode browse cache: %w", marshalErr)
		}
		if err := db.SetSetting(cacheKey, string(raw)); err != nil {
			out.Warning = "书目已加载，但暂时无法保存离线副本"
		}
	}
	return out, nil
}

func decodeBrowseCache(raw, sourceKey, version, channel string) *NovelBrowsePage {
	var cached struct {
		Version string `json:"version"`
		NovelBrowsePage
	}
	if json.Unmarshal([]byte(raw), &cached) != nil || cached.Version != version || cached.ChannelID != channel || cached.FetchedAt <= 0 || len(cached.Items) == 0 || len(cached.Items) > 200 {
		return nil
	}
	for _, item := range cached.Items {
		if item == nil || item.ID <= 0 || item.Kind != "novel" || item.SourceKey != sourceKey || item.RemoteID == "" || item.Title == "" {
			return nil
		}
	}
	return &cached.NovelBrowsePage
}
