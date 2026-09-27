package handler

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/detail"
	"cczjVideo/app/model"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// parseInt64 同时接受数字和字符串形式的数字
func parseInt64(raw json.RawMessage, def int64) (int64, error) {
	if len(raw) == 0 {
		return def, nil
	}
	// 字符串形式："0"
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return def, err
		}
		if s == "" {
			return def, nil
		}
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return def, fmt.Errorf("parse %q: %w", s, err)
		}
		return v, nil
	}
	var v int64
	if err := json.Unmarshal(raw, &v); err != nil {
		return def, err
	}
	return v, nil
}

func parseInt(raw json.RawMessage, def int) (int, error) {
	v, err := parseInt64(raw, int64(def))
	if err != nil {
		return def, err
	}
	return int(v), nil
}

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
	if v, err := parseInt(raw.RecentDays, 0); err == nil {
		r.RecentDays = v
	}
	r.TypeId = normalizeStringId(raw.TypeId)
	r.Year = strings.TrimSpace(raw.Year)
	r.Area = strings.TrimSpace(raw.Area)
	r.Keyword = strings.TrimSpace(raw.Keyword)
	r.Sort = strings.TrimSpace(raw.Sort)
	r.Cursor = strings.TrimSpace(raw.Cursor)
	if v, err := parseInt(raw.Page, 1); err == nil {
		r.Page = v
	}
	if v, err := parseInt(raw.PageSize, 20); err == nil {
		r.PageSize = v
	}
	return nil
}

// normalizeStringId 将 json.RawMessage 规范化为字符串（支持字符串/数字/空）
func normalizeStringId(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// 带引号的字符串
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	// 数字：直接转为字符串
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatInt(n, 10)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatInt(int64(f), 10)
	}
	return ""
}

type VideoListResp struct {
	Videos     []*model.Video `json:"videos"`
	Total      int            `json:"total"`
	NextCursor string         `json:"next_cursor"`
	// LocalVideos carries catalog-only matches the upstream search missed,
	// e.g. "是，大臣" for keyword "大臣" when the source API matches prefixes.
	LocalVideos []*model.Video `json:"local_videos,omitempty"`
}

func GetVideoList(req VideoListReq) (*VideoListResp, error) {
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
	page, err := db.GetCatalogVideoPage(req.SourceKey, filter)
	if err != nil {
		return nil, fmt.Errorf("get videos: %w", err)
	}

	return &VideoListResp{Videos: page.Videos, Total: page.Total, NextCursor: page.NextCursor}, nil
}

// YearAreaResp 前端用于前端：返回所有可选年份 / 地区列表（用于筛选下拉框的选项）
type YearsResp struct {
	Years []string `json:"years"`
	Areas []string `json:"areas"`
}

func GetYearsAndAreas(sourceKey string) (*YearsResp, error) {
	years, areas, err := db.GetCatalogYearsAndAreas(sourceKey)
	if err != nil {
		return nil, err
	}
	return &YearsResp{Years: years, Areas: areas}, nil
}

// GetRecommend 返回 N 条推荐视频（排除指定 id 集合中的视频）
func GetRecommend(sourceKey string, limit int, excludeIds []string) ([]*model.Video, error) {
	return db.GetCatalogRecommend(sourceKey, limit, excludeIds)
}

