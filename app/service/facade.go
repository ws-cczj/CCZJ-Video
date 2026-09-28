package service

import (
	"bytes"
	"cczjVideo/app/applog"
	backupservice "cczjVideo/app/backup"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	fileservice "cczjVideo/app/files"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"cczjVideo/app/proxy"
	sourceservice "cczjVideo/app/source"
	"compress/gzip"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"

	"github.com/andybalholm/brotli"
)

// ======================== Video ========================

func (a *App) GetVideoList(req handler.VideoListReq) (*handler.VideoListResp, error) {
	return a.media.List(req)
}

func (a *App) GetVideoDetail(req handler.VideoDetailReq) (*handler.VideoDetailResp, error) {
	return a.media.Detail(req)
}

// SpeedTestPlayLines 并发测量一个视频各条播放线路的速度，返回按快慢排好的结果。
func (a *App) SpeedTestPlayLines(req handler.PlayLineSpeedReq) (*handler.PlayLineSpeedResp, error) {
	return a.media.SpeedTestPlayLines(req)
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

// GetMergedLibraryList 返回跨源合并后的曲库一页：一张卡片对应一个 global_id，
// 卡片上带这部片在哪些源里有货。
func (a *App) GetMergedLibraryList(req handler.UnionListReq) (*handler.UnionListResp, error) {
	return a.media.UnionList(req)
}

// GetMergedLibraryYearsAndAreas 返回跨源汇总的年份/地区选项。
func (a *App) GetMergedLibraryYearsAndAreas() (*handler.YearsResp, error) {
	return a.media.UnionYearsAndAreas()
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

// ======================== Source ========================

func (a *App) GetAllSources() ([]*model.Source, error) {
	return handler.GetAllSources()
}

func (a *App) GetSourceStats() ([]model.SourceStat, error) {
	return handler.GetSourceStats()
}

func (a *App) AddSource(s *model.Source) error {
	return handler.AddSource(s)
}

func (a *App) UpdateSource(s *model.Source) error {
	return handler.UpdateSource(s)
}

func (a *App) DeleteSource(key string) error {
	return handler.DeleteSource(key)
}

// ======================== 数据源导入 / 导出（含 Brotli 压缩） ========================

// sourceExportPayload 导出的 JSON 结构（写入 Brotli 压缩文件）
type sourceExportPayload = sourceservice.Payload

// ExportSource 导出某个源的所有数据到 .json.br 文件（Brotli 压缩），返回绝对路径
// 前端拿到文件路径后可以在系统文件管理器中复制/传给他人
func (a *App) ExportSource(sourceKey string) (string, error) {
	return sourceservice.NewService().Export(a.getDataDir(), sourceKey)
}

func (a *App) ImportSource(filePath string) (string, error) {
	trimmed := strings.TrimSpace(filePath)
	if trimmed == "" {
		return "", fmt.Errorf("文件路径为空")
	}
	info, err := os.Stat(trimmed)
	if err != nil {
		return "", fmt.Errorf("无法访问文件: %w", err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("路径是目录，不是文件")
	}
	if info.Size() > maxSourceImportBytes {
		return "", fmt.Errorf("导入文件过大：最大允许 %d MiB", maxSourceImportBytes>>20)
	}

	f, err := os.Open(trimmed)
	if err != nil {
		return "", fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()

	// 根据文件扩展名检测压缩格式
	lower := strings.ToLower(trimmed)
	var reader io.Reader = f
	if strings.HasSuffix(lower, ".br") {
		reader = brotli.NewReader(f)
	} else if strings.HasSuffix(lower, ".gz") {
		gzr, err := gzip.NewReader(f)
		if err != nil {
			return "", fmt.Errorf("解压 gzip 失败: %w", err)
		}
		defer gzr.Close()
		reader = gzr
	} else {
		// 无扩展名或 .json：尝试检测 gzip magic bytes
		magic := make([]byte, 2)
		_, err = f.Read(magic)
		if err != nil {
			return "", fmt.Errorf("读取文件失败: %w", err)
		}
		_, _ = f.Seek(0, 0)
		if magic[0] == 0x1f && magic[1] == 0x8b {
			gzr, err := gzip.NewReader(f)
			if err != nil {
				return "", fmt.Errorf("解压 gzip 失败: %w", err)
			}
			defer gzr.Close()
			reader = gzr
		}
	}

	var payload sourceExportPayload
	dec := json.NewDecoder(io.LimitReader(reader, maxSourceImportBytes+1))
	if err := dec.Decode(&payload); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if payload.Source == nil {
		return "", fmt.Errorf("文件中缺少 source 信息")
	}
	return a.persistImportedSource(payload, "file")
}

// ImportSourceFromBase64 imports source data provided by the browser.
func (a *App) ImportSourceFromBase64(filename string, b64Content string) (string, error) {
	if strings.TrimSpace(b64Content) == "" {
		return "", fmt.Errorf("文件内容为空")
	}
	if len(b64Content) > maxSourceImportBase64Bytes {
		return "", fmt.Errorf("导入内容过大：最大允许 %d MiB", maxSourceImportBytes>>20)
	}
	raw, err := base64.StdEncoding.DecodeString(b64Content)
	if err != nil {
		return "", fmt.Errorf("解码 base64 失败: %w", err)
	}

	// 根据文件名扩展名检测压缩格式
	var reader io.Reader = bytes.NewReader(raw)
	lower := strings.ToLower(filename)
	if strings.HasSuffix(lower, ".br") {
		reader = brotli.NewReader(bytes.NewReader(raw))
	} else if strings.HasSuffix(lower, ".gz") {
		gzr, err := gzip.NewReader(bytes.NewReader(raw))
		if err != nil {
			return "", fmt.Errorf("解压 gzip 失败: %w", err)
		}
		defer gzr.Close()
		reader = gzr
	} else {
		// 无扩展名或 .json：尝试检测 gzip magic bytes
		if len(raw) >= 2 && raw[0] == 0x1f && raw[1] == 0x8b {
			gzr, err := gzip.NewReader(bytes.NewReader(raw))
			if err != nil {
				return "", fmt.Errorf("解压 gzip 失败: %w", err)
			}
			defer gzr.Close()
			reader = gzr
		}
	}

	var payload sourceExportPayload
	dec := json.NewDecoder(io.LimitReader(reader, maxSourceImportBytes+1))
	if err := dec.Decode(&payload); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if payload.Source == nil {
		return "", fmt.Errorf("文件中缺少 source 信息")
	}
	return a.persistImportedSource(payload, "base64")
}

func (a *App) persistImportedSource(payload sourceExportPayload, origin string) (string, error) {
	return sourceservice.NewService().Import(payload, origin)
}

// ======================== 全量备份：设置 / 收藏 / 历史 / 迁移归档恢复 ========================

// BackupExport writes settings, favorites, watch history and the metadata they
// point at into one Brotli-compressed JSON file. An empty destination puts the
// file under dataDir/exports and returns that path.
func (a *App) BackupExport(destination string) (string, error) {
	return backupservice.NewService().Export(a.getDataDir(), destination)
}

// BackupImportFromBase64 merges a backup chosen in the browser. Merge only ever
// adds or refreshes: it never deletes local rows.
func (a *App) BackupImportFromBase64(filename string, b64Content string) (backupservice.Result, error) {
	return backupservice.NewService().ImportBase64(filename, b64Content)
}

// BackupArchives lists the pre-migration snapshots the app keeps on disk.
func (a *App) BackupArchives() ([]backupservice.ArchiveInfo, error) {
	return backupservice.NewService().Archives(a.getDataDir())
}

// BackupRestoreArchive merges one snapshot by file name; the path is rebuilt
// under dataDir/schema-backups so only the archive directory is reachable.
func (a *App) BackupRestoreArchive(name string) (backupservice.Result, error) {
	return backupservice.NewService().RestoreArchive(a.getDataDir(), name)
}

// BackupExports lists the backup files already in dataDir/exports, newest first.
// The UI offers these as one-click imports because the WebView2 file dialogs are
// not dependable, and because that is exactly where BackupExport just wrote to.
func (a *App) BackupExports() ([]backupservice.ArchiveInfo, error) {
	return backupservice.NewService().Exports(a.getDataDir())
}

// BackupImportFile merges one file from dataDir/exports by name.
func (a *App) BackupImportFile(name string) (backupservice.Result, error) {
	return backupservice.NewService().ImportFile(a.getDataDir(), name)
}

func (a *App) OpenFolder(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("路径为空")
	}
	if !fileservice.IsWithin(path, a.getDataDir(), a.downloadDir.Get()) {
		return "", fmt.Errorf("path is outside application-managed directories")
	}
	// 如果传入的是文件，则打开其所在目录
	info, err := os.Stat(path)
	if err == nil && !info.IsDir() {
		path = filepath.Dir(path)
	}
	var cmd *exec.Cmd
	switch goruntime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", path)
	case "darwin":
		cmd = exec.Command("open", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("打开目录失败: %w", err)
	}
	return "已打开: " + path, nil
}

// safeFilename 把任意字符串变成安全的文件名
func safeFilename(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '_')
		}
	}
	if len(out) == 0 {
		return "unknown"
	}
	return string(out)
}

