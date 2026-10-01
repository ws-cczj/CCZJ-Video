// Package handler 人工确认的身份合并入口。
//
// 单源列表把「一行目录」当成「一部片」，所以同一部片接了两个源就会出现两张互不可见的卡片。
// 自动合并只处理迁移 v2 那种标题与类型完全一致的情况，剩下的疑似重复必须人看过再动：
// 这里只列候选组并执行用户点单的那一条，合并会搬走收藏和观看进度，不接受批量勾选。
// 跨源查同一部片在哪些源里有货请走 FindSourcesByGlobalId（播放页在用）。
package handler

import (
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"encoding/json"
)

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
