// Identity maintenance is explicit: candidate discovery never merges data.
// A confirmed merge commits references and then invalidates affected sources.
package media

import (
	"cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/payload"
	"context"
	"encoding/json"
)

// MergeCandidatesResp 是待人工确认的疑似重复身份组。
type MergeCandidatesResp struct {
	Groups []db.MergeCandidate `json:"groups"`
}

// ListMergeCandidates 列出候选组。limit 是扫描的身份数上限，前端只用来显示"还有更多"。
func (s *Service) ListMergeCandidates(ctx context.Context, limit int) (*MergeCandidatesResp, error) {
	groups, err := s.store.ListIdentityMergeCandidates(ctx, limit)
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
		id, err := payload.ScalarInt64(item, 0)
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
func (s *Service) MergeIdentities(ctx context.Context, req MergeIdentitiesReq) (*MergeIdentitiesResp, error) {
	change, err := s.store.MergeIdentities(ctx, req.GlobalIDs)
	if err != nil {
		return nil, err
	}
	cache.PublishCatalogChange(change.CatalogChange, "重复身份已合并")
	return &MergeIdentitiesResp{KeepID: change.KeepID, Merged: change.Merged}, nil
}

func (s *Service) Identity(ctx context.Context, source, vod string) (int64, error) {
	return s.catalogGlobalID(ctx, source, vod)
}
func (s *Service) SourcesByIdentity(ctx context.Context, id int64) ([]db.SourceVideoRef, error) {
	return db.FindSourcesByGlobalIdContext(ctx, id)
}