func logInfo(msg string) {
	applog.Info("%s", msg)
}

// ======================== 数据源详情 / 管理操作 ========================

// GetSourceDetail 返回某个 source_key 的字段定义和示例数据
func (a *App) GetSourceDetail(sourceKey string) (*handler.SourceDetail, error) {
	return handler.GetSourceDetail(sourceKey)
}

// SourceActionReq 前端传入的数据源操作请求
type SourceActionReq struct {
	SourceKey string `json:"source_key"`
	Action    string `json:"action"` // truncate / recreate / delete_source
	VodId     string `json:"vod_id"` // action=delete_video 时使用
}

func (r *SourceActionReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		Action    string          `json:"action"`
		VodId     json.RawMessage `json:"vod_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.Action = raw.Action
	r.VodId = normalizeId(raw.VodId)
	return nil
}

// RunSourceAction 统一执行数据源管理操作
// action:
//   - truncate       // 仅清空该源的视频/剧集/分类数据（保留 source 元信息）
//   - recreate       // 删除并重建该源的三张表（数据全部丢失）
//   - delete_source  // 删除该源的所有表 + sources 记录
//   - delete_video   // 删除单条 vod_id（同时清理剧集）
func (a *App) RunSourceAction(req SourceActionReq) (string, error) {
	if req.SourceKey == "" {
		return "", fmt.Errorf("source_key is empty")
	}
	switch strings.ToLower(strings.TrimSpace(req.Action)) {
	case "truncate":
		if _, err := handler.TruncateSourceData(req.SourceKey); err != nil {
			return "", err
		}
		return "truncate ok", nil
	case "recreate":
		if _, err := handler.RecreateSourceTables(req.SourceKey); err != nil {
			return "", err
		}
		return "recreate ok", nil
	case "delete_source":
		// 先删表，再删 sources 记录
		if err := handler.DeleteSource(req.SourceKey); err != nil {
			return "", err
		}
		return "delete_source ok", nil
	case "delete_video":
		if req.VodId == "" {
			return "", fmt.Errorf("vod_id is empty")
		}
		if _, err := handler.DeleteSourceVideo(req.SourceKey, req.VodId); err != nil {
			return "", err
		}
		return "delete_video ok", nil
	default:
		return "", fmt.Errorf("unknown action: %s", req.Action)
	}
}

// ======================== Collect ========================

func (a *App) StartCollect(req handler.CollectReq) (*handler.CollectStatus, error) {
	return a.collection.Start(req)
}

// PauseCollect 暂停指定 source_key 的采集
func (a *App) PauseCollect(req handler.CollectReq) (bool, error) {
	return a.collection.Pause(req.SourceKey), nil
}

// ResumeCollect 恢复指定 source_key 的采集
func (a *App) ResumeCollect(req handler.CollectReq) (bool, error) {
	return a.collection.Resume(req.SourceKey), nil
}

// StopCollect 停止指定 source_key 的采集
func (a *App) StopCollect(req handler.CollectReq) (bool, error) {
	return a.collection.Stop(req.SourceKey), nil
}

// GetCollectStatus 返回指定 source 的采集状态
func (a *App) GetCollectStatus(sourceKey string) *handler.CollectStatus {
	return a.collection.Status(sourceKey)
}

// SearchSource 用 wd=keyword 去指定源站搜索指定页，返回富字段结果（不入库）
// page=1 开始；pageSize<=0 时使用源的默认条数
// 入库请调用 ImportSourceVideos
func (a *App) SearchSource(sourceKey string, keyword string, page int, pageSize int) (*handler.SearchSourceResult, error) {
	return handler.SearchSource(sourceKey, keyword, page, pageSize)
}

// ImportSourceVideos 将用户挑选的源站搜索视频入库（压缩字段 + 合并写入）
// sourceKey 用于确定入库的目标源表；videos 中携带的豆瓣信息也会写入全局 douban_info 表
// 返回成功入库的条数
func (a *App) ImportSourceVideos(sourceKey string, videos []*model.Video) (int, error) {
	return handler.ImportSourceVideos(sourceKey, videos)
}

// GetSourceParamsDoc 返回采集接口参数规范，供前端展示规则指南
func (a *App) GetSourceParamsDoc(sourceKey string) (*handler.SourceParamsDoc, error) {
	return handler.GetSourceParamsDoc(sourceKey)
}

// ======================== 采集调度器 ========================

// GetCollectSchedule 返回采集调度器配置与运行状态
func (a *App) GetCollectSchedule() *handler.SchedulerStatus {
	return a.schedulers.Status()
}

// SetCollectSchedule 修改采集调度配置（持久化到 settings）
func (a *App) SetCollectSchedule(cfg handler.CollectScheduleConfig) (handler.CollectScheduleConfig, error) {
	return a.schedulers.SetConfig(cfg)
}

// TriggerCollectNow 立即触发一次后台采集（不影响定时）
// sourceKey 非空则仅采集该源；mode 可选 full/incremental/once
type TriggerCollectReq struct {
	SourceKey string `json:"source_key"`
	Mode      string `json:"mode"`  // full / incremental / once
	Hours     int    `json:"hours"` // 增量模式的回溯小时数
}

func (a *App) TriggerCollectNow(req TriggerCollectReq) (bool, error) {
	a.schedulers.Trigger(req.SourceKey, req.Mode, req.Hours)
	return true, nil
}

// StopBackgroundCollect 停止后台循环采集，并等待短时间让状态同步
func (a *App) StopBackgroundCollect() (bool, error) {
	a.schedulers.Stop()
	// 标记强制退出，下次 Window.Close() 直接通过
	a.forceQuit.Store(true)
	// 短暂等待确保 Stop 已把 running 置为 false 后返回（前端状态即时刷新）
	time.Sleep(50 * time.Millisecond)
	return true, nil
}

// SetSourceSchedule 设置单个源的调度配置
func (a *App) SetSourceSchedule(req handler.SourceScheduleReq) error {
	return a.schedulers.SetSourceSchedule(req)
}

// ======================== Douban ========================

// DoubanSearchReq 豆瓣搜索请求
type DoubanSearchReq struct {
	Keyword string `json:"keyword"`
}

// DoubanSearchResp 豆瓣搜索响应
type DoubanSearchResp struct {
	SubjectID string `json:"subject_id"`
	URL       string `json:"url"`
}

// DoubanSearch 搜索豆瓣获取 subject_id
func (a *App) DoubanSearch(req DoubanSearchReq) (*DoubanSearchResp, error) {
	if req.Keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	// GlobalID 留空：这是用户在界面上主动发起的搜索，不该被某条记录的 24 小时
	// 「查无此片」冷却挡住；反过来说它的失败结果也不该写进任何一行的冷却。
	id, err := douban.SearchSubjectID(req.Keyword, douban.SearchMeta{VodName: req.Keyword})
	if err != nil {
		return nil, err
	}
	return &DoubanSearchResp{
		SubjectID: id,
		URL:       "https://movie.douban.com/subject/" + id + "/",
	}, nil
}

// DoubanDetailReq 豆瓣详情请求
type DoubanDetailReq struct {
	SubjectID string `json:"subject_id"`
}

// DoubanDetailResp 豆瓣详情响应
type DoubanDetailResp struct {
	*douban.DoubanInfo
}

// DoubanDetail 获取豆瓣详情信息
func (a *App) DoubanDetail(req DoubanDetailReq) (*DoubanDetailResp, error) {
	if req.SubjectID == "" {
		return nil, fmt.Errorf("subject_id 不能为空")
	}
	info, err := douban.ParseDetail(req.SubjectID)
	if err != nil {
		return nil, err
	}
	return &DoubanDetailResp{DoubanInfo: info}, nil
}

// DoubanUpdateVideoReq 更新单个视频的豆瓣信息请求
type DoubanUpdateVideoReq struct {
	Keyword string `json:"keyword"`
}

// DoubanUpdateVideo 手动为某个视频补全豆瓣信息（搜索+解析详情，存入全局表）
func (a *App) DoubanUpdateVideo(req DoubanUpdateVideoReq) (*douban.DoubanInfo, error) {
	if req.Keyword == "" {
		return nil, fmt.Errorf("关键词不能为空")
	}
	return a.schedulers.UpdateDoubanByKeyword(req.Keyword)
}

// DoubanChart 获取豆瓣热榜视频（立即返回，带匹配状态）
func (a *App) DoubanChart() ([]douban.ChartVideoItem, error) {
	return douban.GetChartVideos()
}

// DoubanChartResolve 用户点击时解析热榜视频（实时检查匹配状态）
func (a *App) DoubanChartResolve(subjectID string) (*douban.ChartVideoItem, error) {
	return douban.ResolveChartVideo(subjectID)
}

// DoubanTriggerNow 立即触发一次豆瓣信息补全
func (a *App) DoubanTriggerNow() (int, error) {
	return a.schedulers.TriggerDouban()
}

// DoubanStatus 返回豆瓣调度器状态
func (a *App) DoubanStatus() map[string]interface{} {
	result := map[string]interface{}{
		"running": false,
	}
	running, updating := a.schedulers.DoubanStatus()
	result["running"] = running
	result["updating"] = updating
	// 附加数据统计
	if all, err := db.GetAllDoubanInfo(); err == nil {
		result["total"] = len(all)
		completed := 0
		for _, r := range all {
			if r.SubjectID != "" {
				completed++
			}
		}
		result["completed"] = completed
		result["pending"] = len(all) - completed
	}
	return result
}

// DoubanGetAllReq 豆瓣数据请求（支持分页）
type DoubanGetAllReq struct {
	Page     int `json:"page"`
	PageSize int `json:"page_size"`
}

// DoubanGetAllResp 豆瓣数据响应（含分页信息）
type DoubanGetAllResp struct {
	Rows     []*db.DoubanInfoRow `json:"rows"`
	Total    int                 `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}

