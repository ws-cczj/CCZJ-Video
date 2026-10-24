package media

import (
	"cczjVideo/app/apperror"
	"context"
	"testing"
)

func TestRecycleDomainValidateAndNormalize(t *testing.T) {
	if err := (RecycleReq{}).validate(); err == nil || apperror.CodeOf(err) != apperror.Validation {
		t.Errorf("空参数 validate = %v, want Validation", err)
	}
	if err := (RecycleReq{SourceKey: "s"}).validate(); err == nil {
		t.Errorf("缺 vod_id 竟然通过校验")
	}
	if err := (RecycleReq{SourceKey: "s", VodId: "v"}).validate(); err != nil {
		t.Errorf("完整参数被拒: %v", err)
	}

	if err := newTestMedia().RestoreVideo(context.Background(), RecycleReq{VodId: "v"}); apperror.CodeOf(err) != apperror.Validation {
		t.Errorf("RestoreVideo 缺参 = %v, want Validation", err)
	}
	if _, err := newTestMedia().PurgeVideo(context.Background(), RecycleReq{SourceKey: "s"}); apperror.CodeOf(err) != apperror.Validation {
		t.Errorf("PurgeVideo 缺参 = %v, want Validation", err)
	}

	// 一条都不存在的源：items 必须是空数组而不是 null，前端 v-for 直接吃到 null 会炸。
	resp, err := newTestMedia().ListRecycleBin(context.Background(), RecycleListReq{SourceKey: "recycle_handler_unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Items == nil {
		t.Errorf("items is nil, want 空切片")
	}
	if resp.Page != 1 || resp.PageSize != 50 {
		t.Errorf("分页归一 = page %d size %d, want 1/50", resp.Page, resp.PageSize)
	}
	if resp.Total != 0 {
		t.Errorf("total = %d, want 0", resp.Total)
	}

	deleted, err := newTestMedia().ClearRecycleBin(context.Background(), RecycleListReq{SourceKey: "recycle_handler_unknown"})
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Affected != 0 {
		t.Errorf("affected = %d, want 0", deleted.Affected)
	}
}
