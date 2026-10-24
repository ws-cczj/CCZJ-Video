package source

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"context"

	"strings"
)

type SearchSourceResult struct {
	FilteredCount     int            `json:"filtered_count,omitempty"`
	UnclassifiedCount int            `json:"unclassified_count,omitempty"`
	Limited           bool           `json:"limited,omitempty"`
	Total             int            `json:"total"`
	Page              int            `json:"page"`
	PageCount         int            `json:"page_count"`
	PageSize          int            `json:"page_size"`
	Videos            []*model.Video `json:"videos"`
	From              string         `json:"from"`
	Keyword           string         `json:"keyword"`
}

// SearchSource is the 源搜索 path: it streams remote hits straight to the UI
// and never writes the catalog. Nothing here resurrects soft-deleted rows,
// because the user is only previewing a source. Persisting these hits is an
// explicit action — see ImportSourceVideos, which always resurrects.
//
// This is the intentional counterpart to SearchVideos (app/media/catalog.go),
// which caches each hit as it searches.
func SearchSource(sourceKey string, keyword string, page int, pageSize int) (*SearchSourceResult, error) {
	return SearchSourceContext(context.Background(), sourceKey, keyword, page, pageSize, "")
}

func SearchSourceContext(ctx context.Context, sourceKey, keyword string, page, pageSize int, requestID string) (*SearchSourceResult, error) {
	return NewService().SearchContext(ctx, sourceKey, keyword, page, pageSize, requestID)
}
func (s *Service) SearchContext(ctx context.Context, sourceKey, keyword string, page, pageSize int, requestID string) (*SearchSourceResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return nil, apperror.New(apperror.Validation, "关键词不能为空")
	}

	src, err := db.GetSourceByKeyContext(ctx, sourceKey)
	if err != nil {
		return nil, apperror.Wrap(apperror.NotFound, err, "获取源失败")
	}
	if src.GetKind() != model.SourceKindVideo {
		return nil, apperror.New(apperror.Unsupported, "视频搜索只支持视频采集源")
	}

	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		advCfg := src.GetAdvConfig()
		pageSize = advCfg.CollectLimit
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	ctx = operation.WithFields(ctx, applog.Fields{"source_key": sourceKey, "keyword": keyword, "page": page, "page_size": pageSize, "ui_request_id": requestID,
		"source_config_sha256": applog.Fingerprint([]byte(src.ApiUrl + "\x00" + src.StrategyConfig + "\x00" + src.AdvConfigRaw))})
	operation.Stage(ctx, "source_loaded", nil)

	// Emit: 开始搜索
	emit := func(payload map[string]interface{}) {
		if s.publish != nil && ctx.Err() == nil {
			payload["requestId"] = requestID
			payload["source_key"] = sourceKey
			payload["keyword"] = keyword
			payload["page"] = page
			s.publish("search:progress", payload)
		}
	}
	emit(map[string]interface{}{
		"stage":   "fetching_list",
		"message": "正在从源站获取搜索结果...",
		"current": 0,
		"total":   0,
	})

	strategy := collect.CreateStrategyFromSource(src)
	if strategy == nil {
		return nil, apperror.New(apperror.Unsupported, "源站策略不可用")
	}
	ctx = operation.WithFields(ctx, applog.Fields{"strategy": strategy.GetStrategyName()})
	p, err := collect.FetchSearchPageContext(ctx, strategy, keyword, page)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, apperror.Wrap(apperror.Unavailable, err, "源站搜索失败")
	}
	if p == nil {
		return nil, apperror.New(apperror.Corrupt, "源站搜索未返回有效结果")
	}

	rawCount := len(p.List)
	selection, err := db.SelectSourceCollectVideos(sourceKey, p.List)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取禁用类型失败")
	}
	p.List = selection.Videos
	operation.Stage(ctx, "category_filter", applog.Fields{"normalized_count": rawCount, "visible_count": len(p.List), "disabled_count": selection.Disabled, "unclassified_count": selection.Unclassified, "limited": p.SearchLimited, "remote_total": p.Total.Int(), "remote_pages": p.Pagecount.Int()})
	// Search results are transient. Only ImportSourceVideos may persist the
	// user-selected items; otherwise every search hit would appear on Home.
	// Membership is probed against the catalog so the UI can mark hits the user
	// has already imported, including imports from earlier sessions.
	ids := make([]string, 0, len(p.List))
	for _, v := range p.List {
		if v != nil {
			ids = append(ids, v.VodId.String())
		}
	}
	cached, err := db.ExistingCatalogVodIDs(sourceKey, ids)
	if err != nil {
		applog.WarnFields("source search membership unavailable", operation.Fields(ctx, applog.Fields{"error": err}))
	}
	for _, v := range p.List {
		if v == nil {
			continue
		}
		v.InCatalog = cached[strings.TrimSpace(v.VodId.String())]
		v.VodContent, v.VodActor, v.VodDirector = "", "", ""
		v.VodPlayUrl, v.VodDownUrl, v.VodPlayFrom = "", "", ""
	}
	emit(map[string]interface{}{
		"stage":   "done",
		"message": "搜索完成",
		"current": len(p.List),
		"total":   len(p.List),
	})

	// 返回结果（不入库），分页元数据来自源站
	pageCount := p.Pagecount.Int()
	if pageCount <= 0 && p.Total.Int() > 0 {
		pageCount = (p.Total.Int() + pageSize - 1) / pageSize
	}
	total := p.Total.Int()
	if total < len(p.List) {
		total = len(p.List)
	}

	return &SearchSourceResult{
		FilteredCount:     selection.Disabled + selection.Unclassified,
		UnclassifiedCount: selection.Unclassified,
		Limited:           p.SearchLimited,
		Total:             total,
		Page:              page,
		PageCount:         pageCount,
		PageSize:          pageSize,
		Videos:            p.List,
		From:              sourceKey,
		Keyword:           keyword,
	}, nil
}

