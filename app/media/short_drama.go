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
	key := req.SourceKey
	if key == "" {
		key = req.PreferredSource
	}
	if key == "" {
		return nil, apperror.New(apperror.Validation, "请先选择视频来源")
	}
	req.SourceKey = key
	src, err := db.GetSourceByKeyContext(ctx, key)
	if err != nil {
		return nil, err
	}
	if src.GetKind() != model.SourceKindVideo {
		return nil, apperror.New(apperror.Unsupported, "短剧只支持视频源")
	}
	return s.shortDramaSourcePage(ctx, src, req)
}

func (s *Service) shortDramaSourcePage(ctx context.Context, src *model.Source, req ShortDramaPageReq) (*ShortDramaPage, error) {
	types, err := db.ExportSourceTypesContext(ctx, src.SourceKey)
	if err != nil {
		return nil, err
	}
	related, err := db.GetShortDramaSourceTypesContext(ctx, src.SourceKey, false)
	if err != nil {
		return nil, err
	}
	reuse := req.CategoryID != "" && len(related) > 0
	// A stale scope must be restarted explicitly; reusing the same cursor after
	// refreshing its identity would mix two different provider result sets.

	return s.shortDramaProviderPage(ctx, src, req, types, !reuse)
}

func (s *Service) shortDramaProviderPage(ctx context.Context, src *model.Source, req ShortDramaPageReq, types []db.SourceTypeExport, refreshMetadata bool) (*ShortDramaPage, error) {
	strategy := collect.CreateStrategyFromSource(src)
	if strategy == nil {
		return nil, apperror.New(apperror.Unsupported, "来源策略不可用")
	}
	categories, related, err := sourceShortDramaTypes(ctx, src, strategy, types, refreshMetadata)
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, row := range related {
		allowed[row.SourceTypeID] = true
	}
	result := &ShortDramaPage{SourceKey: src.SourceKey, SourceName: src.Name, Videos: []*model.Video{}}
	ids := []string{}
	for _, row := range related {
		if allowed[row.SourceTypeID] {
			ids = append(ids, row.SourceTypeID)
		}
	}

	var categoryFailures []error
	candidates := make([]*model.Video, 0, len(ids))
	for _, id := range ids {
		candidates = append(candidates, &model.Video{TypeId: model.FlexibleString(id), TypeName: categories[id]})
	}
	policy, err := db.SelectSourceCollectVideosContext(ctx, src.SourceKey, candidates)
	if err != nil {
		return nil, err
	}
	eligible := map[string]bool{}
	for _, row := range policy.Videos {
		eligible[row.TypeId.String()] = true
	}
	eligibleIDs := []string{}
	for _, id := range ids {
		if eligible[id] {
			eligibleIDs = append(eligibleIDs, id)
		}
	}
	if len(eligibleIDs) == 0 {
		return result, nil
	}
	cursor, err := readDramaCursor(req, src.ApiUrl, eligibleIDs)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	checked, attempts := 0, 0
	for checked < len(cursor.Types) && attempts < 5 {
		index := cursor.Offset
		cursor.Offset = (cursor.Offset + 1) % len(cursor.Types)
		checked++
		state := &cursor.Types[index]
		if state.Done {
			continue
		}
		if attempts >= 3 && len(result.Videos) > 0 {
			cursor.Offset = index
			break
		}
		id := state.ID
		attempts++

		target := strategy.BuildListUrl(state.Page, collect.FetchOptions{TypeID: id, Limit: 24, Hours: req.RecentDays * 24})
		if keyword := strings.TrimSpace(req.Keyword); keyword != "" {
			search, ok := strategy.(interface {
				BuildCategorySearchURL(string, string, int, int) string
			})
			if !ok {
				return nil, apperror.New(apperror.Unsupported, "该源不支持分类搜索")
			}
			target = search.BuildCategorySearchURL(keyword, id, state.Page, req.RecentDays*24)
			if target == "" {
				return nil, apperror.New(apperror.Unsupported, "该源不支持分类搜索")
			}
		}
		fetched, err := collect.FetchWithStrategy(ctx, strategy, target)
		if err != nil {
			if strings.TrimSpace(req.Keyword) != "" && collect.IsSearchUnsupported(err) {
				return nil, &apperror.Error{Code: apperror.Unsupported, Message: "该源不支持关键词搜索，请清空关键词继续浏览", Cause: err}
			}
			if ctx.Err() != nil {
				return nil, err
			}
			categoryFailures = append(categoryFailures, err)
			continue
		}
		if fetched.Page.Int() > 0 && fetched.Page.Int() != state.Page {
			return nil, apperror.New(apperror.Unsupported, "来源未返回请求的页码")
		}
		if len(fetched.List) > 100 {
			return nil, apperror.New(apperror.Corrupt, "来源单页结果超出范围")
		}
		if len(result.Videos) > 0 && len(result.Videos)+len(fetched.List) > 100 {
			cursor.Offset = index
			break
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
			if !allowed[video.TypeId.String()] && !(categories[video.TypeId.String()] == "" && shortDramaCategory(video.TypeName)) {
				continue
			}
			video.TypeName = name
			accepted = append(accepted, video)
		}
		if len(accepted) == 0 && len(fetched.List) > 0 {
			categoryFailures = append(categoryFailures, apperror.New(apperror.Unsupported, "来源未按短剧分类返回结果"))
			continue
		}
		selected, err := db.SelectSourceCollectVideosContext(ctx, src.SourceKey, accepted)
		if err != nil {
			return nil, err
		}
		if err = db.UpsertCatalogItemsContext(ctx, src.SourceKey, selected.Videos); err != nil {
			return nil, err
		}
		for _, video := range selected.Videos {
			if seen[video.VodId.String()] {
				continue
			}
			seen[video.VodId.String()] = true
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
		state.Done = fetched.Pagecount.Int() <= state.Page || len(fetched.List) == 0 || state.Page >= 1000
		state.Page++
	}
	if len(result.Videos) == 0 && len(categoryFailures) > 0 {
		return nil, errors.Join(categoryFailures...)
	}
	result.CategoryID, result.HasMore, err = cursor.next()
	if err != nil {
		return nil, err
	}
	if len(result.Videos) > 100 {
		return nil, apperror.New(apperror.Corrupt, "来源聚合结果超出范围")
	}
	cached, err := db.GetCatalogVideoPageContext(ctx, src.SourceKey, db.FilterParams{ShortDrama: true, Page: 1, PageSize: 1, Keyword: req.Keyword, RecentDays: req.RecentDays})
	if err != nil {
		return nil, err
	}
	result.Total = cached.Total

	return result, nil
}

func sourceShortDramaTypes(ctx context.Context, src *model.Source, strategy collect.SourceStrategy, types []db.SourceTypeExport, refreshMetadata bool) (map[string]string, []db.SourceTypeExport, error) {
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
	if refreshMetadata {
		fetched, err := collect.FetchWithStrategy(ctx, strategy, metadataURL)
		if err != nil {
			return nil, nil, err
		}
		if len(fetched.Classes) > db.MaxSourceCategories {
			return nil, nil, apperror.New(apperror.Corrupt, "来源分类数量超出范围")
		}
		completed, completeErr := collect.CompleteCategoryParents(ctx, src.ApiUrl, fetched.Classes)
		if completeErr != nil {
			return nil, nil, completeErr
		}
		fetched.Classes = completed
		meta = fetched
		if len(meta.Classes) > 0 {
			categories = map[string]string{}
		}
	}
	available := map[string]bool{}
	if len(meta.Classes) > db.MaxSourceCategories {
		return nil, nil, apperror.New(apperror.Corrupt, "来源分类数量超出范围")
	}
	metadata := make([]db.SourceTypeExport, 0, len(meta.Classes))
	for _, row := range meta.Classes {
		id, name := strings.TrimSpace(row.ID.String()), strings.TrimSpace(row.Name)
		if id == "" || name == "" {
			continue
		}
		if len(id) > 120 || len(name) > 256 || len(row.Parent.String()) > 120 {
			return nil, nil, apperror.New(apperror.Corrupt, "来源分类格式无效")
		}
		categories[id] = name
		available[id] = true
		metadata = append(metadata, db.SourceTypeExport{SourceTypeID: id, ParentTypeID: row.Parent.String(), TypeName: name})
	}
	if err := db.MergeSourceCategoriesContext(ctx, src.SourceKey, metadata); err != nil {
		return nil, nil, err
	}
	related, err := db.GetShortDramaSourceTypesContext(ctx, src.SourceKey, false)
	if err != nil {
		return nil, nil, err
	}
	filtered := make([]db.SourceTypeExport, 0, len(related))
	for _, row := range related {
		if len(metadata) == 0 || available[row.SourceTypeID] {
			filtered = append(filtered, row)
		}
	}
	return categories, filtered, nil
}