// GetSimilarVideos 返回同类型的相似视频（用于详情页推荐兜底）
func GetSimilarVideos(sourceKey string, typeId string, limit int, excludeIds []string) ([]*model.Video, error) {
	videos, _, err := db.GetCatalogVideos(sourceKey, db.FilterParams{TypeId: typeId, Page: 1, PageSize: limit})
	if err != nil {
		return nil, err
	}
	if len(excludeIds) == 0 {
		return videos, nil
	}
	excluded := map[string]bool{}
	for _, id := range excludeIds {
		excluded[id] = true
	}
	out := videos[:0]
	for _, v := range videos {
		if !excluded[v.VodId.String()] {
			out = append(out, v)
		}
	}
	return out, nil
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

// HydrateHistory hydrates raw history entries with additional video info from global_video
func HydrateHistory(sourceKey string, raws []db.HistEntry) []*HistoryItemWithVideo {
	if len(raws) == 0 {
		return []*HistoryItemWithVideo{}
	}
	out := make([]*HistoryItemWithVideo, 0, len(raws))
	for _, r := range raws {
		item := &HistoryItemWithVideo{
			GlobalID:  r.GlobalID,
			SourceKey: r.SourceKey,
			VodId:     r.VodId,
			EpNum:     r.EpNum,
			Position:  r.Position,
			UpdatedAt: r.UpdatedAt,
			VodName:   r.VodName,
			VodPic:    r.VodPic,
		}
		// Get additional info from source video table if available
		if v, err := db.GetVideoById(r.SourceKey, r.VodId); err == nil && v != nil {
			if item.VodName == "" {
				item.VodName = v.VodName
			}
			if item.VodPic == "" {
				item.VodPic = v.VodPic
			}
			item.VodRemarks = v.VodRemarks
		}
		out = append(out, item)
	}
	return out
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
	if v, err := parseInt64(raw.GlobalID, 0); err == nil {
		r.GlobalID = v
	}
	r.VodId = normalizeStringId(raw.VodId)
	r.Refresh = raw.Refresh
	return nil
}

type VideoDetailResp struct {
	Video    *model.Video     `json:"video"`
	Episodes []*model.Episode `json:"episodes"`
	Error    *detail.Error    `json:"error,omitempty"`
}

// GetVideoDetail always resolves a catalog identity and fetches remote detail.
// It intentionally has no SQLite detail write path.
func GetVideoDetail(req VideoDetailReq) (*VideoDetailResp, error) {
	if req.SourceKey == "" || (req.GlobalID <= 0 && req.VodId == "") {
		return nil, fmt.Errorf("source_key and global_id are required")
	}
	var result *detail.Result
	var err error
	if req.GlobalID > 0 {
		result, err = detail.Default.GetByGlobal(req.SourceKey, req.GlobalID, req.Refresh)
	} else {
		result, err = detail.Default.Get(req.SourceKey, req.VodId, req.Refresh)
	}
	if err != nil {
		if structured, ok := err.(*detail.Error); ok {
			return &VideoDetailResp{Video: structured.Fallback, Error: structured}, nil
		}
		return nil, err
	}
	return &VideoDetailResp{Video: result.Video, Episodes: result.Episodes}, nil
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
	if v, err := parseInt(raw.Page, 1); err == nil {
		r.Page = v
	}
	if v, err := parseInt(raw.PageSize, 20); err == nil {
		r.PageSize = v
	}
	return nil
}

// SearchVideos runs a remote keyword search and caches every hit into the
// source catalog as a side effect, so the Home/Detail views can list them
// without another request. Whether a cached hit resurrects a row the user
// deleted is decided by the catalog_revive_deleted setting.
//
// SearchSource (源搜索) deliberately does NOT cache: it is a preview across
// many sources, so its results only enter the catalog when the user presses
// 入库, which goes through ImportSourceVideos and always resurrects.
func SearchVideos(req VideoSearchReq) (*VideoListResp, error) {
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
	source, err := db.GetSourceByKey(req.SourceKey)
	if err != nil {
		return nil, fmt.Errorf("get source: %w", err)
	}
	strategy := collect.CreateStrategyFromSource(source)
	if strategy == nil {
		return nil, fmt.Errorf("source strategy unavailable")
	}
	fetched, err := collect.FetchSearchPage(strategy, keyword, req.Page)
	if err != nil {
		return nil, fmt.Errorf("remote search: %w", err)
	}
	fetched.List = db.FilterEnabledCollectVideos(fetched.List)

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
		return nil, fmt.Errorf("check catalog membership: %w", err)
	}
	for _, v := range fetched.List {
		if v != nil {
			v.InCatalog = cached[strings.TrimSpace(v.VodId.String())]
		}
	}

	if err := db.UpsertCatalogItems(req.SourceKey, fetched.List); err != nil {
		return nil, fmt.Errorf("cache search catalog: %w", err)
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
		resp.LocalVideos = searchLocalCatalog(req.SourceKey, keyword, remoteIDs)
	}
	return resp, nil
}

const localSearchLimit = 24

// searchLocalCatalog finds catalog rows whose title contains the keyword, which
// covers substring matches the upstream search API does not make. Rows already
// present in the remote result set are dropped to avoid duplicate cards.
func searchLocalCatalog(sourceKey, keyword string, exclude []string) []*model.Video {
	matches, err := db.SearchCatalogByTitle(sourceKey, keyword, localSearchLimit+len(exclude))
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

func GetTypes(req GetTypesReq) ([]*model.VType, error) {
	return db.GetTypes(req.SourceKey)
}

type DeleteVideoReq struct {
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
}

func DeleteVideo(req DeleteVideoReq) error {
	if req.SourceKey == "" || req.VodId == "" {
		return fmt.Errorf("参数不完整")
	}
	return db.DeleteCatalogVideo(req.SourceKey, req.VodId)
}
