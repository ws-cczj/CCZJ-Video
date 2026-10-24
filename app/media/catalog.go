package media

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/cache"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/detail"
	"cczjVideo/app/model"
	"cczjVideo/app/payload"
	"context"
	"encoding/json"
	"strings"
)

type VideoListReq struct {
	SourceKey  string `json:"source_key"`
	RecentDays int    `json:"recent_days"`
	// type_id 同时支持字符串与数字，保存为字符串
	TypeId   string `json:"type_id"`
	Year     string `json:"year"`    // 年份筛选（"all" 或具体年份）
	Area     string `json:"area"`    // 地区筛选（"all" 或具体地区）
	Keyword  string `json:"keyword"` // 关键词：标题/演员/导演/备注/年份/地区/类型 模糊匹配
	Sort     string `json:"sort"`    // "" 默认; "rating" 按评分; "hot" 按热度
	Cursor   string `json:"cursor"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r *VideoListReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey  string          `json:"source_key"`
		RecentDays json.RawMessage `json:"recent_days"`
		TypeId     json.RawMessage `json:"type_id"`
		Year       string          `json:"year"`
		Area       string          `json:"area"`
		Keyword    string          `json:"keyword"`
		Sort       string          `json:"sort"`
		Cursor     string          `json:"cursor"`
		Page       json.RawMessage `json:"page"`
		PageSize   json.RawMessage `json:"page_size"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	if v, err := payload.ScalarInt(raw.RecentDays, 0); err == nil {
		r.RecentDays = v
	}
	r.TypeId = payload.ScalarID(raw.TypeId)
	r.Year = strings.TrimSpace(raw.Year)
	r.Area = strings.TrimSpace(raw.Area)
	r.Keyword = strings.TrimSpace(raw.Keyword)
	r.Sort = strings.TrimSpace(raw.Sort)
	r.Cursor = strings.TrimSpace(raw.Cursor)
	if v, err := payload.ScalarInt(raw.Page, 1); err == nil {
		r.Page = v
	}
	if v, err := payload.ScalarInt(raw.PageSize, 20); err == nil {
		r.PageSize = v
	}
	return nil
}

type VideoListResp struct {
	Videos     []*model.Video `json:"videos"`
	Total      int            `json:"total"`
	NextCursor string         `json:"next_cursor"`
	// LocalVideos carries catalog-only matches the upstream search missed,
	// e.g. "是，大臣" for keyword "大臣" when the source API matches prefixes.
	LocalVideos []*model.Video `json:"local_videos,omitempty"`
}

func (s *Service) List(ctx context.Context, req VideoListReq) (*VideoListResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := db.FilterParams{
		TypeId:     req.TypeId,
		Year:       req.Year,
		Area:       req.Area,
		Keyword:    req.Keyword,
		Sort:       req.Sort,
		RecentDays: req.RecentDays,
		Cursor:     req.Cursor,
		Page:       req.Page,
		PageSize:   req.PageSize,
	}
	page, err := db.GetCatalogVideoPageContext(ctx, req.SourceKey, filter)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "get videos")
	}

	return &VideoListResp{Videos: page.Videos, Total: page.Total, NextCursor: page.NextCursor}, nil
}

// YearAreaResp 前端用于前端：返回所有可选年份 / 地区列表（用于筛选下拉框的选项）
type YearsResp struct {
	Years []string `json:"years"`
	Areas []string `json:"areas"`
}

func (s *Service) YearsAndAreas(ctx context.Context, sourceKey string) (*YearsResp, error) {
	years, areas, err := db.GetCatalogYearsAndAreasContext(ctx, sourceKey)
	if err != nil {
		return nil, err
	}
	return &YearsResp{Years: years, Areas: areas}, nil
}

// HistoryItemWithVideo 前端可用的"继续观看"条目：含视频名+封面，便于卡片展示
type HistoryItemWithVideo struct {
	GlobalID   int     `json:"global_id"`
	SourceKey  string  `json:"source_key"`
	VodId      string  `json:"vod_id"`
	EpNum      int     `json:"ep_num"`
	Position   float64 `json:"position"`
	UpdatedAt  string  `json:"updated_at"`
	VodName    string  `json:"vod_name"`
	VodPic     string  `json:"vod_pic"`
	VodRemarks string  `json:"vod_remarks"`
}

type VideoDetailReq struct {
	SourceKey string `json:"source_key"`
	GlobalID  int64  `json:"global_id"`
	VodId     string `json:"vod_id"`
	Refresh   bool   `json:"refresh"` // 为 true 时先从源站拉取最新数据再返回
}

func (r *VideoDetailReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		GlobalID  json.RawMessage `json:"global_id"`
		VodId     json.RawMessage `json:"vod_id"`
		Refresh   bool            `json:"refresh"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	if v, err := payload.ScalarInt64(raw.GlobalID, 0); err == nil {
		r.GlobalID = v
	}
	r.VodId = payload.ScalarID(raw.VodId)
	r.Refresh = raw.Refresh
	return nil
}

type VideoDetailResp struct {
	Video    *model.Video      `json:"video"`
	Episodes []*model.Episode  `json:"episodes"`
	Lines    []*model.PlayLine `json:"lines,omitempty"`
	Error    *detail.Error     `json:"error,omitempty"`
}

// GetVideoDetail always resolves a catalog identity and fetches remote detail.
// It intentionally has no SQLite detail write path.
func (s *Service) Detail(ctx context.Context, req VideoDetailReq) (*VideoDetailResp, error) {
	if req.SourceKey == "" || (req.GlobalID <= 0 && req.VodId == "") {
		return nil, apperror.New(apperror.Validation, "source_key and global_id are required")
	}
	var result *detail.Result
	var err error
	if req.GlobalID > 0 {
		result, err = s.details.GetByGlobalContext(ctx, req.SourceKey, req.GlobalID, req.Refresh)
	} else {
		result, err = s.details.GetContext(ctx, req.SourceKey, req.VodId, req.Refresh)
	}
	if err != nil {
		if structured, ok := err.(*detail.Error); ok {
			return &VideoDetailResp{Video: structured.Fallback, Error: structured}, nil
		}
		return nil, err
	}
	return &VideoDetailResp{Video: result.Video, Episodes: result.Episodes, Lines: result.Lines}, nil
}

type VideoSearchReq struct {
	SourceKey string `json:"source_key"`
	Keyword   string `json:"keyword"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
}