// DoubanGetAll 获取全局豆瓣信息表中的记录（支持分页）
func (a *App) DoubanGetAll(req DoubanGetAllReq) (*DoubanGetAllResp, error) {
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	rows, total, err := db.GetAllDoubanInfoPaginated(req.Page, req.PageSize)
	if err != nil {
		return nil, err
	}
	return &DoubanGetAllResp{
		Rows:     rows,
		Total:    total,
		Page:     req.Page,
		PageSize: req.PageSize,
	}, nil
}

// ======================== 豆瓣评论 ========================

// DoubanCommentsReq 获取豆瓣评论请求
type DoubanCommentsReq struct {
	DoubanId string `json:"douban_id"`
	Page     int    `json:"page"`
	Sort     string `json:"sort"` // "new_score" | "time"
}

// GetDoubanComments 获取豆瓣评论（带 24h 缓存）
func (a *App) GetDoubanComments(req DoubanCommentsReq) (*douban.DoubanCommentsResp, error) {
	if req.DoubanId == "" {
		return nil, fmt.Errorf("douban_id 不能为空")
	}
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.Sort == "" {
		req.Sort = "new_score"
	}
	return douban.FetchComments(req.DoubanId, req.Page, req.Sort)
}

// ======================== 全局类型管理 ========================

