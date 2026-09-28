// Package handler 跨源合并视图与人工确认的身份合并入口。
//
// 单源列表把「一行目录」当成「一部片」，所以同一部片接了两个源就会出现两张互不可见的卡片。
// 这一层给出以 global_id 聚合的曲库读路径，以及一个只由用户点单条执行的合并队列：
// 自动合并只处理迁移 v2 那种标题与类型完全一致的情况，剩下的疑似重复必须人看过再动。
package handler

import (
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"encoding/json"
	"fmt"
	"strings"
)

// UnionListReq 是跨源合并列表的筛选条件。没有 source_key 是有意为之：
// 这个视图的意义就是跨源，想只看一个源请回到视频库。
type UnionListReq struct {
	RecentDays int    `json:"recent_days"`
	TypeId     string `json:"type_id"`
	Year       string `json:"year"`
	Area       string `json:"area"`
	Keyword    string `json:"keyword"`
	Cursor     string `json:"cursor"`
	PageSize   int    `json:"page_size"`
}

func (r *UnionListReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		RecentDays json.RawMessage `json:"recent_days"`
		TypeId     json.RawMessage `json:"type_id"`
		Year       string          `json:"year"`
		Area       string          `json:"area"`
		Keyword    string          `json:"keyword"`
		Cursor     string          `json:"cursor"`
		PageSize   json.RawMessage `json:"page_size"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, err := parseInt(raw.RecentDays, 0); err == nil {
		r.RecentDays = v
	}
	// 这里的 type_id 是 global_types.id，合并列表按全局类型筛选。
	r.TypeId = normalizeStringId(raw.TypeId)
	r.Year = strings.TrimSpace(raw.Year)
	r.Area = strings.TrimSpace(raw.Area)
	r.Keyword = strings.TrimSpace(raw.Keyword)
	r.Cursor = strings.TrimSpace(raw.Cursor)
	if v, err := parseInt(raw.PageSize, 24); err == nil {
		r.PageSize = v
	}
	return nil
}

// UnionListResp 是一页合并卡片。Total 数的是身份数而不是目录行数，
// 所以它会比同筛选下的单源列表小——这正是"合并"要达到的效果。
type UnionListResp struct {
	Videos     []*db.UnionVideo `json:"videos"`
	Total      int              `json:"total"`
	NextCursor string           `json:"next_cursor"`
}

func GetUnionVideoList(req UnionListReq) (*UnionListResp, error) {
	if req.PageSize <= 0 {
		req.PageSize = 24
	}
	page, err := db.GetCatalogUnionPage(db.FilterParams{
		TypeId:     req.TypeId,
		Year:       req.Year,
		Area:       req.Area,
		Keyword:    req.Keyword,
		RecentDays: req.RecentDays,
		Cursor:     req.Cursor,
		PageSize:   req.PageSize,
	})
	if err != nil {
		return nil, fmt.Errorf("get merged library: %w", err)
	}
	return &UnionListResp{Videos: page.Videos, Total: page.Total, NextCursor: page.NextCursor}, nil
}

// GetUnionYearsAndAreas 返回跨源的年份/地区选项，供合并列表的筛选下拉框使用。
func GetUnionYearsAndAreas() (*YearsResp, error) {
	years, areas, err := db.GetUnionYearsAndAreas()
	if err != nil {
		return nil, err
	}
	return &YearsResp{Years: years, Areas: areas}, nil
}

// MergeCandidatesResp 是待人工确认的疑似重复身份组。
type MergeCandidatesResp struct {
	Groups []db.MergeCandidate `json:"groups"`
}

// ListMergeCandidates 列出候选组。limit 是扫描的身份数上限，前端只用来显示"还有更多"。
func ListMergeCandidates(limit int) (*MergeCandidatesResp, error) {
	groups, err := db.ListIdentityMergeCandidates(limit)
	if err != nil {
		return nil, err
	}
	return &MergeCandidatesResp{Groups: groups}, nil
}

// MergeIdentitiesReq 指定一组要合并的 global_id。一次一组，不做批量勾选：
// 合并会搬走收藏和观看进度，接受一次点错的代价太高。
type MergeIdentitiesReq struct {
	GlobalIDs []int64 `json:"global_ids"`
}

func (r *MergeIdentitiesReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		GlobalIDs []json.RawMessage `json:"global_ids"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	ids := make([]int64, 0, len(raw.GlobalIDs))
	for _, item := range raw.GlobalIDs {
		id, err := parseInt64(item, 0)
		if err != nil {
			return err
		}
		ids = append(ids, id)
	}
	r.GlobalIDs = ids
	return nil
}

// MergeIdentitiesResp 报告存活身份与被并掉的条数。
type MergeIdentitiesResp struct {
	KeepID int64 `json:"keep_id"`
	Merged int   `json:"merged"`
}

// MergeIdentities 合并用户确认的一组身份。
func MergeIdentities(req MergeIdentitiesReq) (*MergeIdentitiesResp, error) {
	keep, merged, err := cache.MergeGlobalVideoIdentities(req.GlobalIDs, "重复身份已合并")
	if err != nil {
		return nil, err
	}
	return &MergeIdentitiesResp{KeepID: keep, Merged: merged}, nil
}
