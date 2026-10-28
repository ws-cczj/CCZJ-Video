package service

import (
	"bytes"
	"cczjVideo/app/db"
	"cczjVideo/app/media"
	"cczjVideo/app/model"
	"cczjVideo/app/payload"
	"context"
	"encoding/base64"

	"github.com/andybalholm/brotli"
)

// ======================== Video ========================
func (a *App) GetShortDramaPage(ctx context.Context, req media.ShortDramaPageReq) (*media.ShortDramaPage, error) {
	return runRequest(a, ctx, "video.GetShortDramaPage", func(ctx context.Context) (*media.ShortDramaPage, error) { return a.media.ShortDramaPage(ctx, req) })
}

func (a *App) GetVideoList(ctx context.Context, req media.VideoListReq) (*media.VideoListResp, error) {
	return runRequest(a, ctx, "video.GetVideoList", func(ctx context.Context) (*media.VideoListResp, error) { return a.media.List(ctx, req) })
}

func (a *App) GetVideoDetail(ctx context.Context, req media.VideoDetailReq) (*media.VideoDetailResp, error) {
	return runRequest(a, ctx, "video.detail", func(ctx context.Context) (*media.VideoDetailResp, error) {
		return a.media.Detail(ctx, req)
	})
}

// ProbePlayLines reads declared playlist quality without downloading segments.
func (a *App) ProbePlayLines(ctx context.Context, req media.PlayLineSpeedReq) ([]*media.PlayLineQualityItem, error) {
	return runRequest(a, ctx, "video.ProbePlayLines", func(ctx context.Context) ([]*media.PlayLineQualityItem, error) {
		return a.media.ProbePlayLines(ctx, req)
	})
}

// SpeedTestPlayLines retains the explicit speed measurement binding for older clients.
func (a *App) SpeedTestPlayLines(ctx context.Context, req media.PlayLineSpeedReq) (*media.PlayLineSpeedResp, error) {
	return runRequest(a, ctx, "video.line_probe", func(ctx context.Context) (*media.PlayLineSpeedResp, error) {
		return a.media.SpeedTestPlayLines(ctx, req)
	})
}