// ImportSourceVideos stores only catalog fields from user-selected search hits.
// It is the 入库 button, i.e. the explicit counterpart to the automatic
// collection/search caching paths, so it bypasses catalog_revive_deleted.
// 返回成功入库的条数

// ============================================================
// GetSourceParams: 返回采集接口参数规范（给前端规则指南用）
// ============================================================
type SourceParamDoc struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Desc    string `json:"desc"`
	Example string `json:"example"`
}

type SourceParamsDoc struct {
	BaseUrl       string           `json:"base_url"`
	PathParams    []SourceParamDoc `json:"path_params"`
	QueryAc       []SourceParamDoc `json:"query_ac"`
	QueryCommon   []SourceParamDoc `json:"query_common"`
	QueryAdvanced []SourceParamDoc `json:"query_advanced"`
}

func GetSourceParamsDoc(sourceKey string) (*SourceParamsDoc, error) {
	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, err
	}
	return &SourceParamsDoc{
		BaseUrl: src.ApiUrl,
		PathParams: []SourceParamDoc{
			{
				Name:    "from",
				Type:    "路径参数",
				Desc:    "指定播放源 / 解析分组，过滤返回对应格式的播放地址",
				Example: "/from/mtm3u8 代表只返回 mtm3u8 格式播放链接",
			},
		},
		QueryAc: []SourceParamDoc{
			{Name: "ac=list", Type: "query", Desc: "获取视频列表（分页列表数据）", Example: "?ac=list"},
			{Name: "ac=videolist", Type: "query", Desc: "获取全字段视频列表（数据量更大）", Example: "?ac=videolist"},
			{Name: "ac=detail", Type: "query", Desc: "获取视频详情（单条/多条完整信息，当前使用模式）", Example: "?ac=detail"},
		},
		QueryCommon: []SourceParamDoc{
			{Name: "pg", Type: "int", Desc: "页码，用于分页", Example: "pg=2 获取第 2 页数据"},
			{Name: "limit", Type: "int", Desc: "单页返回数据条数（多数站点上限 100）", Example: "limit=50"},
			{Name: "t / type_id", Type: "int", Desc: "分类 ID，按影视分类筛选", Example: "t=47"},
			{Name: "ids", Type: "string", Desc: "视频 ID，ac=detail 专用，多 ID 用英文逗号分隔", Example: "ids=136279,136278"},
			{Name: "wd", Type: "string", Desc: "搜索关键词，按片名模糊检索", Example: "wd=修仙"},
			{Name: "h", Type: "int", Desc: "小时数，筛选 N 小时内更新的资源", Example: "h=24"},
		},
		QueryAdvanced: []SourceParamDoc{
			{Name: "isend", Type: "0/1", Desc: "是否完结，1=全集完结，0=连载中", Example: "isend=1"},
			{Name: "year", Type: "string", Desc: "上映年份，支持单年份/年份区间", Example: "year=2026 或 year=2020-2026"},
			{Name: "sort_direct", Type: "string", Desc: "排序方向，默认按更新时间倒序", Example: "sort_direct=asc"},
			{Name: "vod_letter", Type: "string", Desc: "首字母筛选（拼音首字母）", Example: "vod_letter=W"},
		},
	}, nil
}
