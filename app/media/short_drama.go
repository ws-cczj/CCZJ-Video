package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type ShortDramaPageReq struct {
	SourceKey       string `json:"source_key"`
	PreferredSource string `json:"preferred_source"`
	CategoryID      string `json:"category_id"`
	Keyword         string `json:"keyword"`
	Page            int    `json:"page"`
	RecentDays      int    `json:"recent_days"`
}
type ShortDramaPage struct {
	SourceKey  string         `json:"source_key"`
	SourceName string         `json:"source_name"`
	CategoryID string         `json:"category_id"`
	Videos     []*model.Video `json:"videos"`
	Total      int            `json:"total"`
	HasMore    bool           `json:"has_more"`
}

func shortDramaCategory(name string) bool {
	return strings.Contains(name, "短剧") || strings.Contains(name, "短劇") || strings.Contains(name, "短片剧")
}

// ShortDramaPage reads a bounded provider page and saves only catalog fields,
// like ordinary search. Detail/media stay owned by the existing detail service.
// The selected provider/category are returned so later pages never switch source.
func (s *Service) ShortDramaPage(ctx context.Context, req ShortDramaPageReq) (*ShortDramaPage, error) {
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Page > 1000 {
		return nil, apperror.New(apperror.Validation, "页码超出范围")
	}
	if req.RecentDays < 0 || req.RecentDays > 365 {
		return nil, apperror.New(apperror.Validation, "更新日期范围无效")
	}
	sources, err := db.GetSourcesByKindContext(ctx, model.SourceKindVideo)
	if err != nil {
		return nil, err
	}
	if req.SourceKey != "" {
		src, err := db.GetSourceByKeyContext(ctx, req.SourceKey)
		if err != nil {
			return nil, err
		}
		if src.GetKind() != model.SourceKindVideo {
			return nil, apperror.New(apperror.Unsupported, "短剧只支持视频源")
		}
		sources = []*model.Source{src}
	} else {
		for i, src := range sources {
			if src.SourceKey == req.PreferredSource {
				sources[0], sources[i] = sources[i], sources[0]
				break
			}
		}
	}
	empty := &ShortDramaPage{Videos: []*model.Video{}}
	var failures []error
	attempted := 0
	for _, src := range sources {
		if req.SourceKey == "" && src.Enabled == 0 {
			continue
		}
		if attempted >= 5 {
			break
		}
		attempted++
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		pageCtx, stop := context.WithTimeout(ctx, 6*time.Second)
		page, err := s.shortDramaSourcePage(pageCtx, src, req)
		stop()
		if err != nil {
			if req.SourceKey != "" {
				return nil, err
			}
			failures = append(failures, err)
			continue
		}
		if len(page.Videos) > 0 {
			return page, nil
		}
		if page.SourceKey != "" {
			empty = page
		}
	}
	if len(failures) > 0 {
		return nil, apperror.Wrap(apperror.Unavailable, errors.Join(failures...), "短剧来源暂不可用，请重试")
	}
	return empty, nil
}