// CompressDetailJSONBrotli and DecompressDetailJSONBrotli are small transport
// helpers for the browser-side detail cache. The cache lives in localStorage,
// while Brotli stays in Go so it also works in WebView2 versions that do not
// expose CompressionStream('br').
func (a *App) CompressDetailJSONBrotli(value string) (string, error) {
	var buf bytes.Buffer
	writer := brotli.NewWriterLevel(&buf, 5)
	if _, err := writer.Write([]byte(value)); err != nil {
		_ = writer.Close()
		return "", err
	}
	if err := writer.Close(); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (a *App) DecompressDetailJSONBrotli(encoded string) (string, error) {
	ctx := context.Background()
	compressed, err := payload.Base64(ctx, encoded, 16<<20)
	if err != nil {
		return "", err
	}
	decoded, err := payload.UnpackBytes(ctx, "detail.br", compressed, payload.Limits{Input: 16 << 20, Output: 16 << 20})
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (a *App) SearchVideos(ctx context.Context, req media.VideoSearchReq) (*media.VideoListResp, error) {
	return runRequest(a, ctx, "video.SearchVideos", func(ctx context.Context) (*media.VideoListResp, error) {
		return a.media.Search(ctx, req)
	})
}

// GetGlobalIdForVideo 获取指定源中某个视频的 global_id
func (a *App) GetGlobalIdForVideo(ctx context.Context, sourceKey string, vodId string) (int64, error) {
	return runRequest(a, ctx, "video.GetGlobalIdForVideo", func(ctx context.Context) (int64, error) { return a.media.Identity(ctx, sourceKey, vodId) })
}

// FindSourcesByGlobalId 通过 global_id 查找所有拥有该视频的源
func (a *App) FindSourcesByGlobalId(ctx context.Context, globalId int64) ([]db.SourceVideoRef, error) {
	return runRequest(a, ctx, "video.FindSourcesByGlobalId", func(ctx context.Context) ([]db.SourceVideoRef, error) {
		return a.media.SourcesByIdentity(ctx, globalId)
	})
}

func (a *App) GetTypes(ctx context.Context, req media.GetTypesReq) ([]*model.VType, error) {
	return runRequest(a, ctx, "video.GetTypes", func(ctx context.Context) ([]*model.VType, error) { return a.media.Types(ctx, req) })
}

// DeleteVideo 把指定源里的一条视频移入回收站（软删除）。收藏和历史不删，
// 只是跟着目录行一起隐藏，恢复后原样回来。
func (a *App) DeleteVideo(ctx context.Context, req media.DeleteVideoReq) error {
	return runRequestError(a, ctx, "video.DeleteVideo", func(ctx context.Context) error { return a.media.Delete(ctx, req) })
}

// GetRecycleBin 列出回收站（软删除）的目录条目，source_key 留空表示跨源。
func (a *App) GetRecycleBin(ctx context.Context, req media.RecycleListReq) (*media.RecycleListResp, error) {
	return runRequest(a, ctx, "video.GetRecycleBin", func(ctx context.Context) (*media.RecycleListResp, error) { return a.media.ListRecycleBin(ctx, req) })
}

// RestoreVideo 把一条回收站条目放回视频库。
func (a *App) RestoreVideo(ctx context.Context, req media.RecycleReq) error {
	return runRequestError(a, ctx, "video.RestoreVideo", func(ctx context.Context) error { return a.media.RestoreVideo(ctx, req) })
}

// PurgeVideo 彻底删除一条回收站条目。
func (a *App) PurgeVideo(ctx context.Context, req media.RecycleReq) (*media.RecycleResult, error) {
	return runRequest(a, ctx, "video.PurgeVideo", func(ctx context.Context) (*media.RecycleResult, error) { return a.media.PurgeVideo(ctx, req) })
}

// ClearRecycleBin 清空回收站。
func (a *App) ClearRecycleBin(ctx context.Context, req media.RecycleListReq) (*media.RecycleResult, error) {
	return runRequest(a, ctx, "video.ClearRecycleBin", func(ctx context.Context) (*media.RecycleResult, error) { return a.media.ClearRecycleBin(ctx, req) })
}

// GetYearsAndAreas 返回当前源下所有可选的年份/地区，供前端筛选下拉框使用
func (a *App) GetYearsAndAreas(ctx context.Context, sourceKey string) (*media.YearsResp, error) {
	return runRequest(a, ctx, "video.GetYearsAndAreas", func(ctx context.Context) (*media.YearsResp, error) { return a.media.YearsAndAreas(ctx, sourceKey) })
}

// ListIdentityMergeCandidates 列出疑似重复的身份组，供设置页诊断分组人工确认。
// 这些组不会自动合并：剩下的都是"标题差个年份/清晰度"或"同名挂在不同类型下"的情况，
// 判断错了要搬走收藏和观看进度，所以只列出来等人点。
func (a *App) ListIdentityMergeCandidates(ctx context.Context, limit int) (*media.MergeCandidatesResp, error) {
	return runRequest(a, ctx, "video.ListIdentityMergeCandidates", func(ctx context.Context) (*media.MergeCandidatesResp, error) {
		return a.media.ListMergeCandidates(ctx, limit)
	})
}

// MergeGlobalVideoIdentities 合并用户确认的一组身份，返回存活 id 和被并掉的条数。
func (a *App) MergeGlobalVideoIdentities(ctx context.Context, req media.MergeIdentitiesReq) (*media.MergeIdentitiesResp, error) {
	return runRequest(a, ctx, "video.MergeGlobalVideoIdentities", func(ctx context.Context) (*media.MergeIdentitiesResp, error) {
		return a.media.MergeIdentities(ctx, req)
	})
}

// GetRecommend 返回 N 条推荐视频（会排除 excludeIds 中的 vod_id，避免"猜你喜欢"和"继续观看"重复）
type RecommendReq struct {
	SourceKey  string   `json:"source_key"`
	Limit      int      `json:"limit"`
	ExcludeIds []string `json:"exclude_ids"`
	ShortDrama bool     `json:"short_drama,omitempty"`
}

func (a *App) GetRecommend(ctx context.Context, req RecommendReq) ([]*model.Video, error) {
	return runRequest(a, ctx, "video.GetRecommend", func(ctx context.Context) ([]*model.Video, error) {
		if req.ShortDrama {
			return a.media.ShortDramaRecommendations(ctx, req.SourceKey, req.ExcludeIds)
		}
		return a.media.Recommend(ctx, req.SourceKey, req.Limit, req.ExcludeIds)
	})
}

// GetSimilarVideos 返回同类型的相似视频（用于详情页推荐兜底）
type SimilarReq struct {
	SourceKey  string   `json:"source_key"`
	TypeId     string   `json:"type_id"`
	Limit      int      `json:"limit"`
	ExcludeIds []string `json:"exclude_ids"`
}

func (a *App) GetSimilarVideos(ctx context.Context, req SimilarReq) ([]*model.Video, error) {
	return runRequest(a, ctx, "video.GetSimilarVideos", func(ctx context.Context) ([]*model.Video, error) {
		return a.media.Similar(ctx, req.SourceKey, req.TypeId, req.Limit, req.ExcludeIds)
	})
}