// GlobalTypeItem 全局类型项
type GlobalTypeItem struct {
	Id             int    `json:"id"`
	TypeName       string `json:"type_name"`
	CollectEnabled int    `json:"collect_enabled"`
	Sort           int    `json:"sort"`
	CreatedAt      string `json:"created_at"`
}

// GetGlobalTypes 获取所有全局类型
func (a *App) GetGlobalTypes() ([]*db.GlobalTypeRow, error) {
	return db.GetAllGlobalTypes()
}

// SetGlobalTypeCollectEnabledReq 设置全局类型采集开关请求
type SetGlobalTypeCollectEnabledReq struct {
	TypeName string `json:"type_name"`
	Enabled  bool   `json:"enabled"`
}

// SetGlobalTypeCollectEnabled 设置全局类型的采集开关
func (a *App) SetGlobalTypeCollectEnabled(req SetGlobalTypeCollectEnabledReq) (bool, error) {
	if req.TypeName == "" {
		return false, fmt.Errorf("type_name is empty")
	}
	if err := db.SetGlobalTypeCollectEnabled(req.TypeName, req.Enabled); err != nil {
		return false, err
	}
	return true, nil
}

// SyncGlobalTypes 从所有源同步类型到全局类型表
func (a *App) SyncGlobalTypes() (int, error) {
	return db.SyncGlobalTypesFromSources()
}