func (s *Service) shortDramaSourcePage(ctx context.Context, src *model.Source, req ShortDramaPageReq) (*ShortDramaPage, error) {
	strategy := collect.CreateStrategyFromSource(src)
	if strategy == nil {
		return nil, apperror.New(apperror.Unsupported, "来源策略不可用")
	}
	types, err := db.ExportSourceTypesContext(ctx, src.SourceKey)
	if err != nil {
		return nil, err
	}
	categories := map[string]string{}
	for _, row := range types {
		categories[row.SourceTypeID] = row.TypeName
	}
	// Metadata is read through the same transport, decoder and network budget.
	metadataURL := strategy.BuildListUrl(1, collect.FetchOptions{Limit: 1})
	if categories, ok := strategy.(interface{ BuildCategoriesURL() string }); ok {
		metadataURL = categories.BuildCategoriesURL()
	}
	meta := &collect.FetchResult{}
	// A locked next page/search can reuse the catalog's category identity.
	// Page-one browsing still refreshes metadata, so changed IDs can recover.
	if req.CategoryID == "" || !shortDramaCategory(categories[req.CategoryID]) || (req.Page == 1 && strings.TrimSpace(req.Keyword) == "") {
		meta, err = collect.FetchWithStrategy(ctx, strategy, metadataURL)
		if err != nil {
			return nil, err
		}
	}
	for _, row := range meta.Classes {
		categories[row.ID.String()] = row.Name
	}
	result := &ShortDramaPage{SourceKey: src.SourceKey, SourceName: src.Name, Videos: []*model.Video{}}
	ids := []string{}
	for _, row := range meta.Classes {
		if shortDramaCategory(row.Name) {
			ids = append(ids, row.ID.String())
		}
	}
	if len(ids) == 0 {
		for _, row := range types {
			if shortDramaCategory(row.TypeName) {
				ids = append(ids, row.SourceTypeID)
			}
		}
	}
	if req.CategoryID != "" {
		if !shortDramaCategory(categories[req.CategoryID]) {
			return nil, apperror.New(apperror.Validation, "来源短剧分类已变化，请刷新")
		}
		ids = []string{req.CategoryID}
	}
	tried := 0
	for _, id := range ids {
		if tried >= 3 {
			break
		}
		tried++
		// Apply the same category bans before making the category request.
		policy, err := db.SelectSourceCollectVideosContext(ctx, src.SourceKey, []*model.Video{{TypeId: model.FlexibleString(id), TypeName: categories[id]}})
		if err != nil {
			return nil, err
		}
		if len(policy.Videos) == 0 {
			continue
		}
		target := strategy.BuildListUrl(req.Page, collect.FetchOptions{TypeID: id, Limit: 24, Hours: req.RecentDays * 24})
		if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
			search, ok := strategy.(interface {
				BuildCategorySearchURL(string, string, int, int) string
			})
			if !ok {
				return nil, apperror.New(apperror.Unsupported, "该源不支持分类搜索")
			}
			target = search.BuildCategorySearchURL(keyword, id, req.Page, req.RecentDays*24)
			if target == "" {
				return nil, apperror.New(apperror.Unsupported, "该源不支持分类搜索")
			}
		}
		fetched, err := collect.FetchWithStrategy(ctx, strategy, target)
		if err != nil {
			if strings.TrimSpace(req.Keyword) != "" && collect.IsSearchUnsupported(err) {
				return nil, &apperror.Error{Code: apperror.Unsupported, Message: "该源不支持关键词搜索，请清空关键词继续浏览", Cause: err}
			}
			return nil, err
		}
		if fetched.Page.Int() > 0 && fetched.Page.Int() != req.Page {
			return nil, apperror.New(apperror.Unsupported, "来源未返回请求的页码")
		}
		if len(fetched.List) > 100 {
			return nil, apperror.New(apperror.Corrupt, "来源单页结果超出范围")
		}
		accepted := []*model.Video{}
		for _, video := range fetched.List {
			if video == nil {
				continue
			}
			// Never trust a server to honour t, nor infer a type from the title.
			name := categories[video.TypeId.String()]
			if name == "" {
				name = video.TypeName
			}
			if !shortDramaCategory(name) {
				continue
			}
			video.TypeName = name
			accepted = append(accepted, video)
		}
		if len(accepted) != len(fetched.List) {
			return nil, apperror.New(apperror.Unsupported, "来源未按短剧分类返回结果")
		}
		selected, err := db.SelectSourceCollectVideosContext(ctx, src.SourceKey, accepted)
		if err != nil {
			return nil, err
		}
		if err = db.UpsertCatalogItemsContext(ctx, src.SourceKey, selected.Videos); err != nil {
			return nil, err
		}
		for _, video := range selected.Videos {
			item, err := db.GetCatalogItemContext(ctx, src.SourceKey, video.VodId.String())
			if errors.Is(err, sql.ErrNoRows) {
				continue // Recycled or hidden catalog entries stay hidden.
			}
			if err != nil {
				return nil, err
			}
			if item.LifecycleState != "active" {
				continue
			}
			video.GlobalId = item.GlobalID
			video.VodPlayUrl = ""
			video.VodDownUrl = ""
			video.VodContent = ""
			result.Videos = append(result.Videos, video)
		}
		result.CategoryID = id
		result.Total = fetched.Total.Int()
		result.HasMore = fetched.Pagecount.Int() > req.Page
		if len(result.Videos) > 0 || req.CategoryID != "" || req.Keyword != "" {
			return result, nil
		}
	}
	return result, nil
}
