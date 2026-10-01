package service

import (
	"bytes"
	"cczjVideo/app/db"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"encoding/base64"
	"io"

	"github.com/andybalholm/brotli"
)

// ======================== Video ========================

func (a *App) GetVideoList(req handler.VideoListReq) (*handler.VideoListResp, error) {
	return a.media.List(req)
}

func (a *App) GetVideoDetail(req handler.VideoDetailReq) (*handler.VideoDetailResp, error) {
	return a.media.Detail(a.background.Context(), req)
}

// SpeedTestPlayLines 并发测量一个视频各条播放线路的速度，返回按快慢排好的结果。
func (a *App) SpeedTestPlayLines(req handler.PlayLineSpeedReq) (*handler.PlayLineSpeedResp, error) {
	return a.media.SpeedTestPlayLines(a.background.Context(), req)
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
	compressed, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	reader := brotli.NewReader(bytes.NewReader(compressed))
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func (a *App) SearchVideos(req handler.VideoSearchReq) (*handler.VideoListResp, error) {
	return a.media.Search(req)
}

// GetGlobalIdForVideo 获取指定源中某个视频的 global_id
func (a *App) GetGlobalIdForVideo(sourceKey string, vodId string) (int64, error) {
	return a.media.GlobalID(sourceKey, vodId)
}

// FindSourcesByGlobalId 通过 global_id 查找所有拥有该视频的源
func (a *App) FindSourcesByGlobalId(globalId int64) ([]db.SourceVideoRef, error) {
	return a.media.SourcesByGlobalID(globalId)
}

func (a *App) GetTypes(req handler.GetTypesReq) ([]*model.VType, error) {
	return a.media.Types(req)
}

// DeleteVideo 把指定源里的一条视频移入回收站（软删除）。收藏和历史不删，
// 只是跟着目录行一起隐藏，恢复后原样回来。
func (a *App) DeleteVideo(req handler.DeleteVideoReq) error {
	return a.media.Delete(req)
}

// GetRecycleBin 列出回收站（软删除）的目录条目，source_key 留空表示跨源。
func (a *App) GetRecycleBin(req handler.RecycleListReq) (*handler.RecycleListResp, error) {
	return a.media.RecycleBin(req)
}

// RestoreVideo 把一条回收站条目放回视频库。
func (a *App) RestoreVideo(req handler.RecycleReq) error {
	return a.media.Restore(req)
}

// PurgeVideo 彻底删除一条回收站条目。
func (a *App) PurgeVideo(req handler.RecycleReq) (*handler.RecycleResult, error) {
	return a.media.Purge(req)
}

// ClearRecycleBin 清空回收站。
func (a *App) ClearRecycleBin(req handler.RecycleListReq) (*handler.RecycleResult, error) {
	return a.media.ClearRecycleBin(req)
}

// GetYearsAndAreas 返回当前源下所有可选的年份/地区，供前端筛选下拉框使用
func (a *App) GetYearsAndAreas(sourceKey string) (*handler.YearsResp, error) {
	return a.media.YearsAndAreas(sourceKey)
}

// ListIdentityMergeCandidates 列出疑似重复的身份组，供设置页诊断分组人工确认。
// 这些组不会自动合并：剩下的都是"标题差个年份/清晰度"或"同名挂在不同类型下"的情况，
// 判断错了要搬走收藏和观看进度，所以只列出来等人点。
func (a *App) ListIdentityMergeCandidates(limit int) (*handler.MergeCandidatesResp, error) {
	return a.media.MergeCandidates(limit)
}

// MergeGlobalVideoIdentities 合并用户确认的一组身份，返回存活 id 和被并掉的条数。
func (a *App) MergeGlobalVideoIdentities(req handler.MergeIdentitiesReq) (*handler.MergeIdentitiesResp, error) {
	return a.media.MergeIdentities(req)
}

// GetRecommend 返回 N 条推荐视频（会排除 excludeIds 中的 vod_id，避免"猜你喜欢"和"继续观看"重复）
type RecommendReq struct {
	SourceKey  string   `json:"source_key"`
	Limit      int      `json:"limit"`
	ExcludeIds []string `json:"exclude_ids"`
}

func (a *App) GetRecommend(req RecommendReq) ([]*model.Video, error) {
	return a.media.Recommend(req.SourceKey, req.Limit, req.ExcludeIds)
}

// GetSimilarVideos 返回同类型的相似视频（用于详情页推荐兜底）
type SimilarReq struct {
	SourceKey  string   `json:"source_key"`
	TypeId     string   `json:"type_id"`
	Limit      int      `json:"limit"`
	ExcludeIds []string `json:"exclude_ids"`
}

func (a *App) GetSimilarVideos(req SimilarReq) ([]*model.Video, error) {
	return a.media.Similar(req.SourceKey, req.TypeId, req.Limit, req.ExcludeIds)
}