// ======================== 日志 ========================

// LogEntry 前端写入的日志条目
type LogEntry struct {
	Level   string `json:"level"`   // INFO / WARN / ERROR
	Message string `json:"message"` // 消息
	Source  string `json:"source"`  // 可选：来源（组件/文件）
	Detail  string `json:"detail"`  // 可选：详细堆栈或上下文
}

// WriteLog 写入一条日志；同时记录到文件（按天滚动）并进入内存时间线
func (a *App) WriteLog(entry LogEntry) (bool, error) {
	msg := entry.Message
	if entry.Source != "" {
		msg = entry.Source + " :: " + msg
	}
	if entry.Detail != "" {
		msg = msg + "\n    Detail: " + entry.Detail
	}
	switch entry.Level {
	case "WARN", "warn", "warning":
		applog.Warn("%s", msg)
	case "ERROR", "error", "err":
		applog.Error("%s", msg)
	default:
		applog.Info("%s", msg)
	}
	return true, nil
}

// GetLogList 返回可用日志文件名列表（按时间倒序）
func (a *App) GetLogList() []string {
	return applog.Default().ListFiles()
}

// GetLogContent 返回指定日志文件的完整内容
func (a *App) GetLogContent(filename string) (string, error) {
	return applog.Default().ReadFile(filename)
}