func (r *VideoSearchReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		Keyword   string          `json:"keyword"`
		Page      json.RawMessage `json:"page"`
		PageSize  json.RawMessage `json:"page_size"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.Keyword = raw.Keyword
	if v, err := payload.ScalarInt(raw.Page, 1); err == nil {
		r.Page = v
	}
	if v, err := payload.ScalarInt(raw.PageSize, 20); err == nil {
		r.PageSize = v
	}
	return nil
}

// Search runs a remote keyword search and caches every hit into the
// source catalog as a side effect, so the Home/Detail views can list them
// without another request. Whether a cached hit resurrects a row the user
// deleted is decided by the catalog_revive_deleted setting.
//
// SearchSource (源搜索) deliberately does NOT cache: it is a preview across
// many sources, so its results only enter the catalog when the user presses
// 入库, which goes through ImportSourceVideos and always resurrects.

func (s *Service) Search(ctx context.Context, req VideoSearchReq) (*VideoListResp, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	keyword := strings.TrimSpace(req.Keyword)
	if keyword == "" {
		return &VideoListResp{Videos: []*model.Video{}, Total: 0}, nil
	}
	// Search is an on-demand remote operation. Project only catalog fields when
	// caching results locally; never enrich a search hit through detail requests.
	source, err := db.GetSourceByKeyContext(ctx, req.SourceKey)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "get source")
	}
	if source.GetKind() != model.SourceKindVideo {
		return nil, apperror.New(apperror.Unsupported, "视频搜索只支持视频采集源")
	}
	strategy := collect.CreateStrategyFromSource(source)
	if strategy == nil {
		return nil, apperror.New(apperror.Unsupported, "source strategy unavailable")
	}
	fetched, err := collect.FetchSearchPageContext(ctx, strategy, keyword, req.Page)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, apperror.Wrap(apperror.Unavailable, err, "remote search")
	}
	fetched.List, err = db.FilterSourceCollectVideos(req.SourceKey, fetched.List)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "读取禁用类型失败")
	}

	// Snapshot catalog membership before this search caches its own hits,
	// otherwise every result would come back flagged as already stored.
	remoteIDs := make([]string, 0, len(fetched.List))
	for _, v := range fetched.List {
		if v != nil {
			remoteIDs = append(remoteIDs, v.VodId.String())
		}
	}
	cached, err := db.ExistingCatalogVodIDs(req.SourceKey, remoteIDs)
	if err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "check catalog membership")
	}
	for _, v := range fetched.List {
		if v != nil {
			v.InCatalog = cached[strings.TrimSpace(v.VodId.String())]
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := db.UpsertCatalogItemsContext(ctx, req.SourceKey, fetched.List); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "cache search catalog")
	}
	for _, v := range fetched.List {
		if v != nil {
			v.VodContent = ""
			v.VodActor = ""
			v.VodDirector = ""
			v.VodPlayUrl = ""
			v.VodDownUrl = ""
		}
	}
	resp := &VideoListResp{Videos: fetched.List, Total: fetched.Total.Int()}
	if req.Page == 1 {
		resp.LocalVideos = s.searchLocalCatalog(ctx, req.SourceKey, keyword, remoteIDs)
	}
	return resp, nil
}

const localSearchLimit = 24

// searchLocalCatalog finds catalog rows whose title contains the keyword, which
// covers substring matches the upstream search API does not make. Rows already
// present in the remote result set are dropped to avoid duplicate cards.
func (s *Service) searchLocalCatalog(ctx context.Context, sourceKey, keyword string, exclude []string) []*model.Video {
	matches, err := db.SearchCatalogByTitleContext(ctx, sourceKey, keyword, localSearchLimit+len(exclude))
	if err != nil {
		applog.Warn("[SearchVideos] 本地目录补搜失败: %v", err)
		return nil
	}
	excluded := make(map[string]bool, len(exclude))
	for _, id := range exclude {
		excluded[strings.TrimSpace(id)] = true
	}
	out := make([]*model.Video, 0, localSearchLimit)
	for _, v := range matches {
		if v == nil || excluded[strings.TrimSpace(v.VodId.String())] {
			continue
		}
		out = append(out, v)
		if len(out) == localSearchLimit {
			break
		}
	}
	return out
}

type GetTypesReq struct {
	SourceKey string `json:"source_key"`
}

func (s *Service) Types(ctx context.Context, req GetTypesReq) ([]*model.VType, error) {
	return db.GetTypesContext(ctx, req.SourceKey)
}

type DeleteVideoReq struct {
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
}

func (s *Service) Delete(ctx context.Context, req DeleteVideoReq) error {
	if req.SourceKey == "" || req.VodId == "" {
		return apperror.New(apperror.Validation, "参数不完整")
	}
	// 走缓存层而不是直接删库：删除后这条视频的所有派生缓存都必须作废。
	change, err := s.store.DeleteCatalog(ctx, req.SourceKey, req.VodId)
	if err != nil {
		return err
	}
	cache.PublishCatalogChange(change, "视频已删除")
	return nil
}
