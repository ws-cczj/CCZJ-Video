package library

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
)

type ComicBrowsePage struct {
	ChannelID  string  `json:"channelId"`
	SourceName string  `json:"sourceName"`
	Items      []*Item `json:"items"`
	FetchedAt  int64   `json:"fetchedAt"`
	FromCache  bool    `json:"fromCache"`
	Stale      bool    `json:"stale"`
	Warning    string  `json:"warning"`
}

func (s *Service) ComicBrowseChannels() []provider.BrowseChannel {
	return []provider.BrowseChannel{
		{ID: "guazi", Title: "人气国漫", Description: "瓜子漫画 · 热门作品"},
		{ID: "dongman", Title: "咚漫精选", Description: "恋爱 · 奇幻 · 校园 · 悬疑"},
		{ID: "honkai", Title: "崩坏3", Description: "米哈游官方漫画"},
		{ID: "terra", Title: "泰拉记事社", Description: "明日方舟 · 终末地官方漫画"},
	}
}

// Browse installs only a bundled, adapted catalog and respects an existing
// disabled source. Community scripts are not involved in this operation.
func (s *Service) defaultComicBrowseSource(ctx context.Context, channel string) (*resolvedSource, error) {
	catalogID := map[string]string{"honkai": "mihon-bh3", "terra": "mihon-terra", "dongman": "mihon-dongman", "guazi": "mihon-guazi"}[channel]
	if catalogID == "" {
		return nil, apperror.New(apperror.Validation, "漫画浏览分类无效")
	}
	s.browseMu.Lock()
	defer s.browseMu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var bundled []struct {
		ID       string          `json:"id"`
		Name     string          `json:"name"`
		URL      string          `json:"apiUrl"`
		Document json.RawMessage `json:"document"`
	}
	if err := json.Unmarshal(bundledSourceCatalog, &bundled); err != nil {
		return nil, apperror.Wrap(apperror.Internal, err, "内置漫画源配置无效")
	}
	for _, candidate := range bundled {
		if candidate.ID != catalogID {
			continue
		}
		desired, parseErr := provider.ParseDocument(string(candidate.Document), "comic")
		if parseErr != nil {
			return nil, parseErr
		}
		sources, err := db.GetSourcesByKind(model.SourceKindComic)
		if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "读取漫画浏览源失败")
		}
		disabled := false
		for _, row := range sources {
			doc, parseErr := provider.ParseDocument(row.StrategyConfig, "comic")
			if parseErr != nil || doc.Adapter != desired.Adapter || strings.TrimRight(row.ApiUrl, "/") != strings.TrimRight(candidate.URL, "/") {
				continue
			}
			if row.Enabled != 1 || row.AutoDisabledAt != 0 {
				disabled = true
				continue
			}
			return s.resolve(row.SourceKey, model.SourceKindComic)
		}
		if disabled {
			return nil, apperror.New(apperror.Unavailable, "漫画浏览源已停用，可在内容源中重新启用")
		}
		source, addErr := s.AddSource(model.SourceKindComic, candidate.Name, candidate.URL, string(candidate.Document))
		if addErr != nil {
			return nil, addErr
		}
		return s.resolve(source.SourceKey, model.SourceKindComic)
	}
	return nil, apperror.New(apperror.Unsupported, "当前没有可用的漫画浏览源")
}