// GetLogDir 返回日志所在目录（方便前端在界面上展示"打开日志目录"）
func (a *App) GetLogDir() string {
	return applog.Default().Dir()
}

// ClearLogs 删除所有日志文件
func (a *App) ClearLogs() (int, error) {
	n := applog.Default().Clear()
	return n, nil
}

// ======================== Favorites ========================

type FavReq struct {
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
	GlobalId  int    `json:"global_id"`
}

func (r *FavReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
		GlobalId  int             `json:"global_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.VodId = normalizeId(raw.VodId)
	r.GlobalId = raw.GlobalId
	return nil
}

func (a *App) AddFavorite(req FavReq) error {
	return a.media.AddFavorite(req.SourceKey, req.VodId)
}

func (a *App) RemoveFavorite(req FavReq) error {
	return a.media.RemoveFavorite(req.SourceKey, req.VodId)
}

func (a *App) IsFavorite(req FavReq) (bool, error) {
	return a.media.IsFavorite(req.SourceKey, req.VodId)
}

func (a *App) GetFavorites(page, pageSize int) ([]db.FavWithVideo, error) {
	return a.media.Favorites(page, pageSize)
}

// ======================== Watch History ========================

type HistoryReq struct {
	SourceKey string  `json:"source_key"`
	VodId     string  `json:"vod_id"`
	EpNum     int     `json:"ep_num"`
	Position  float64 `json:"position"`
}

func (r *HistoryReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
		EpNum     json.RawMessage `json:"ep_num"`
		Position  json.RawMessage `json:"position"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.VodId = normalizeId(raw.VodId)
	if v, err := parseInt(raw.EpNum, 1); err == nil {
		r.EpNum = v
	}
	if v, err := parseFloat(raw.Position, 0); err == nil {
		r.Position = v
	}
	return nil
}

func (a *App) SaveWatchHistory(req HistoryReq) error {
	return a.media.SaveHistory(req.SourceKey, req.VodId, req.EpNum, req.Position)
}

func (a *App) GetRecentHistory(limit int) ([]*handler.HistoryItemWithVideo, error) {
	return a.media.RecentHistory(limit)
}

// GetHistoryPosition 返回某集上次播放到的位置（秒），无记录时为 0。
func (a *App) GetHistoryPosition(req HistoryReq) (float64, error) {
	return a.media.HistoryPosition(req.SourceKey, req.VodId, req.EpNum)
}

// DeleteHistoryItem 删除单条观看历史
func (a *App) DeleteHistoryItem(req HistoryReq) error {
	return a.media.DeleteHistoryItem(req.SourceKey, req.VodId, req.EpNum)
}

// DeleteHistoryByVideo 删除某个视频的全部观看历史
func (a *App) DeleteHistoryByVideo(req FavReq) error {
	return a.media.DeleteHistoryByVideo(req.SourceKey, req.VodId)
}

// ClearAllHistory 清空全部观看历史，返回删除的条数
func (a *App) ClearAllHistory() (int, error) {
	return a.media.ClearHistory()
}

