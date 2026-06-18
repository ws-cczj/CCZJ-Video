package handler

import (
	"cczjVideo/app/db"
	"cczjVideo/app/model"
	"cczjVideo/app/util"
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
	SourceKey string `json:"source_key"`
	// type_id 同时支持字符串与数字，保存为字符串
	TypeId   string `json:"type_id"`
	Year     string `json:"year"`     // 年份筛选（"all" 或具体年份）
	Area     string `json:"area"`     // 地区筛选（"all" 或具体地区）
	Keyword  string `json:"keyword"`  // 关键词：标题/演员/导演/备注/年份/地区/类型 模糊匹配
	Sort     string `json:"sort"`     // "" 默认; "rating" 按评分; "hot" 按热度
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func (r *VideoListReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		TypeId    json.RawMessage `json:"type_id"`
		Year      string          `json:"year"`
		Area      string          `json:"area"`
		Keyword   string          `json:"keyword"`
		Sort      string          `json:"sort"`
		Page      json.RawMessage `json:"page"`
		PageSize  json.RawMessage `json:"page_size"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.TypeId = normalizeStringId(raw.TypeId)
	r.Year = strings.TrimSpace(raw.Year)
	r.Area = strings.TrimSpace(raw.Area)
	r.Keyword = strings.TrimSpace(raw.Keyword)
	r.Sort = strings.TrimSpace(raw.Sort)
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
	Videos []*model.Video `json:"videos"`
	Total  int            `json:"total"`
}

func GetVideoList(req VideoListReq) (*VideoListResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	filter := db.FilterParams{
		TypeId:   req.TypeId,
		Year:     req.Year,
		Area:     req.Area,
		Keyword:  req.Keyword,
		Sort:     req.Sort,
		Page:     req.Page,
		PageSize: req.PageSize,
	}
	videos, total, err := db.GetVideos(req.SourceKey, filter)
	if err != nil {
		return nil, fmt.Errorf("get videos: %w", err)
	}

	for _, v := range videos {
		v.VodActor = util.DecompressIfNeeded(v.VodActor)
		v.VodDirector = util.DecompressIfNeeded(v.VodDirector)
		v.VodContent = util.DecompressIfNeeded(v.VodContent)
	}
	// 用全局豆瓣数据批量补充缺失字段（一次 JOIN 替代 N×2 次查询）
	db.EnrichVideosWithDouban(videos)

	return &VideoListResp{Videos: videos, Total: total}, nil
}

// YearAreaResp 前端用于前端：返回所有可选年份 / 地区列表（用于筛选下拉框的选项）
type YearsResp struct {
	Years []string `json:"years"`
	Areas []string `json:"areas"`
}

func GetYearsAndAreas(sourceKey string) (*YearsResp, error) {
	years, _ := db.GetDistinctYears(sourceKey)
	areas, _ := db.GetDistinctAreas(sourceKey)
	return &YearsResp{Years: years, Areas: areas}, nil
}

// GetRecommend 返回 N 条推荐视频（排除指定 id 集合中的视频）
func GetRecommend(sourceKey string, limit int, excludeIds []string) ([]*model.Video, error) {
	videos, err := db.GetRandomRecommend(sourceKey, limit, excludeIds)
	if err != nil {
		return nil, err
	}
	for _, v := range videos {
		v.VodActor = util.DecompressIfNeeded(v.VodActor)
		v.VodDirector = util.DecompressIfNeeded(v.VodDirector)
	}
	// 用全局豆瓣数据批量补充缺失字段（一次 JOIN 替代 N×2 次查询）
	db.EnrichVideosWithDouban(videos)
	return videos, nil
}

// HistoryItemWithVideo 前端可用的"继续观看"条目：含视频名+封面，便于卡片展示
type HistoryItemWithVideo struct {
	SourceKey string  `json:"source_key"`
	VodId     string  `json:"vod_id"`
	EpNum     int     `json:"ep_num"`
	Position  float64 `json:"position"`
	UpdatedAt string  `json:"updated_at"`
	VodName   string  `json:"vod_name"`
	VodPic    string  `json:"vod_pic"`
	VodRemarks string `json:"vod_remarks"`
}

// HydrateHistory hydrates raw history entries with additional video info from global_video
func HydrateHistory(sourceKey string, raws []db.HistEntry) []*HistoryItemWithVideo {
	if len(raws) == 0 {
		return []*HistoryItemWithVideo{}
	}
	out := make([]*HistoryItemWithVideo, 0, len(raws))
	for _, r := range raws {
		item := &HistoryItemWithVideo{
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
	VodId     string `json:"vod_id"`
}

func (r *VideoDetailReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.VodId = normalizeStringId(raw.VodId)
	return nil
}

type VideoDetailResp struct {
	Video    *model.Video     `json:"video"`
	Episodes []*model.Episode `json:"episodes"`
}

func GetVideoDetail(req VideoDetailReq) (*VideoDetailResp, error) {
	video, err := db.GetVideoById(req.SourceKey, req.VodId)
	if err != nil {
		return nil, fmt.Errorf("video not found: %w", err)
	}

	video.VodContent = util.DecompressIfNeeded(video.VodContent)
	video.VodActor = util.DecompressIfNeeded(video.VodActor)
	video.VodDirector = util.DecompressIfNeeded(video.VodDirector)
	video.VodPlayUrl = util.DecompressIfNeeded(video.VodPlayUrl)
	video.VodDownUrl = util.DecompressIfNeeded(video.VodDownUrl)

	// 用全局豆瓣数据补充缺失字段
	db.EnrichVideoWithDouban(video)

	episodes, _ := db.GetEpisodes(req.SourceKey, req.VodId)

	return &VideoDetailResp{Video: video, Episodes: episodes}, nil
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

func SearchVideos(req VideoSearchReq) (*VideoListResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}

	videos, total, err := db.SearchVideos(req.SourceKey, req.Keyword, req.Page, req.PageSize)
	if err != nil {
		return nil, fmt.Errorf("search videos: %w", err)
	}

	for _, v := range videos {
		v.VodActor = util.DecompressIfNeeded(v.VodActor)
		v.VodDirector = util.DecompressIfNeeded(v.VodDirector)
		v.VodContent = util.DecompressIfNeeded(v.VodContent)
	}
	// 用全局豆瓣数据批量补充缺失字段（一次 JOIN 替代 N×2 次查询）
	db.EnrichVideosWithDouban(videos)
	return &VideoListResp{Videos: videos, Total: total}, nil
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
	return db.DeleteVideo(req.SourceKey, req.VodId)
}