func (s *Service) BrowseComics(ctx context.Context, channel string, refresh bool) (*ComicBrowsePage, error) {
	release := s.priority.begin()
	defer release()
	resolved, err := s.defaultComicBrowseSource(ctx, channel)
	if err != nil {
		return nil, err
	}
	cacheKey := "comic.browse.v1." + channel
	raw, err := db.GetSetting(cacheKey)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取漫画书目缓存失败")
	}
	cached := decodeComicBrowseCache(raw, resolved.source.SourceKey, resolved.version, channel)
	if cached != nil {
		refs := make([]db.LibraryRef, 0, len(cached.Items))
		for _, item := range cached.Items {
			refs = append(refs, db.LibraryRef{Kind: "comic", SourceKey: item.SourceKey, RemoteID: item.RemoteID})
		}
		rows, readErr := db.GetLibraryItemsByRefs(ctx, refs)
		if readErr != nil {
			return nil, apperror.Wrap(apperror.Storage, readErr, "读取漫画书目失败")
		}
		byRemote := make(map[string]*model.LibraryItem, len(rows))
		for _, row := range rows {
			byRemote[row.RemoteID] = row
		}
		for index, item := range cached.Items {
			row := byRemote[item.RemoteID]
			if row == nil || row.SourceKey != resolved.source.SourceKey || row.LifecycleState != "active" {
				cached = nil
				break
			}
			cached.Items[index] = s.toDTO(model.SourceKindComic, row, map[string]string{row.SourceKey: resolved.source.Name})
		}
		if cached != nil && !refresh && time.Since(time.Unix(cached.FetchedAt, 0)) < browseCacheTTL {
			cached.FromCache = true
			return cached, nil
		}
	}
	found, err := resolved.pipeline.ComicBrowse(ctx, resolved.source.ApiUrl, channel)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if cached != nil {
			cached.FromCache, cached.Stale = true, true
			cached.Warning = "暂时无法更新，正在展示上次保存的漫画"
			return cached, nil
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(found.Items) == 0 || len(found.Items) > 500 {
		return nil, apperror.New(apperror.Corrupt, "漫画书目为空或超出范围")
	}
	rows := make([]*model.LibraryItem, 0, len(found.Items))
	seen := make(map[string]bool)
	for _, item := range found.Items {
		if item == nil || item.RemoteID == "" || item.Title == "" || item.URL == "" {
			return nil, apperror.New(apperror.Corrupt, "漫画书目信息不完整")
		}
		if seen[item.RemoteID] {
			continue
		}
		seen[item.RemoteID] = true
		rows = append(rows, &model.LibraryItem{RemoteID: item.RemoteID, RemoteURL: item.URL, Title: item.Title, Author: item.Author, Cover: item.Cover, Summary: item.Summary, Genres: item.Genres, Status: item.Status, Latest: item.Latest})
	}
	ids, err := db.UpsertLibraryItems(model.SourceKindComic, resolved.source.SourceKey, rows, true)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "保存漫画书目失败")
	}
	out := &ComicBrowsePage{ChannelID: channel, SourceName: resolved.source.Name, Items: []*Item{}, FetchedAt: time.Now().Unix()}
	for index, row := range rows {
		if index >= len(ids) || ids[index] == 0 {
			continue
		}
		row.Id, row.Kind, row.SourceKey, row.LifecycleState = ids[index], "comic", resolved.source.SourceKey, "active"
		out.Items = append(out.Items, s.toDTO(model.SourceKindComic, row, map[string]string{row.SourceKey: resolved.source.Name}))
	}
	encoded, err := json.Marshal(struct {
		Version string `json:"version"`
		*ComicBrowsePage
	}{resolved.version, out})
	if err != nil {
		return nil, err
	}
	if err := db.SetSetting(cacheKey, string(encoded)); err != nil {
		out.Warning = "漫画已加载，但暂时无法保存离线书目"
	}
	return out, nil
}

func decodeComicBrowseCache(raw, sourceKey, version, channel string) *ComicBrowsePage {
	var cached struct {
		Version string `json:"version"`
		ComicBrowsePage
	}
	if json.Unmarshal([]byte(raw), &cached) != nil || cached.Version != version || cached.ChannelID != channel || cached.FetchedAt <= 0 || cached.FetchedAt > time.Now().Add(time.Minute).Unix() || len(cached.Items) == 0 || len(cached.Items) > 500 {
		return nil
	}
	seen := make(map[string]bool)
	for _, item := range cached.Items {
		if item == nil || item.ID <= 0 || item.Kind != "comic" || item.SourceKey != sourceKey || item.RemoteID == "" || item.Title == "" || seen[item.RemoteID] {
			return nil
		}
		seen[item.RemoteID] = true
	}
	return &cached.ComicBrowsePage
}