// GetWatchedEpisodes 返回指定视频已观看的所有集数
func (a *App) GetWatchedEpisodes(req FavReq) ([]int, error) {
	return a.media.WatchedEpisodes(req.SourceKey, req.VodId)
}

// --- helpers ---
func normalizeId(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
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
func parseInt(raw json.RawMessage, def int) (int, error) {
	if len(raw) == 0 {
		return def, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return def, err
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return def, err
		}
		return v, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return def, err
	}
	return v, nil
}
func parseFloat(raw json.RawMessage, def float64) (float64, error) {
	if len(raw) == 0 {
		return def, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return def, err
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return def, err
		}
		return v, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return def, err
	}
	return v, nil
}

// ======================== Settings ========================

func (a *App) GetSetting(key string) (string, error) {
	return a.settings.Get(key)
}

func (a *App) SetSetting(key, value string) error {
	return a.settings.Set(key, value)
}

// settingAllowPrivateNetwork 打开后，媒体与图片代理允许访问私网/回环地址
// （NAS、局域网里的源、本机起的缓存代理）。缺省关闭，理由见 proxy.ValidateTarget。
const settingAllowPrivateNetwork = "proxy_allow_private_network"

// SetAllowPrivateNetwork 落库并立刻生效。改完不用重启是刻意的：用户多半是在
// 「某个源放不出来」时来翻这个开关，必须当场试播才知道是不是它的问题。
func (a *App) SetAllowPrivateNetwork(allow bool) error {
	value := "0"
	if allow {
		value = "1"
	}
	if err := a.settings.Set(settingAllowPrivateNetwork, value); err != nil {
		return err
	}
	proxy.SetAllowPrivateTargets(allow)
	applog.Warn("[Proxy] 内网放行开关已改为 %v", allow)
	return nil
}

func (a *App) GetAllowPrivateNetwork() bool {
	raw, err := a.settings.Get(settingAllowPrivateNetwork)
	return err == nil && strings.TrimSpace(raw) == "1"
}

// ======================== Window / Title Bar ========================
//
// 拖动逻辑由 Wails 自身的 CSS 自定义属性机制实现：
//   · 在 <header class="titlebar"> 上设置 `--wails-draggable: drag`
//   · 在按钮区域上设置 `--wails-draggable: no-drag`
// 所以这里不再需要 Win32 手动调用 SendMessageW。
// 保留以下两个方法给前端用作"切换最大化状态 / 读取最大化状态"的辅助接口。

func (a *App) WindowToggleMax() bool {
	return a.window.ToggleMax()
}

func (a *App) WindowIsMax() bool {
	return a.window.IsMax()
}

// WindowSetFullscreen 切换"系统级全屏"（覆盖任务栏，移除窗口边框）
//
//	enter=true  → 进入全屏
//	enter=false → 退出全屏
//
// Wails 的 WindowFullscreen 在 Windows 上会自动移除标题栏并覆盖任务栏。
func (a *App) WindowSetFullscreen(enter bool) {
	a.window.SetFullscreen(enter)
}

func (a *App) WindowIsFs() bool {
	return a.window.IsFullscreen()
}

// SetTitleBarTheme 切换标题栏主题（"dark" 或 "light"）
// Wails 在启动时已设置 CustomTheme；这里通过 ExecJS 让系统重新应用。
func (a *App) SetTitleBarTheme(theme string) error {
	_ = theme
	a.window.ReloadTitleBar()
	return nil
}

// WindowSetResizable 设置窗口是否可拖动调整大小
func (a *App) WindowSetResizable(resizable bool) {
	a.window.SetResizable(resizable)
}

// WindowGetResizable 返回窗口是否可调整大小
func (a *App) WindowGetResizable() bool {
	return a.window.Resizable()
}

// WindowSetSize 设置窗口尺寸
func (a *App) WindowSetSize(width, height int) {
	a.window.SetSize(width, height)
}

// WindowSizeResp 窗口尺寸响应
type WindowSizeResp struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowGetSize 返回当前窗口尺寸
func (a *App) WindowGetSize() *WindowSizeResp {
	size := a.window.Size()
	return &WindowSizeResp{Width: size.Width, Height: size.Height}
}

// ApplyWindowSettings 从数据库加载并应用窗口设置（启动时调用）
func (a *App) ApplyWindowSettings() {
	a.window.ApplySettings()
}
