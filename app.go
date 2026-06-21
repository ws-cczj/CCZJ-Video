package main

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/ciligou"
	"cczjVideo/app/collect"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"cczjVideo/app/util"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/andybalholm/brotli"
	"github.com/bwmarrin/snowflake"
	"github.com/wailsapp/wails/v3/pkg/application"
)

var imageProxyClient = &http.Client{
	Timeout: 10 * time.Second,
	Transport: &http.Transport{
		MaxIdleConns:        20,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
	},
}

type App struct {
	app              *application.App
	collectMu        sync.Mutex
	doubanScheduler  *douban.Scheduler
	ciligouScheduler *ciligou.Scheduler
	forceQuit        atomic.Bool
}

// ======================== 关闭行为 ========================

var minimizeToTray atomic.Bool

func init() {
	// 初始化默认值：点击关闭按钮时最小化到托盘
	minimizeToTray.Store(true)
}

func (a *App) shouldMinimizeToTray() bool {
	return minimizeToTray.Load()
}

// GetCloseBehavior 返回关闭行为：true=缩小到托盘，false=直接退出
func (a *App) GetCloseBehavior() bool {
	return minimizeToTray.Load()
}

// SetCloseBehavior 设置关闭行为并持久化
func (a *App) SetCloseBehavior(minimize bool) {
	minimizeToTray.Store(minimize)
	val := "0"
	if minimize {
		val = "1"
	}
	_ = db.SetSetting("close_to_tray", val)
}

// RestartApp 重启应用
func (a *App) RestartApp() {
	exe, err := os.Executable()
	if err != nil {
		return
	}

	// 使用 os.StartProcess 比 exec.Command(cmd) 不继承父进程资源，更可靠
	// Windows 上需使用 STARTF_USESTDHANDLES 但直接用 os.StartProcess
	// 并设置 CreationFlags=DETACHED_PROCESS|CREATE_NEW_PROCESS_GROUP 让新进程独立于父进程
	attr := &os.ProcAttr{
		Files: []*os.File{nil, nil, nil},
	}
	// 在 Windows 上为可执行程序通过 cmd.exe 包装一下，确保完全脱离父进程
	if goruntime.GOOS == "windows" {
		// Windows: 使用 DETACHED_PROCESS 标志，完全脱离
		attr.Files = []*os.File{os.Stdin, os.Stdout, os.Stderr}
	}

	// 简单直接使用 exec.Command 搭配 cmd.Run 不等待，用 os.StartProcess
	// 先启动新进程，再退出。使用 os.StartProcess 对跨平台兼容
	_, err = os.StartProcess(exe, []string{exe}, attr)
	if err != nil {
		// 回退到 exec.Command
		cmd := exec.Command(exe)
		_ = cmd.Start()
	}

	// 延迟一点时间给新进程准备好再退出
	time.Sleep(200 * time.Millisecond)
	a.app.Quit()
}

func NewApp() *App {
	return &App{}
}

func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	a.app = application.Get()

	dataDir := a.getDataDir()
	os.MkdirAll(dataDir, 0755)

	// 初始化日志（data/applog 目录），按月滚动，超过2个月自动清理
	logDir := filepath.Join(dataDir, "applog")
	if err := applog.Init(logDir); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to init applog: %v\n", err)
	}

	if err := db.InitDB(dataDir); err != nil {
		panic(fmt.Sprintf("Failed to init database: %v", err))
	}

	// 把 db 层的日志桥接到 applog（避免 db 直接依赖 applog 产生循环）
	db.SetLogger(func(level, msg string) {
		switch level {
		case "ERROR":
			applog.Error(msg)
		case "WARN":
			applog.Warn(msg)
		default:
			applog.Info(msg)
		}
	})

	util.InitSnowFlake()
	_ = snowflake.Epoch

	// 启动后台采集调度器（根据配置决定是否真正运行）
	go func() {
		s := handler.GetScheduler(ctx)
		s.Start()
	}()

	// 启动豆瓣信息补全调度器（每30秒执行一次）
	a.doubanScheduler = douban.NewScheduler(30 * time.Second)
	go a.doubanScheduler.Start()

	// 启动磁力链接爬取调度器（每60秒执行一次）
	a.ciligouScheduler = ciligou.NewScheduler(60 * time.Second)
	go a.ciligouScheduler.Start()

	// 同步全局类型表
	go func() {
		count, err := db.SyncGlobalTypesFromSources()
		if err != nil {
			applog.Warn("同步全局类型失败: %v", err)
		} else {
			applog.Info("同步全局类型完成: %d 条", count)
		}
	}()

	a.app.Event.Emit("app:ready", map[string]string{
		"data_dir": dataDir,
	})

	// 加载上次未完成的下载任务
	a.loadPersistedTasks()

	// 加载关闭行为设置：只有在数据库中明确存在 "0" 时才禁用；首次运行保持默认开启
	if v, err := db.GetSetting("close_to_tray"); err == nil {
		minimizeToTray.Store(v == "1")
	}

	// 应用窗口设置（尺寸、是否可调整大小）
	a.ApplyWindowSettings()

	return nil
}

// getDataDir 返回数据目录路径
func (a *App) getDataDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "data")
}

// ======================== Video ========================

func (a *App) GetVideoList(req handler.VideoListReq) (*handler.VideoListResp, error) {
	return handler.GetVideoList(req)
}

func (a *App) GetVideoDetail(req handler.VideoDetailReq) (*handler.VideoDetailResp, error) {
	return handler.GetVideoDetail(req)
}

func (a *App) SearchVideos(req handler.VideoSearchReq) (*handler.VideoListResp, error) {
	return handler.SearchVideos(req)
}

// GetGlobalIdForVideo 获取指定源中某个视频的 global_id
func (a *App) GetGlobalIdForVideo(sourceKey string, vodId string) (int64, error) {
	return db.GetGlobalIdForVideo(sourceKey, vodId)
}

// FindSourcesByGlobalId 通过 global_id 查找所有拥有该视频的源
func (a *App) FindSourcesByGlobalId(globalId int64) ([]db.SourceVideoRef, error) {
	return db.FindSourcesByGlobalId(globalId)
}

func (a *App) GetTypes(req handler.GetTypesReq) ([]*model.VType, error) {
	return handler.GetTypes(req)
}

// DeleteVideo 删除指定源中的视频（同时清理收藏和历史）
func (a *App) DeleteVideo(req handler.DeleteVideoReq) error {
	return handler.DeleteVideo(req)
}

// GetYearsAndAreas 返回当前源下所有可选的年份/地区，供前端筛选下拉框使用
func (a *App) GetYearsAndAreas(sourceKey string) (*handler.YearsResp, error) {
	return handler.GetYearsAndAreas(sourceKey)
}

// GetRecommend 返回 N 条推荐视频（会排除 excludeIds 中的 vod_id，避免"猜你喜欢"和"继续观看"重复）
type RecommendReq struct {
	SourceKey string   `json:"source_key"`
	Limit     int      `json:"limit"`
	ExcludeIds []string `json:"exclude_ids"`
}

func (a *App) GetRecommend(req RecommendReq) ([]*model.Video, error) {
	if req.Limit <= 0 {
		req.Limit = 8
	}
	if req.ExcludeIds == nil {
		req.ExcludeIds = []string{}
	}
	return handler.GetRecommend(req.SourceKey, req.Limit, req.ExcludeIds)
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
type sourceExportPayload struct {
	Version   int                `json:"version"`
	Exported  string             `json:"exported"`
	Source    *model.Source      `json:"source"`
	Videos    []*db.ExportVideoRow `json:"videos"`
	Types     []*db.ExportTypeRow  `json:"types"`
}

// ExportSource 导出某个源的所有数据到 .json.br 文件（Brotli 压缩），返回绝对路径
// 前端拿到文件路径后可以在系统文件管理器中复制/传给他人
func (a *App) ExportSource(sourceKey string) (string, error) {
	if strings.TrimSpace(sourceKey) == "" {
		return "", fmt.Errorf("source_key 为空")
	}
	// 1) 拿源信息
	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return "", fmt.Errorf("未找到数据源 %s: %w", sourceKey, err)
	}

	// 2) 拿所有视频行（含压缩后的 vod_play_url）
	videos, err := db.ExportAllVideos(sourceKey)
	if err != nil {
		return "", fmt.Errorf("导出视频失败: %w", err)
	}

	// 3) 拿所有类型行
	types, err := db.ExportAllTypes(sourceKey)
	if err != nil {
		return "", fmt.Errorf("导出类型失败: %w", err)
	}

	// 4) 组装 payload
	payload := sourceExportPayload{
		Version:  1,
		Exported: time.Now().Format(time.RFC3339),
		Source:   src,
		Videos:   videos,
		Types:    types,
	}

	// 5) 序列化 JSON 并 Brotli 压缩写入文件
	dataDir := a.getDataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", fmt.Errorf("创建数据目录失败: %w", err)
	}
	exportDir := filepath.Join(dataDir, "exports")
	if err := os.MkdirAll(exportDir, 0755); err != nil {
		return "", fmt.Errorf("创建导出目录失败: %w", err)
	}
	safeName := safeFilename(sourceKey)
	fname := fmt.Sprintf("source_%s_%s.json.br", safeName, time.Now().Format("20060102_150405"))
	fpath := filepath.Join(exportDir, fname)

	f, err := os.OpenFile(fpath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("创建导出文件失败: %w", err)
	}
	defer f.Close()

	// 使用 Brotli 压缩（quality=6 兼顾压缩率和速度）
	br := brotli.NewWriterLevel(f, 6)
	enc := json.NewEncoder(br)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		_ = br.Close()
		_ = os.Remove(fpath)
		return "", fmt.Errorf("写入 JSON 失败: %w", err)
	}
	if err := br.Close(); err != nil {
		_ = os.Remove(fpath)
		return "", fmt.Errorf("压缩失败: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(fpath)
		return "", fmt.Errorf("关闭文件失败: %w", err)
	}

	logInfo(fmt.Sprintf("ExportSource: 导出源 %s (%d 条视频, %d 条类型) -> %s",
		sourceKey, len(videos), len(types), fpath))
	return fpath, nil
}

// ImportSource 从本地文件导入一个数据源（支持 .json.br / .json.gz / .json）
// 若同名 source_key 已存在，会直接合并（Upsert 视频），由调用方先清库或删除
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
	dec := json.NewDecoder(reader)
	if err := dec.Decode(&payload); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if payload.Source == nil {
		return "", fmt.Errorf("文件中缺少 source 信息")
	}
	sourceKey := strings.TrimSpace(payload.Source.SourceKey)
	if sourceKey == "" {
		return "", fmt.Errorf("source.source_key 为空")
	}

	// 先把 source 写入 sources 表（不存在则插入，存在则更新）
	existing, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		// 不存在 -> 新增
		if err := db.AddSource(payload.Source); err != nil {
			return "", fmt.Errorf("写入 source 失败: %w", err)
		}
	} else {
		// 存在 -> 用导入的覆盖
		merged := existing
		merged.Name = payload.Source.Name
		merged.ApiUrl = payload.Source.ApiUrl
		merged.UrlTemplate = payload.Source.UrlTemplate
		merged.UrlPrefix = payload.Source.UrlPrefix
		merged.UrlSuffix = payload.Source.UrlSuffix
		merged.AdvConfigRaw = payload.Source.AdvConfigRaw
		merged.ScheduleCfgRaw = payload.Source.ScheduleCfgRaw
		advCfg := merged.GetAdvConfig()
		if payload.Source.CollectLimit > 0 && advCfg.CollectLimit == 0 {
			advCfg.CollectLimit = payload.Source.CollectLimit
			merged.SetAdvConfig(advCfg)
		}
		if payload.Source.CollectHours > 0 && advCfg.CollectHours == 0 {
			advCfg.CollectHours = payload.Source.CollectHours
			merged.SetAdvConfig(advCfg)
		}
		if err := db.UpdateSource(merged); err != nil {
			return "", fmt.Errorf("更新 source 失败: %w", err)
		}
	}

	// 写入类型
	if len(payload.Types) > 0 {
		if err := db.ImportTypes(sourceKey, payload.Types); err != nil {
			return "", fmt.Errorf("导入类型失败: %w", err)
		}
	}

	// 写入视频
	if len(payload.Videos) > 0 {
		if err := db.ImportVideos(sourceKey, payload.Videos); err != nil {
			return "", fmt.Errorf("导入视频失败: %w", err)
		}
	}

	msg := fmt.Sprintf("导入完成: 源 %q, %d 条视频, %d 条类型",
		sourceKey, len(payload.Videos), len(payload.Types))
	logInfo("ImportSource: " + msg)
	return msg, nil
}

// ImportSourceFromBase64 从 base64 编码的文件内容导入数据源
// 前端只能通过 FileReader 拿到 base64/ArrayBuffer，无法拿到真实文件路径，
// 所以提供此版本绕开“路径”限制；内容支持 .json.br / .json.gz / .json
func (a *App) ImportSourceFromBase64(filename string, b64Content string) (string, error) {
	if strings.TrimSpace(b64Content) == "" {
		return "", fmt.Errorf("文件内容为空")
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
	dec := json.NewDecoder(reader)
	if err := dec.Decode(&payload); err != nil {
		return "", fmt.Errorf("解析 JSON 失败: %w", err)
	}
	if payload.Source == nil {
		return "", fmt.Errorf("文件中缺少 source 信息")
	}
	sourceKey := strings.TrimSpace(payload.Source.SourceKey)
	if sourceKey == "" {
		return "", fmt.Errorf("source.source_key 为空")
	}

	// 写 source 表
	existing, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		if err := db.AddSource(payload.Source); err != nil {
			return "", fmt.Errorf("写入 source 失败: %w", err)
		}
	} else {
		merged := existing
		merged.Name = payload.Source.Name
		merged.ApiUrl = payload.Source.ApiUrl
		merged.UrlTemplate = payload.Source.UrlTemplate
		merged.UrlPrefix = payload.Source.UrlPrefix
		merged.UrlSuffix = payload.Source.UrlSuffix
		merged.AdvConfigRaw = payload.Source.AdvConfigRaw
		merged.ScheduleCfgRaw = payload.Source.ScheduleCfgRaw
		advCfg := merged.GetAdvConfig()
		if payload.Source.CollectLimit > 0 && advCfg.CollectLimit == 0 {
			advCfg.CollectLimit = payload.Source.CollectLimit
			merged.SetAdvConfig(advCfg)
		}
		if payload.Source.CollectHours > 0 && advCfg.CollectHours == 0 {
			advCfg.CollectHours = payload.Source.CollectHours
			merged.SetAdvConfig(advCfg)
		}
		if err := db.UpdateSource(merged); err != nil {
			return "", fmt.Errorf("更新 source 失败: %w", err)
		}
	}

	if len(payload.Types) > 0 {
		if err := db.ImportTypes(sourceKey, payload.Types); err != nil {
			return "", fmt.Errorf("导入类型失败: %w", err)
		}
	}
	if len(payload.Videos) > 0 {
		if err := db.ImportVideos(sourceKey, payload.Videos); err != nil {
			return "", fmt.Errorf("导入视频失败: %w", err)
		}
	}

	msg := fmt.Sprintf("导入完成: 源 %q, %d 条视频, %d 条类型",
		sourceKey, len(payload.Videos), len(payload.Types))
	logInfo("ImportSourceFromBase64: " + msg)
	return msg, nil
}

// OpenFolder 用系统文件管理器打开指定目录（便于用户拿到导出文件）
func (a *App) OpenFolder(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("路径为空")
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
	applog.Info(msg)
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
		if err := db.DropSourceTables(req.SourceKey); err != nil {
			return "", err
		}
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
	return a.doCollect(req)
}

func (a *App) doCollect(req handler.CollectReq) (*handler.CollectStatus, error) {
	entry := handler.GetOrCreateEngine(req.SourceKey)
	if entry.IsRunning() {
		return nil, fmt.Errorf("采集源 %s 正在采集中", req.SourceKey)
	}

	mode := model.CollectMode(req.Mode)
	if mode == "" {
		mode = model.CollectModeFull
	}

	var engineOpts []collect.EngineOption
	engineOpts = append(engineOpts, collect.WithCollectMode(mode))
	if mode == model.CollectModeIncremental && req.Hours > 0 {
		engineOpts = append(engineOpts, collect.WithTimeHours(req.Hours))
	}

	engine := collect.NewEngineV2(
		req.SourceKey,
		func(msg string) {
			a.app.Event.Emit("collect:log", map[string]interface{}{
				"source_key": req.SourceKey,
				"message":    msg,
			})
		},
		func(current, total int) {
			entry.UpdateProgress(current, total)
			a.app.Event.Emit("collect:progress", map[string]interface{}{
				"source_key": req.SourceKey,
				"current":    current,
				"total":      total,
			})
		},
		func(page int, names []string) {
			entry.UpdatePageNames(page, names)
			a.app.Event.Emit("collect:page", map[string]interface{}{
				"source_key": req.SourceKey,
				"page":       page,
				"names":      names,
			})
		},
		engineOpts...,
	)
	engine.SetContext(context.Background())
	entry.BindEngine(engine, string(mode))

	go func() {
		_, err := engine.Run()
		entry.MarkDone(errStr(err))
		a.app.Event.Emit("collect:done", map[string]interface{}{
			"source_key": req.SourceKey,
			"error":      errStr(err),
			"mode":       string(mode),
		})
	}()

	return handler.GetCollectStatus(req.SourceKey), nil
}

// PauseCollect 暂停指定 source_key 的采集
func (a *App) PauseCollect(req handler.CollectReq) (bool, error) {
	return handler.PauseCollect(req.SourceKey), nil
}

// ResumeCollect 恢复指定 source_key 的采集
func (a *App) ResumeCollect(req handler.CollectReq) (bool, error) {
	return handler.ResumeCollect(req.SourceKey), nil
}

// StopCollect 停止指定 source_key 的采集
func (a *App) StopCollect(req handler.CollectReq) (bool, error) {
	return handler.StopCollect(req.SourceKey), nil
}

// GetCollectStatus 返回指定 source 的采集状态
func (a *App) GetCollectStatus(sourceKey string) *handler.CollectStatus {
	return handler.GetCollectStatus(sourceKey)
}

// SearchSource 用 wd=keyword 去指定源站模糊搜索，把结果入库，并返回
// limit=0 时使用源的默认条数
func (a *App) SearchSource(sourceKey string, keyword string, limit int) (*handler.SearchSourceResult, error) {
	return handler.SearchSource(sourceKey, keyword, limit)
}

// GetSourceParamsDoc 返回采集接口参数规范，供前端展示规则指南
func (a *App) GetSourceParamsDoc(sourceKey string) (*handler.SourceParamsDoc, error) {
	return handler.GetSourceParamsDoc(sourceKey)
}

// ======================== 采集调度器 ========================

// GetCollectSchedule 返回采集调度器配置与运行状态
func (a *App) GetCollectSchedule() *handler.SchedulerStatus {
	s := handler.GetScheduler(context.Background())
	status := s.Status()
	return &status
}

// SetCollectSchedule 修改采集调度配置（持久化到 settings）
func (a *App) SetCollectSchedule(cfg handler.CollectScheduleConfig) (handler.CollectScheduleConfig, error) {
	if err := handler.SetScheduleConfig(cfg); err != nil {
		return cfg, err
	}
	// 让调度器在下次循环使用最新配置
	s := handler.GetScheduler(context.Background())
	s.ReloadConfig()
	// 如果后台采集从"关"变为"开"，重启调度器
	if cfg.EnableBackground {
		s.Stop()
		time.Sleep(100 * time.Millisecond)
		go func() {
			ns := handler.GetScheduler(context.Background())
			ns.Start()
		}()
	} else {
		s.Stop()
	}
	return handler.GetScheduleConfig(), nil
}

// TriggerCollectNow 立即触发一次后台采集（不影响定时）
// sourceKey 非空则仅采集该源；mode 可选 full/incremental/once
type TriggerCollectReq struct {
	SourceKey string `json:"source_key"`
	Mode      string `json:"mode"`  // full / incremental / once
	Hours     int    `json:"hours"` // 增量模式的回溯小时数
}

func (a *App) TriggerCollectNow(req TriggerCollectReq) (bool, error) {
	s := handler.GetScheduler(context.Background())
	m := model.CollectMode(req.Mode)
	if m == "" {
		m = model.CollectModeFull
	}
	if req.SourceKey != "" {
		s.TriggerOne(req.SourceKey, m, req.Hours)
	} else {
		// 全部源
		go s.TriggerNow()
	}
	return true, nil
}

// StopBackgroundCollect 停止后台循环采集，并等待短时间让状态同步
func (a *App) StopBackgroundCollect() (bool, error) {
	s := handler.GetScheduler(context.Background())
	s.Stop()
	// 同时停止豆瓣调度器
	if a.doubanScheduler != nil {
		a.doubanScheduler.Stop()
	}
	// 停止磁力链接调度器
	if a.ciligouScheduler != nil {
		a.ciligouScheduler.Stop()
	}
	// 标记强制退出，下次 Window.Close() 直接通过
	a.forceQuit.Store(true)
	// 短暂等待确保 Stop 已把 running 置为 false 后返回（前端状态即时刷新）
	time.Sleep(50 * time.Millisecond)
	return true, nil
}

// SetSourceSchedule 设置单个源的调度配置
func (a *App) SetSourceSchedule(req handler.SourceScheduleReq) error {
	src, err := db.GetSourceByKey(req.SourceKey)
	if err != nil {
		return fmt.Errorf("源不存在: %s", req.SourceKey)
	}

	intervalMin := req.IntervalMin
	if intervalMin < 5 {
		intervalMin = 5
	}
	sc := model.ScheduleConfig{
		Enabled:     req.Enabled,
		Mode:        model.CollectMode(req.Mode),
		IntervalMin: intervalMin,
	}
	src.SetScheduleConfig(&sc)

	if err := db.UpdateSource(src); err != nil {
		return err
	}

	// 更新调度器中该源的定时器
	s := handler.GetScheduler(context.Background())
	s.UpdateSourceSchedule(req.SourceKey)
	return nil
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
	id, err := douban.SearchSubjectID(req.Keyword)
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
	return a.doubanScheduler.Updater().UpdateSingleByKeyword(req.Keyword)
}

// DoubanTriggerNow 立即触发一次豆瓣信息补全
func (a *App) DoubanTriggerNow() (int, error) {
	if a.doubanScheduler == nil {
		return 0, fmt.Errorf("豆瓣调度器未启动")
	}
	return a.doubanScheduler.TriggerNow()
}

// DoubanStatus 返回豆瓣调度器状态
func (a *App) DoubanStatus() map[string]interface{} {
	result := map[string]interface{}{
		"running": false,
	}
	if a.doubanScheduler != nil {
		result["running"] = a.doubanScheduler.IsRunning()
		result["updating"] = a.doubanScheduler.Updater().IsRunning()
	}
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

// ======================== 全局类型管理 ========================

// GlobalTypeItem 全局类型项
type GlobalTypeItem struct {
	Id              int    `json:"id"`
	TypeName        string `json:"type_name"`
	CollectEnabled  int    `json:"collect_enabled"`
	MagnetEnabled   int    `json:"magnet_enabled"`
	Sort            int    `json:"sort"`
	CreatedAt       string `json:"created_at"`
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

// SetGlobalTypeMagnetEnabledReq 设置全局类型磁力开关请求
type SetGlobalTypeMagnetEnabledReq struct {
	TypeName string `json:"type_name"`
	Enabled  bool   `json:"enabled"`
}

// SetGlobalTypeMagnetEnabled 设置全局类型的磁力链接获取开关
func (a *App) SetGlobalTypeMagnetEnabled(req SetGlobalTypeMagnetEnabledReq) (bool, error) {
	if req.TypeName == "" {
		return false, fmt.Errorf("type_name is empty")
	}
	if err := db.SetGlobalTypeMagnetEnabled(req.TypeName, req.Enabled); err != nil {
		return false, err
	}
	return true, nil
}

// SyncGlobalTypes 从所有源同步类型到全局类型表
func (a *App) SyncGlobalTypes() (int, error) {
	return db.SyncGlobalTypesFromSources()
}

// ======================== 磁力链接 ========================

// CiligouStatus 磁力链接调度器状态
func (a *App) CiligouStatus() map[string]interface{} {
	result := map[string]interface{}{
		"running": false,
	}
	if a.ciligouScheduler != nil {
		result["running"] = a.ciligouScheduler.IsRunning()
		result["updating"] = a.ciligouScheduler.Updater().IsRunning()
	}
	return result
}

// CiligouTriggerNow 立即触发一次磁力链接获取
func (a *App) CiligouTriggerNow() (int, error) {
	if a.ciligouScheduler == nil {
		return 0, fmt.Errorf("磁力链接调度器未启动")
	}
	return a.ciligouScheduler.TriggerNow()
}

// ======================== 日志 ========================

// LogEntry 前端写入的日志条目
type LogEntry struct {
	Level   string `json:"level"`   // INFO / WARN / ERROR
	Message string `json:"message"` // 消息
	Source  string `json:"source"`  // 可选：来源（组件/文件）
	Detail  string `json:"detail"`  // 可选：详细堆栈或上下文
}

// WriteLog 写入一条日志；同时记录到文件（自动按月滚动）
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
		applog.Warn(msg)
	case "ERROR", "error", "err":
		applog.Error(msg)
	default:
		applog.Info(msg)
	}
	return true, nil
}

// GetLogList 返回可用日志文件名列表（按时间倒序）
func (a *App) GetLogList() []string {
	return applog.Default().ListFiles()
}

// GetLogContent 返回指定日志文件的最新内容（最多 512KB）
func (a *App) GetLogContent(filename string) (string, error) {
	return applog.Default().ReadFile(filename)
}

// GetLogContentTail 返回指定日志文件的末尾 N 行内容
func (a *App) GetLogContentTail(filename string, tailLines int) (string, error) {
	return applog.Default().ReadFileTail(filename, tailLines)
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

// getVodName 从源视频表中查找 vod_name，用于兼容旧 API（sourceKey+vodId → vodName）
func (a *App) getVodName(sourceKey, vodId string) (string, error) {
	v, err := db.GetVideoById(sourceKey, vodId)
	if err != nil {
		return "", fmt.Errorf("视频不存在: %w", err)
	}
	return v.VodName, nil
}

type FavReq struct {
	SourceKey string `json:"source_key"`
	VodId     string `json:"vod_id"`
}

func (r *FavReq) UnmarshalJSON(data []byte) error {
	var raw struct {
		SourceKey string          `json:"source_key"`
		VodId     json.RawMessage `json:"vod_id"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	r.SourceKey = raw.SourceKey
	r.VodId = normalizeId(raw.VodId)
	return nil
}

func (a *App) AddFavorite(req FavReq) error {
	vodName, err := a.getVodName(req.SourceKey, req.VodId)
	if err != nil {
		return err
	}
	return db.AddFavorite(req.SourceKey, req.VodId, vodName)
}

func (a *App) RemoveFavorite(req FavReq) error {
	vodName, err := a.getVodName(req.SourceKey, req.VodId)
	if err != nil {
		return err
	}
	return db.RemoveFavorite(vodName, req.SourceKey)
}

func (a *App) IsFavorite(req FavReq) (bool, error) {
	vodName, err := a.getVodName(req.SourceKey, req.VodId)
	if err != nil {
		return false, nil
	}
	return db.IsFavorite(vodName, req.SourceKey), nil
}

func (a *App) GetFavorites(page, pageSize int) ([]db.FavWithVideo, error) {
	return db.GetFavorites(page, pageSize)
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
	vodName, err := a.getVodName(req.SourceKey, req.VodId)
	if err != nil {
		return err
	}
	return db.SaveWatchHistory(req.SourceKey, req.VodId, vodName, req.EpNum, req.Position)
}

func (a *App) GetRecentHistory(limit int) ([]*handler.HistoryItemWithVideo, error) {
	if limit <= 0 {
		limit = 20
	}
	raw, err := db.GetRecentHistory(limit)
	if err != nil {
		return nil, err
	}
	return handler.HydrateHistory("", raw), nil
}

// DeleteHistoryItem 删除单条观看历史
func (a *App) DeleteHistoryItem(req HistoryReq) error {
	vodName, err := a.getVodName(req.SourceKey, req.VodId)
	if err != nil {
		return err
	}
	row, err := db.GetGlobalVideoByName(vodName)
	if err != nil {
		return err
	}
	return db.DeleteHistoryItem(row.Id, req.EpNum)
}

// DeleteHistoryByVideo 删除某个视频的全部观看历史
func (a *App) DeleteHistoryByVideo(req FavReq) error {
	return db.DeleteHistoryByVideo(req.SourceKey, req.VodId)
}

// ClearAllHistory 清空全部观看历史，返回删除的条数
func (a *App) ClearAllHistory() (int, error) {
	n, err := db.ClearAllHistory()
	return int(n), err
}

// GetWatchedEpisodes 返回指定视频已观看的所有集数
func (a *App) GetWatchedEpisodes(req FavReq) ([]int, error) {
	vodName, err := a.getVodName(req.SourceKey, req.VodId)
	if err != nil {
		return nil, err
	}
	return db.GetWatchedEpisodes(vodName)
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
	return db.GetSetting(key)
}

func (a *App) SetSetting(key, value string) error {
	return db.SetSetting(key, value)
}

// ======================== Window / Title Bar ========================
//
// 拖动逻辑由 Wails 自身的 CSS 自定义属性机制实现：
//   · 在 <header class="titlebar"> 上设置 `--wails-draggable: drag`
//   · 在按钮区域上设置 `--wails-draggable: no-drag`
// 所以这里不再需要 Win32 手动调用 SendMessageW。
// 保留以下两个方法给前端用作"切换最大化状态 / 读取最大化状态"的辅助接口。

func (a *App) WindowToggleMax() bool {
	if a.app.Window.Current().IsMaximised() {
		a.app.Window.Current().UnMaximise()
		return false
	}
	a.app.Window.Current().Maximise()
	return true
}

func (a *App) WindowIsMax() bool {
	return a.app.Window.Current().IsMaximised()
}

// WindowSetFullscreen 切换"系统级全屏"（覆盖任务栏，移除窗口边框）
//   enter=true  → 进入全屏
//   enter=false → 退出全屏
// Wails 的 WindowFullscreen 在 Windows 上会自动移除标题栏并覆盖任务栏。
func (a *App) WindowSetFullscreen(enter bool) {
	if enter {
		a.app.Window.Current().Fullscreen()
	} else {
		a.app.Window.Current().UnFullscreen()
	}
}

func (a *App) WindowIsFs() bool {
	return a.app.Window.Current().IsFullscreen()
}

// SetTitleBarTheme 切换标题栏主题（"dark" 或 "light"）
// Wails 在启动时已设置 CustomTheme；这里通过 ExecJS 让系统重新应用。
func (a *App) SetTitleBarTheme(theme string) error {
	_ = theme
	a.app.Window.Current().ExecJS("location.reload()")
	return nil
}

// WindowSetResizable 设置窗口是否可拖动调整大小
func (a *App) WindowSetResizable(resizable bool) {
	a.app.Window.Current().SetResizable(resizable)
	val := "1"
	if !resizable {
		val = "0"
	}
	_ = db.SetSetting("window_resizable", val)
}

// WindowGetResizable 返回窗口是否可调整大小
func (a *App) WindowGetResizable() bool {
	return a.app.Window.Current().Resizable()
}

// WindowSetSize 设置窗口尺寸
func (a *App) WindowSetSize(width, height int) {
	if width < 800 {
		width = 800
	}
	if height < 500 {
		height = 500
	}
	a.app.Window.Current().SetSize(width, height)
	_ = db.SetSetting("window_width", strconv.Itoa(width))
	_ = db.SetSetting("window_height", strconv.Itoa(height))
}

// WindowSizeResp 窗口尺寸响应
type WindowSizeResp struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// WindowGetSize 返回当前窗口尺寸
func (a *App) WindowGetSize() *WindowSizeResp {
	w, h := a.app.Window.Current().Size()
	return &WindowSizeResp{Width: w, Height: h}
}

// ApplyWindowSettings 从数据库加载并应用窗口设置（启动时调用）
func (a *App) ApplyWindowSettings() {
	w := a.app.Window.Current()
	if w == nil {
		return
	}
	if wStr, err := db.GetSetting("window_width"); err == nil {
		if width, err := strconv.Atoi(wStr); err == nil && width >= 800 {
			if hStr, err := db.GetSetting("window_height"); err == nil {
				if height, err := strconv.Atoi(hStr); err == nil && height >= 500 {
					w.SetSize(width, height)
				}
			}
		}
	}
	if v, err := db.GetSetting("window_resizable"); err == nil {
		w.SetResizable(v != "0")
	}
}

// ======================== Video Download ========================

// VideoDownloadReq 前端发起的下载请求
type VideoDownloadReq struct {
	TaskId   string `json:"task_id"`
	Url      string `json:"url"`
	Filename string `json:"filename"` // 不含目录的文件名（含扩展名）
	SaveDir  string `json:"save_dir"` // 可选：自定义保存目录
	Force    bool   `json:"force"`    // 强制覆盖已有任务（用于重复下载确认后）
}

// VideoDownloadStatus 下载状态（用于 GetDownloadProgress 与事件推送）
type VideoDownloadStatus struct {
	TaskId     string  `json:"task_id"`
	Url        string  `json:"url"`
	Filename   string  `json:"filename"`
	SavePath   string  `json:"save_path"`
	Total      int64   `json:"total"`
	Downloaded int64   `json:"downloaded"`
	SpeedBps   float64 `json:"speed_bps"`
	EtaSec     float64 `json:"eta_sec"`
	Status     string  `json:"status"` // queued / downloading / done / error / cancelled
	Error      string  `json:"error,omitempty"`
	StartTime  int64   `json:"start_time"`
	EndTime    int64   `json:"end_time,omitempty"`
	Chunks     []ChunkProgress `json:"chunks,omitempty"` // 多连接并行下载的每个分块进度
}

// ChunkProgress 单个并发连接的分块进度
type ChunkProgress struct {
	ID    int   `json:"id"`
	Start int64 `json:"start"`
	End   int64 `json:"end"`
	Done  int64 `json:"done"` // 本块已下载字节数
}

// downloadTask 内部状态
type downloadTask struct {
	mu         sync.Mutex
	status     VideoDownloadStatus
	cancel     context.CancelFunc
	httpClient *http.Client
	paused     bool     // 是否处于暂停状态
	segIndex   int      // 下一个要下载的分片索引（m3u8 断点恢复用）
	segments   []string // m3u8 的分片 URL 列表
	isM3u8     bool     // 是否 m3u8 任务
	hasTotal   bool     // 是否已估算出 total（续传时不再重算）
}

// persistedTask 磁盘持久化格式
type persistedTask struct {
	TaskId     string   `json:"task_id"`
	Url        string   `json:"url"`
	Filename   string   `json:"filename"`
	SavePath   string   `json:"save_path"`
	Total      int64    `json:"total"`
	Downloaded int64    `json:"downloaded"`
	Status     string   `json:"status"`
	SegIndex   int      `json:"seg_index"`
	IsM3u8     bool     `json:"is_m3u8"`
	Segments   []string `json:"segments,omitempty"`
	StartTime  int64    `json:"start_time"`
	ErrorMsg   string   `json:"error_msg,omitempty"`
	HasTotal   bool     `json:"has_total"`
}

var (
	downloadMu        sync.Mutex
	downloadTasks     = make(map[string]*downloadTask)
	customDirMu       sync.RWMutex
	customDownloadDir = "" // 非空则覆盖 defaultDownloadDir
)

func defaultDownloadDir() string {
	customDirMu.RLock()
	c := customDownloadDir
	customDirMu.RUnlock()
	if c != "" {
		if st, err := os.Stat(c); err == nil && st.IsDir() {
			return c
		}
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "."
	}
	return filepath.Join(home, "Downloads")
}

func sanitizeFilename(name string) string {
	if name == "" {
		name = "video"
	}
	replacer := strings.NewReplacer(
		"/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		`"`, "_", "<", "_", ">", "_", "|", "_", "\n", "_", "\r", "_", "\t", "_",
	)
	out := replacer.Replace(name)
	// 去除首尾空白与点（Windows 不允许以点结尾的目录）
	out = strings.TrimSpace(out)
	out = strings.Trim(out, ".")
	if len(out) > 180 {
		// 保留扩展名
		ext := filepath.Ext(out)
		base := strings.TrimSuffix(out, ext)
		out = base[:180-len(ext)] + ext
	}
	if out == "" {
		out = "video"
	}
	return out
}

// ensureFilenameExt 根据 URL 的扩展名补齐 filename（若 filename 没有扩展名）
func ensureFilenameExt(filename, urlStr string) string {
	if strings.Contains(filepath.Base(filename), ".") {
		// 如果扩展名是 .m3u8，替换为 .ts（分片合并后的容器）
		if strings.EqualFold(filepath.Ext(filename), ".m3u8") {
			return strings.TrimSuffix(filename, filepath.Ext(filename)) + ".ts"
		}
		return filename
	}
	// 去掉 query / fragment
	u := urlStr
	if i := strings.IndexAny(u, "?#"); i >= 0 {
		u = u[:i]
	}
	base := filepath.Base(u)
	ext := filepath.Ext(base)
	if strings.EqualFold(ext, ".m3u8") {
		// m3u8 的分片拼接后仍是有效的 MPEG-TS，保存为 .ts 即可
		ext = ".ts"
	}
	if ext == "" || len(ext) > 8 {
		ext = ".mp4"
	}
	return filename + ext
}

// StartVideoDownload 启动一个下载任务（非阻塞）
func (a *App) StartVideoDownload(req VideoDownloadReq) (*VideoDownloadStatus, error) {
	if req.TaskId == "" {
		return nil, fmt.Errorf("task_id is empty")
	}
	if strings.TrimSpace(req.Url) == "" {
		return nil, fmt.Errorf("url is empty")
	}

	// ========== 重复下载检测：同一 URL 已有任务 ==========
	downloadMu.Lock()
	var duplicateId string
	for _, existing := range downloadTasks {
		existing.mu.Lock()
		sameURL := strings.TrimSpace(existing.status.Url) == strings.TrimSpace(req.Url)
		status := existing.status.Status
		tid := existing.status.TaskId
		existing.mu.Unlock()
		if sameURL && status != "cancelled" && status != "error" {
			duplicateId = tid
			break
		}
	}
	if duplicateId != "" {
		if req.Force {
			// 强制覆盖：取消并移除旧任务，清理旧文件
			if oldTask, ok := downloadTasks[duplicateId]; ok {
				oldTask.cancel()
				// 清理临时/目标文件
				oldTask.mu.Lock()
				sp := oldTask.status.SavePath
				oldTask.mu.Unlock()
				if sp != "" {
					_ = os.Remove(sp)
					_ = os.Remove(sp + ".part")
				}
			}
			delete(downloadTasks, duplicateId)
			downloadMu.Unlock()
			a.savePersistedTasks()
		} else {
			downloadMu.Unlock()
			return nil, fmt.Errorf("duplicate_url: 该链接已有下载任务，是否覆盖？")
		}
	} else {
		downloadMu.Unlock()
	}

	// 确定保存目录
	dir := strings.TrimSpace(req.SaveDir)
	if dir == "" {
		dir = defaultDownloadDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create download dir failed: %w", err)
	}

	filename := sanitizeFilename(req.Filename)
	filename = ensureFilenameExt(filename, req.Url)
	savePath := filepath.Join(dir, filename)

	// 避免重复任务
	downloadMu.Lock()
	if t, ok := downloadTasks[req.TaskId]; ok {
		downloadMu.Unlock()
		s := t.snapshot()
		return &s, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	task := &downloadTask{
		cancel:   cancel,
		paused:   false,
		segIndex: 0,
		isM3u8:   isM3u8URL(req.Url),
		httpClient: &http.Client{
			Timeout: 4 * time.Hour, // 大文件限制 4 小时，防止无限挂起
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
			},
		},
		status: VideoDownloadStatus{
			TaskId:    req.TaskId,
			Url:       req.Url,
			Filename:  filename,
			SavePath:  savePath,
			Status:    "queued",
			StartTime: time.Now().Unix(),
		},
	}
	downloadTasks[req.TaskId] = task
	downloadMu.Unlock()

	go a.runDownload(ctx, task)

	a.savePersistedTasks()
	s := task.snapshot()
	return &s, nil
}

func (t *downloadTask) snapshot() VideoDownloadStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := t.status
	return cp
}

func (a *App) runDownload(ctx context.Context, task *downloadTask) {
	task.mu.Lock()
	task.status.Status = "downloading"
	urlStr := task.status.Url
	savePath := task.status.SavePath
	task.mu.Unlock()

	// 判断是否为 m3u8
	if isM3u8URL(urlStr) {
		a.downloadM3u8(ctx, task, urlStr, savePath)
		return
	}
	a.downloadDirect(ctx, task, urlStr, savePath)
}

// isM3u8URL 检测是否为 m3u8 播放列表
func isM3u8URL(u string) bool {
	trimmed := u
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 {
		trimmed = trimmed[:i]
	}
	return strings.Contains(strings.ToLower(trimmed), ".m3u8")
}

// resolveURL 相对路径转绝对
func resolveURL(base, ref string) string {
	if ref == "" {
		return base
	}
	if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
		return ref
	}
	baseParsed, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refParsed, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	resolved := baseParsed.ResolveReference(refParsed)
	return resolved.String()
}

// adDomainBlacklist 已知广告域名关键字（匹配 hostname 子串）
var adDomainBlacklist = []string{
	"dcs-vod.", "vod-dcs.",
	"ads.", "ad.", "advert",
	"dsp.", "doubleclick",
	"googlesyndication", "googleads",
}

// parseM3u8Segments 解析 m3u8 文件，返回所有 .ts 切片 URL
// 双层广告过滤：第一层域名黑名单，第二层 DISCONTINUITY 分组保守移除小组
func parseM3u8Segments(m3u8URL, content string) []string {
	lines := strings.Split(content, "\n")

	// 先收集所有片段行
	type segInfo struct {
		lineIdx int
		absURL  string
	}
	var allSegs []segInfo
	for i, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		allSegs = append(allSegs, segInfo{i, resolveURL(m3u8URL, line)})
	}

	// 第一层：域名黑名单
	adSet := make(map[int]bool)
	for _, s := range allSegs {
		host := extractHost(s.absURL)
		for _, d := range adDomainBlacklist {
			if strings.Contains(host, d) {
				adSet[s.lineIdx] = true
				break
			}
		}
	}
	// 黑名单命中了部分片段（非全部）→ 过滤掉广告片段
	if len(adSet) > 0 && len(adSet) < len(allSegs) {
		var result []string
		for _, s := range allSegs {
			if !adSet[s.lineIdx] {
				result = append(result, s.absURL)
			}
		}
		return result
	}

	// 第二层（兜底）：DISCONTINUITY 分组，保守移除小组（广告）
	const maxAdSegCount = 4
	const maxAdDuration = 25.0
	type group struct {
		segs     []string
		duration float64
	}
	var groups []group
	cur := group{}
	var lastDuration float64
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if line == "" {
			continue
		}
		if line == "#EXT-X-DISCONTINUITY" {
			if len(cur.segs) > 0 {
				groups = append(groups, cur)
				cur = group{}
			}
			continue
		}
		// 提取 #EXTINF 时长
		if strings.HasPrefix(line, "#EXTINF:") {
			parts := strings.TrimPrefix(line, "#EXTINF:")
			if idx := strings.Index(parts, ","); idx >= 0 {
				parts = parts[:idx]
			}
			if d, err := strconv.ParseFloat(strings.TrimSpace(parts), 64); err == nil {
				lastDuration = d
			}
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		cur.segs = append(cur.segs, resolveURL(m3u8URL, line))
		cur.duration += lastDuration
		lastDuration = 0
	}
	if len(cur.segs) > 0 {
		groups = append(groups, cur)
	}

	// 无分组或只有一组 → 全部保留
	if len(groups) <= 1 {
		var result []string
		for _, g := range groups {
			result = append(result, g.segs...)
		}
		return result
	}

	// 保守策略：只移除片段数 ≤ maxAdSegCount 且总时长 ≤ maxAdDuration 的小组
	isAdGroup := func(g group) bool {
		return len(g.segs) > 0 && len(g.segs) <= maxAdSegCount &&
			g.duration > 0 && g.duration <= maxAdDuration
	}
	// 统计非广告组数量
	contentCount := 0
	for _, g := range groups {
		if !isAdGroup(g) {
			contentCount++
		}
	}
	// 所有组都被判定为广告 → 全部保留（避免误删全部内容）
	if contentCount == 0 {
		var result []string
		for _, g := range groups {
			result = append(result, g.segs...)
		}
		return result
	}
	// 正常情况：只移除广告小组
	var result []string
	for _, g := range groups {
		if !isAdGroup(g) {
			result = append(result, g.segs...)
		}
	}
	return result
}

// extractHost 从 URL 中提取 hostname
func extractHost(rawURL string) string {
	// 快速提取 host，避免完整 url.Parse 开销
	s := rawURL
	if idx := strings.Index(s, "://"); idx >= 0 {
		s = s[idx+3:]
	}
	if idx := strings.Index(s, "/"); idx >= 0 {
		s = s[:idx]
	}
	if idx := strings.Index(s, "@"); idx >= 0 {
		s = s[idx+1:]
	}
	if idx := strings.Index(s, ":"); idx >= 0 {
		s = s[:idx]
	}
	return s
}

// httpGetText 简单的文本 GET
func httpGetText(ctx context.Context, client *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// downloadDirect 直接下载单个文件（支持断点续传、多连接并行下载、暂停）
// 核心改进：
//   - 检测服务器是否支持 Range 头；支持则开启 N 路并发连接（像 IDM 那样）
//   - 每个连接下载独立的字节区间，同时写入同一个文件的不同 offset
//   - 文件内容本身就是最终格式，不需要合并
func (a *App) downloadDirect(ctx context.Context, task *downloadTask, urlStr, savePath string) {
	tmpPath := savePath + ".part"
	defer func() {
		if task.snapshot().Status != "done" {
			// 保留 .part 文件，以便断点续传
		}
	}()

	// ========== 1) 探测文件大小 & Range 支持 ==========
	probeReq, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		probeReq, _ = http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	}
	probeReq.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	probeReq.Header.Set("Range", "bytes=0-0")
	probeResp, err := task.httpClient.Do(probeReq)
	if err == nil {
		probeResp.Body.Close()
	}

	var total int64 = -1
	supportsRange := false
	if err == nil && probeResp != nil {
		// 206 = 服务器支持 Range
		if probeResp.StatusCode == 206 {
			supportsRange = true
		}
		// 解析 Content-Length 或 Content-Range
		if cl := probeResp.ContentLength; cl > 0 && probeResp.StatusCode == 200 {
			total = cl
		}
		if cr := probeResp.Header.Get("Content-Range"); cr != "" {
			// bytes 0-0/123456  -> total = 123456
			if slash := strings.LastIndex(cr, "/"); slash >= 0 {
				if v, perr := strconv.ParseInt(strings.TrimSpace(cr[slash+1:]), 10, 64); perr == nil {
					total = v
				}
			}
		}
	}

	// ========== 2) 读取已下载字节数 ==========
	var existingBytes int64 = 0
	if fi, ferr := os.Stat(tmpPath); ferr == nil {
		existingBytes = fi.Size()
	}

	// ========== 3) 如果已有 total，则先在前端展示，避免 0/0 ==========
	if total > 0 {
		task.mu.Lock()
		task.status.Total = total
		task.status.Downloaded = existingBytes
		task.mu.Unlock()
	}

	// ========== 4) 判断走哪个分支：多连接并行 OR 单连接 ==========
	const numConnections = 6
	const minParallelSize = 2 * 1024 * 1024 // 小于 2MB 没必要并行
	remaining := total - existingBytes

	useParallel := supportsRange && total > 0 && remaining > minParallelSize

	if useParallel {
		a.downloadDirectParallel(ctx, task, urlStr, tmpPath, total, existingBytes, numConnections)
	} else {
		a.downloadDirectSingle(ctx, task, urlStr, tmpPath, total, existingBytes)
	}

	// ========== 5) 完成（如果内部函数没有处理）==========
	if task.snapshot().Status == "done" {
		if rerr := os.Rename(tmpPath, savePath); rerr != nil {
			task.setError("rename failed: " + rerr.Error())
			a.emitProgress(task)
			return
		}
		a.savePersistedTasks()
	}
}

// downloadDirectSingle 单连接下载（回退方案）
func (a *App) downloadDirectSingle(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total, existingBytes int64) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		task.setError("build request failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	if existingBytes > 0 {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingBytes))
	}

	resp, err := task.httpClient.Do(httpReq)
	if err != nil {
		task.setError("request failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		task.setError("HTTP " + strconv.Itoa(resp.StatusCode))
		a.emitProgress(task)
		return
	}

	// 更新 total（如果响应里有）
	if resp.ContentLength > 0 {
		computedTotal := resp.ContentLength + existingBytes
		if computedTotal > total {
			total = computedTotal
		}
	} else if cr := resp.Header.Get("Content-Range"); cr != "" {
		if slash := strings.LastIndex(cr, "/"); slash >= 0 {
			if v, perr := strconv.ParseInt(strings.TrimSpace(cr[slash+1:]), 10, 64); perr == nil {
				total = v
			}
		}
	}
	if total > 0 {
		task.mu.Lock()
		task.status.Total = total
		task.mu.Unlock()
	}

	// 打开文件
	flag := os.O_CREATE | os.O_WRONLY
	if existingBytes > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	out, err := os.OpenFile(tmpPath, flag, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}

	var (
		buf          = make([]byte, 128*1024)
		downloaded   = existingBytes
		lastEmit     = time.Now()
		lastBytes    = existingBytes
		emitInterval = 300 * time.Millisecond
	)

	for {
		select {
		case <-ctx.Done():
			out.Close()
			task.setCancel()
			a.emitProgress(task)
			return
		default:
		}

		if task.isPaused() {
			out.Close()
			a.emitProgress(task)
			return
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				task.setError("write file failed: " + werr.Error())
				a.emitProgress(task)
				return
			}
			downloaded += int64(n)
			task.mu.Lock()
			task.status.Downloaded = downloaded
			task.mu.Unlock()

			if time.Since(lastEmit) >= emitInterval {
				elapsed := time.Since(lastEmit).Seconds()
				var speed float64
				if elapsed > 0 {
					speed = float64(downloaded-lastBytes) / elapsed
				}
				task.mu.Lock()
				task.status.SpeedBps = speed
				if total > 0 && speed > 0 {
					task.status.EtaSec = float64(total-downloaded) / speed
				}
				task.mu.Unlock()
				lastEmit = time.Now()
				lastBytes = downloaded
				a.emitProgress(task)
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			out.Close()
			task.setError("read failed: " + rerr.Error())
			a.emitProgress(task)
			return
		}
	}

	if cerr := out.Close(); cerr != nil {
		task.setError("close file failed: " + cerr.Error())
		a.emitProgress(task)
		return
	}

	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	if task.status.Total <= 0 {
		task.status.Total = downloaded
	}
	task.status.Downloaded = downloaded
	task.mu.Unlock()
	a.emitProgress(task)
}

// downloadDirectParallel 多连接并行下载（IDM 风格）
// 将文件分为 N 个区间，每个连接独立下载一个 Range，同时写入同一文件的不同 offset
func (a *App) downloadDirectParallel(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total, existingBytes int64, numConnections int) {
	// ========== 1) 打开/创建输出文件（预分配大小以便 Seek） ==========
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	// 预分配磁盘空间（避免多次扩缩开销）
	if total > 0 {
		_ = out.Truncate(total)
	}

	// ========== 2) 计算每个连接的字节区间 ==========
	remainingStart := existingBytes
	remainingBytes := total - remainingStart
	chunkSize := remainingBytes / int64(numConnections)
	if chunkSize <= 0 {
		chunkSize = 1
	}

	type chunkRange struct {
		id     int
		start  int64
		end    int64 // inclusive
	}
	var chunks []chunkRange
	for i := 0; i < numConnections; i++ {
		start := remainingStart + int64(i)*chunkSize
		var end int64
		if i == numConnections-1 {
			end = total - 1 // 最后一块到文件尾
		} else {
			end = remainingStart + int64(i+1)*chunkSize - 1
		}
		if start < total {
			chunks = append(chunks, chunkRange{id: i, start: start, end: end})
		}
	}

	// ========== 3) 启动 N 个 worker ==========
	var (
		muProgress   sync.Mutex
		downloaded   = existingBytes
		lastEmit     = time.Now()
		lastBytes    = existingBytes
		emitInterval = 300 * time.Millisecond
		errCh        = make(chan error, numConnections)
		doneCh       = make(chan struct{})
		active       int32
		chunkDone    = make([]int64, len(chunks)) // 每个分块已下载字节
	)

	atomic.StoreInt32(&active, int32(len(chunks)))

	worker := func(chk chunkRange) {
		// 计算需要下载的字节数
		chunkTotal := chk.end - chk.start + 1
		if chunkTotal <= 0 {
			atomic.AddInt32(&active, -1)
			return
		}

		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if rerr != nil {
			errCh <- rerr
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chk.start, chk.end))

		resp, rerr := task.httpClient.Do(req)
		if rerr != nil {
			errCh <- rerr
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errCh <- fmt.Errorf("HTTP %d (chunk %d)", resp.StatusCode, chk.id)
			return
		}

		// 当前写入位置 = chk.start 起
		writeAt := chk.start
		buf := make([]byte, 256*1024) // 增大缓冲区提升吞吐

		for {
			if task.isPaused() {
				// 暂停：worker 退出，主循环检测到所有 worker 退出后退出整体
				// 下次 ResumeDownload 会从头续传（.part 文件已有部分内容）
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := out.WriteAt(buf[:n], writeAt); werr != nil {
					errCh <- werr
					return
				}
				writeAt += int64(n)
				// 更新全局下载进度 & 分块进度
				muProgress.Lock()
				downloaded += int64(n)
				chunkDone[chk.id] += int64(n)
				cur := downloaded
				// 构建分块进度快照
				chunksSnap := make([]ChunkProgress, len(chunks))
				for i := range chunks {
					chunksSnap[i] = ChunkProgress{
						ID:    chunks[i].id,
						Start: chunks[i].start,
						End:   chunks[i].end,
						Done:  chunkDone[i],
					}
				}
				muProgress.Unlock()

				// 节流推送
				if time.Since(lastEmit) >= emitInterval {
					elapsed := time.Since(lastEmit).Seconds()
					var speed float64
					if elapsed > 0 {
						speed = float64(cur-lastBytes) / elapsed
					}
					task.mu.Lock()
					task.status.Downloaded = cur
					task.status.SpeedBps = speed
					task.status.Chunks = chunksSnap
					if total > 0 && speed > 0 {
						task.status.EtaSec = float64(total-cur) / speed
					}
					task.mu.Unlock()
					lastEmit = time.Now()
					lastBytes = cur
					a.emitProgress(task)
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					break
				}
				errCh <- rerr
				return
			}
		}

		atomic.AddInt32(&active, -1)
		if atomic.LoadInt32(&active) == 0 {
			close(doneCh)
		}
	}

	for _, chk := range chunks {
		go worker(chk)
	}

	// ========== 4) 主循环：等待完成 / 取消 / 暂停 / 错误 ==========
	for {
		select {
		case <-ctx.Done():
			out.Close()
			task.setCancel()
			a.emitProgress(task)
			return
		case <-doneCh:
			// 所有 chunk 完成
			if cerr := out.Close(); cerr != nil {
				task.setError("close file failed: " + cerr.Error())
				a.emitProgress(task)
				return
			}
			task.mu.Lock()
			task.status.Status = "done"
			task.status.EndTime = time.Now().Unix()
			task.status.SpeedBps = 0
			task.status.EtaSec = 0
			task.status.Total = total
			task.status.Downloaded = downloaded
			task.mu.Unlock()
			a.emitProgress(task)
			return
		case werr := <-errCh:
			out.Close()
			task.setError("parallel download failed: " + werr.Error())
			a.emitProgress(task)
			return
		default:
			// 周期性检测暂停
			if task.isPaused() {
				// 等待 worker 自然退出后再关闭文件
				out.Close()
				a.emitProgress(task)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// downloadM3u8 解析并下载 m3u8 的所有分片，合并为一个 .ts 文件
// 核心改进：
//   1) 真正的 N 路并发下载（类似 IDM 思路），N 个 worker 并发下载分片
//   2) 续传时保持 total 估算不变，避免进度条跳变
//   3) 按分片索引顺序写入磁盘，支持任意顺序的并发返回
func (a *App) downloadM3u8(ctx context.Context, task *downloadTask, m3u8URL, savePath string) {
	tmpPath := savePath + ".part"
	const numWorkers = 5
	const totalLockAfter = 8 // 下载 8 个分片后锁定 total 估算，之后不再变动

	// ========== 1) 解析 m3u8（或使用缓存） ==========
	var segments []string
	task.mu.Lock()
	segments = task.segments
	hasCachedSegs := len(segments) > 0
	isResume := task.hasTotal && task.segIndex > 0
	task.mu.Unlock()

	if !hasCachedSegs {
		m3u8Text, err := httpGetText(ctx, task.httpClient, m3u8URL)
		if err != nil {
			task.setError("fetch m3u8 failed: " + err.Error())
			a.emitProgress(task)
			return
		}
		segments = parseM3u8Segments(m3u8URL, m3u8Text)

		// 若首个 segment 也是 m3u8（多级 playlist），再深入一层
		if len(segments) > 0 && isM3u8URL(segments[0]) {
			subText, err := httpGetText(ctx, task.httpClient, segments[0])
			if err != nil {
				task.setError("fetch sub m3u8 failed: " + err.Error())
				a.emitProgress(task)
				return
			}
			segments = parseM3u8Segments(segments[0], subText)
		}
		if len(segments) == 0 {
			task.setError("no segments found in m3u8")
			a.emitProgress(task)
			return
		}
		task.mu.Lock()
		task.segments = segments
		task.mu.Unlock()
	}

	totalSegs := len(segments)

	// ========== 2) 读取已下载字节数 / 起始分片索引 ==========
	var existingBytes int64 = 0
	if fi, err := os.Stat(tmpPath); err == nil {
		existingBytes = fi.Size()
	}

	startIdx := task.nextSegIdx()
	if startIdx >= totalSegs {
		startIdx = 0
	}

	// ========== 3) 打开文件（新建/追加） ==========
	flag := os.O_CREATE | os.O_WRONLY
	if existingBytes > 0 && startIdx > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
		existingBytes = 0
		startIdx = 0
		isResume = false
	}
	out, err := os.OpenFile(tmpPath, flag, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}

	// ========== 4) N 路并发下载 + 顺序写入 ==========
	type segResult struct {
		idx  int
		data []byte
		err  error
	}

	var (
		downloadedBytes = existingBytes
		lastEmit        = time.Now()
		lastBytes       = existingBytes
		emitInterval    = 300 * time.Millisecond
		// total 估算策略：
		//   - 续传：沿用已保存的 total（isResume=true），从不动它
		//   - 新下载：前 totalLockAfter 个分片里，用平均值估算 total；达到阈值后 total 锁定，
		//     后续不再更新 total，避免进度条数字跳动
		segSumBytes   float64 = 0
		segCountReady int64   = 0
		totalLocked   bool    = isResume // 续传时认为 total 已锁定
	)

	// 下载任务队列（每个 worker 从中取索引）
	idxCh := make(chan int, totalSegs)
	resultCh := make(chan segResult, totalSegs)
	// 取消信号：让所有 worker 停止
	stopCh := make(chan struct{})

	// worker 函数：并发下载分片
	worker := func() {
		for {
			select {
			case idx, ok := <-idxCh:
				if !ok {
					return
				}
				// 检查取消 / 暂停
				if task.isPaused() {
					resultCh <- segResult{idx: idx, err: nil, data: nil} // 空数据表示被暂停
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-stopCh:
					return
				default:
				}

				seg := segments[idx]
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, seg, nil)
				if err != nil {
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d request: %w", idx+1, err)}
					continue
				}
				req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
				resp, err := task.httpClient.Do(req)
				if err != nil {
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d do: %w", idx+1, err)}
					continue
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					resp.Body.Close()
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d HTTP %d", idx+1, resp.StatusCode)}
					continue
				}
				data, rerr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if rerr != nil {
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d read: %w", idx+1, rerr)}
					continue
				}
				resultCh <- segResult{idx: idx, data: data, err: nil}

			case <-ctx.Done():
				return
			case <-stopCh:
				return
			}
		}
	}

	// 启动 N 个 worker
	for w := 0; w < numWorkers; w++ {
		go worker()
	}

	// 把分片索引发给 workers（从 startIdx 开始）
	go func() {
		for i := startIdx; i < totalSegs; i++ {
			select {
			case idxCh <- i:
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			}
		}
		// 注意：不关闭 idxCh，避免 worker 收到已关闭的 channel panic
		// 由主循环根据已处理数量判断是否结束
	}()

	// 主循环：顺序写入
	// 用 pending map 暂存乱序到达的分片数据
	// 下一个期望写入的分片索引 = nextExpected
	pending := make(map[int][]byte)
	nextExpected := startIdx
	remaining := totalSegs - startIdx
	processed := 0

	for processed < remaining {
		select {
		case <-ctx.Done():
			// 取消：清理并退出
			close(stopCh)
			out.Close()
			task.setCancel()
			a.emitProgress(task)
			return
		case result, ok := <-resultCh:
			if !ok {
				close(stopCh)
				out.Close()
				task.setError("result channel closed unexpectedly")
				a.emitProgress(task)
				return
			}

			// 检查暂停：任意分片下载时检测到暂停就终止本轮
			if task.isPaused() {
				close(stopCh)
				task.setSegIdx(nextExpected)
				out.Close()
				a.emitProgress(task)
				return
			}

			if result.err != nil {
				close(stopCh)
				out.Close()
				task.setError(result.err.Error())
				a.emitProgress(task)
				return
			}

			// 结果到达；要么立即写入（若正好是 nextExpected），要么暂存
			if result.idx == nextExpected {
				if result.data != nil && len(result.data) > 0 {
					if _, werr := out.Write(result.data); werr != nil {
						close(stopCh)
						out.Close()
						task.setError("write segment failed: " + werr.Error())
						a.emitProgress(task)
						return
					}
					segSize := float64(len(result.data))
					downloadedBytes += int64(segSize)
					// 仅在 total 未锁定时累计用于估算
					if !totalLocked {
						segSumBytes += segSize
						segCountReady++
					}
				}
				nextExpected++
				processed++

				// 尝试把 pending 中连续的分片也写出去
				for {
					if data, exists := pending[nextExpected]; exists {
						if data != nil && len(data) > 0 {
							if _, werr := out.Write(data); werr != nil {
								close(stopCh)
								out.Close()
								task.setError("write segment failed: " + werr.Error())
								a.emitProgress(task)
								return
							}
							segSize := float64(len(data))
							downloadedBytes += int64(segSize)
							if !totalLocked {
								segSumBytes += segSize
								segCountReady++
							}
						}
						delete(pending, nextExpected)
						nextExpected++
						processed++
					} else {
						break
					}
				}

				// total 估算逻辑：
				//   1. 若已锁定，绝对不动 total
				//   2. 否则累计到 totalLockAfter 个分片后锁定；锁定前也可以粗略给用户展示
				if !totalLocked && segCountReady >= totalLockAfter {
					avgSeg := segSumBytes / float64(segCountReady)
					estTotal := int64(avgSeg * float64(totalSegs))
					if estTotal < downloadedBytes {
						estTotal = downloadedBytes
					}
					task.mu.Lock()
					task.status.Total = estTotal
					task.hasTotal = true
					task.mu.Unlock()
					totalLocked = true // 锁定后不再更新 total
				} else if !totalLocked && segCountReady >= 3 {
					// 锁定前粗略估算（每 3 个分片更新一次，避免 1 个分片时 total 变化过大）
					avgSeg := segSumBytes / float64(segCountReady)
					estTotal := int64(avgSeg * float64(totalSegs))
					if estTotal < downloadedBytes {
						estTotal = downloadedBytes
					}
					task.mu.Lock()
					task.status.Total = estTotal
					task.hasTotal = true
					task.mu.Unlock()
				}

				// 更新 downloaded
				task.mu.Lock()
				task.status.Downloaded = downloadedBytes
				task.segIndex = nextExpected
				task.mu.Unlock()

				// 节流推送进度
				if time.Since(lastEmit) >= emitInterval {
					elapsed := time.Since(lastEmit).Seconds()
					var speed float64
					if elapsed > 0 {
						speed = float64(downloadedBytes-lastBytes) / elapsed
					}
					// 续传场景也可以计算 eta（使用已保存的 total）
					var eta float64
					task.mu.Lock()
					totalForEta := task.status.Total
					task.status.SpeedBps = speed
					if totalForEta > 0 && speed > 0 && totalForEta > downloadedBytes {
						eta = float64(totalForEta-downloadedBytes) / speed
					}
					task.status.EtaSec = eta
					task.mu.Unlock()
					lastEmit = time.Now()
					lastBytes = downloadedBytes
					a.emitProgress(task)
				}

				// 定期持久化（每 20 个分片）
				if processed%20 == 0 {
					a.savePersistedTasks()
				}
			} else {
				// 乱序到达的分片：暂存到 pending
				if result.data != nil {
					pending[result.idx] = result.data
				} else {
					// 空结果 = 被暂停的分片标记：主循环终止
					close(stopCh)
					task.setSegIdx(nextExpected)
					out.Close()
					a.emitProgress(task)
					return
				}
			}
		}
	}

	close(stopCh)

	// ========== 5) 完成 ==========
	if cerr := out.Close(); cerr != nil {
		task.setError("close file failed: " + cerr.Error())
		a.emitProgress(task)
		return
	}
	if rerr := os.Rename(tmpPath, savePath); rerr != nil {
		task.setError("rename failed: " + rerr.Error())
		a.emitProgress(task)
		return
	}

	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	task.status.Total = downloadedBytes
	task.status.Downloaded = downloadedBytes
	task.mu.Unlock()
	a.emitProgress(task)
	a.savePersistedTasks()
}

func (t *downloadTask) setError(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.Status = "error"
	t.status.Error = msg
	t.status.EndTime = time.Now().Unix()
}
func (t *downloadTask) setCancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.Status = "cancelled"
	t.status.EndTime = time.Now().Unix()
}

// isPaused 原子读取暂停状态
func (t *downloadTask) isPaused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.paused
}

// setPaused 更新暂停状态并同步到对外显示
func (t *downloadTask) setPaused(val bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.paused = val
	if val {
		t.status.Status = "paused"
	} else {
		t.status.Status = "downloading"
	}
}

// nextSegIdx 原子读取下一个分片索引
func (t *downloadTask) nextSegIdx() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.segIndex
}

func (t *downloadTask) setSegIdx(i int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.segIndex = i
}

func (a *App) emitProgress(task *downloadTask) {
	s := task.snapshot()
	a.app.Event.Emit("download:progress", s)
}

// GetDownloadProgress 查询单个任务状态
func (a *App) GetDownloadProgress(taskId string) (*VideoDownloadStatus, error) {
	downloadMu.Lock()
	t, ok := downloadTasks[taskId]
	downloadMu.Unlock()
	if !ok {
		return nil, fmt.Errorf("task not found: %s", taskId)
	}
	s := t.snapshot()
	return &s, nil
}

// ListDownloads 返回所有任务快照
func (a *App) ListDownloads() []VideoDownloadStatus {
	downloadMu.Lock()
	defer downloadMu.Unlock()
	out := make([]VideoDownloadStatus, 0, len(downloadTasks))
	for _, t := range downloadTasks {
		out = append(out, t.snapshot())
	}
	return out
}

// CancelDownload 取消一个下载任务
func (a *App) CancelDownload(taskId string) bool {
	downloadMu.Lock()
	t, ok := downloadTasks[taskId]
	downloadMu.Unlock()
	if !ok {
		return false
	}
	if t.cancel != nil {
		t.cancel()
	}
	a.savePersistedTasks()
	return true
}

// ======================== 任务持久化 ========================

func (a *App) persistPath() string {
	return filepath.Join(a.getDataDir(), "downloads.json")
}

func (a *App) toPersisted(t *downloadTask) persistedTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	return persistedTask{
		TaskId:     t.status.TaskId,
		Url:        t.status.Url,
		Filename:   t.status.Filename,
		SavePath:   t.status.SavePath,
		Total:      t.status.Total,
		Downloaded: t.status.Downloaded,
		Status:     t.status.Status,
		SegIndex:   t.segIndex,
		IsM3u8:     t.isM3u8,
		Segments:   t.segments,
		StartTime:  t.status.StartTime,
		ErrorMsg:   t.status.Error,
		HasTotal:   t.hasTotal,
	}
}

// savePersistedTasks 把当前所有下载任务写入磁盘 JSON
func (a *App) savePersistedTasks() {
	downloadMu.Lock()
	items := make([]persistedTask, 0, len(downloadTasks))
	for _, t := range downloadTasks {
		items = append(items, a.toPersisted(t))
	}
	downloadMu.Unlock()

	data, err := json.Marshal(items)
	if err != nil {
		return
	}
	_ = os.WriteFile(a.persistPath(), data, 0644)
}

// loadPersistedTasks 从磁盘 JSON 恢复下载任务
// 注意：仅重建任务对象，不自动继续下载（用户手动点击「继续」）
func (a *App) loadPersistedTasks() {
	data, err := os.ReadFile(a.persistPath())
	if err != nil {
		return
	}
	var items []persistedTask
	if err := json.Unmarshal(data, &items); err != nil {
		return
	}

	downloadMu.Lock()
	defer downloadMu.Unlock()
	for _, it := range items {
		// 已完成/已取消且临时文件不存在的任务不必恢复
		tmpPath := it.SavePath + ".part"
		_, tmpExists := os.Stat(tmpPath)
		isActive := it.Status == "downloading" || it.Status == "queued" || it.Status == "paused" || it.Status == "error"
		if !isActive && tmpExists != nil {
			continue
		}

		task := &downloadTask{
			status: VideoDownloadStatus{
				TaskId:     it.TaskId,
				Url:        it.Url,
				Filename:   it.Filename,
				SavePath:   it.SavePath,
				Total:      it.Total,
				Downloaded: it.Downloaded,
				Status:     "paused", // 重新启动后统一标记为 paused，由用户决定是否继续
				StartTime:  it.StartTime,
				Error:      it.ErrorMsg,
			},
			segIndex: it.SegIndex,
			segments: it.Segments,
			isM3u8:   it.IsM3u8,
			hasTotal: it.HasTotal,
			paused:   true,
			httpClient: &http.Client{
				Timeout: 4 * time.Hour, // 大文件限制 4 小时，防止无限挂起
				Transport: &http.Transport{
					MaxIdleConns:        10,
					IdleConnTimeout:     60 * time.Second,
					TLSHandshakeTimeout: 15 * time.Second,
				},
			},
		}
		downloadTasks[it.TaskId] = task
	}
}

// ======================== 下载控制 ========================

// PauseDownload 暂停一个下载任务
func (a *App) PauseDownload(taskId string) bool {
	downloadMu.Lock()
	t, ok := downloadTasks[taskId]
	downloadMu.Unlock()
	if !ok {
		return false
	}
	s := t.snapshot()
	if s.Status != "downloading" && s.Status != "queued" {
		return false
	}
	t.setPaused(true)
	a.emitProgress(t)
	a.savePersistedTasks()
	return true
}

// ResumeDownload 恢复一个暂停的下载任务
func (a *App) ResumeDownload(taskId string) bool {
	downloadMu.Lock()
	t, ok := downloadTasks[taskId]
	downloadMu.Unlock()
	if !ok {
		return false
	}
	s := t.snapshot()
	if s.Status != "paused" {
		return false
	}
	// 更新状态 + 启动新的 goroutine 继续下载
	t.setPaused(false)
	// 为新的下载周期创建新的 context（因为之前的可能已被 cancel 关联）
	ctx, cancel := context.WithCancel(context.Background())
	t.mu.Lock()
	t.cancel = cancel
	t.mu.Unlock()

	if t.isM3u8 {
		go a.downloadM3u8(ctx, t, t.status.Url, t.status.SavePath)
	} else {
		go a.downloadDirect(ctx, t, t.status.Url, t.status.SavePath)
	}
	a.emitProgress(t)
	return true
}

// RemoveDownload 从列表移除任务记录（不会删除已下载文件）
func (a *App) RemoveDownload(taskId string) bool {
	downloadMu.Lock()
	if _, ok := downloadTasks[taskId]; ok {
		if t := downloadTasks[taskId]; t != nil && t.cancel != nil {
			t.cancel()
		}
		delete(downloadTasks, taskId)
		downloadMu.Unlock()
		a.savePersistedTasks()
		return true
	}
	downloadMu.Unlock()
	return false
}

// GetDownloadDir 返回默认下载目录
func (a *App) GetDownloadDir() string {
	return defaultDownloadDir()
}

// SetDownloadDir 设置自定义下载目录（空字符串则重置为默认）
func (a *App) SetDownloadDir(dir string) string {
	dir = strings.TrimSpace(dir)
	customDirMu.Lock()
	defer customDirMu.Unlock()
	if dir == "" {
		customDownloadDir = ""
	} else {
		// 尝试创建目录
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return defaultDownloadDir()
			}
		}
		customDownloadDir = dir
	}
	// 持久化到数据库
	_ = db.SetSetting("download_dir", customDownloadDir)
	return defaultDownloadDir()
}

// OpenFileInExplorer 在系统文件管理器中打开该文件（定位到文件）
func (a *App) OpenFileInExplorer(path string) bool {
	if path == "" {
		return false
	}
	if _, err := os.Stat(path); err != nil {
		return false
	}

	// Windows: 使用 explorer /select, 定位到具体文件
	if goruntime.GOOS == "windows" {
		cmd := exec.Command("explorer", "/select,", path)
		if err := cmd.Start(); err == nil {
			return true
		}
		// 回退：打开目录
		dir := filepath.Dir(path)
		cmd = exec.Command("explorer", dir)
		if err := cmd.Start(); err == nil {
			return true
		}
		return false
	}

	// 其他平台：打开所在目录
	dir := filepath.Dir(path)
	cmd := exec.Command("xdg-open", dir)
	cmd.Start()
	return true
}

// ======================== Migrate ========================

func (a *App) MigrateOldData(apiUrl, sourceKey string) error {
	src, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return fmt.Errorf("source not found: %s", sourceKey)
	}

	engine := collect.NewEngine(sourceKey,
		func(msg string) {
			a.app.Event.Emit("collect:log", map[string]interface{}{
				"source_key": sourceKey,
				"message":    msg,
			})
		},
		func(current, total int) {
			a.app.Event.Emit("collect:progress", map[string]interface{}{
				"source_key": sourceKey,
				"current":    current,
				"total":      total,
			})
		})

	fetcher := collect.FetchAll
	_ = fetcher
	_ = src

	firstPage, err := collect.FetchPage(apiUrl, 1)
	if err != nil {
		return err
	}

	a.app.Event.Emit("collect:progress", map[string]interface{}{
		"source_key": sourceKey,
		"current":    1,
		"total":      firstPage.Pagecount.Int(),
	})

	_ = engine

	for _, v := range firstPage.List {
		if v.VodName == "" || v.TypeName == "" {
			continue
		}
		v.VodContent = collect.CleanHTML(v.VodContent)
		v.VodContent = collect.CompressTextField(v.VodContent)
		v.VodActor = collect.CleanHTML(v.VodActor)
		v.VodActor = collect.CompressTextField(v.VodActor)
		v.VodDirector = collect.CleanHTML(v.VodDirector)
		v.VodDirector = collect.CompressTextField(v.VodDirector)
		v.VodPlayUrl = collect.CompressTextField(v.VodPlayUrl)
		v.VodDownUrl = collect.CompressTextField(v.VodDownUrl)
	}

	if err := db.UpsertVideos(sourceKey, firstPage.List); err != nil {
		return err
	}
	for _, v := range firstPage.List {
		db.InsTypeIfNotExist(sourceKey, v.TypeId, v.TypeName)
	}

	a.app.Event.Emit("collect:done", map[string]interface{}{
		"source_key": sourceKey,
		"count":      len(firstPage.List),
	})

	return nil
}

func (a *App) ServiceShutdown() error {
	// 记录退出时间（下次启动"补采"可使用）
	handler.TouchLastExit()
	// 停止调度器的后台循环
	s := handler.GetScheduler(context.Background())
	s.Stop()

	// 停止豆瓣调度器
	if a.doubanScheduler != nil {
		a.doubanScheduler.Stop()
	}

	// 停止磁力链接调度器
	if a.ciligouScheduler != nil {
		a.ciligouScheduler.Stop()
	}

	applog.Info("应用正常退出")
	// 关闭日志文件
	applog.Default().Close()
	db.Close()

	return nil
}

// IsSchedulerRunning 检查是否有调度任务正在运行（采集调度或豆瓣调度）
func (a *App) IsSchedulerRunning() bool {
	s := handler.GetScheduler(context.Background())
	if s.IsRunning() {
		return true
	}
	// 检查是否有活跃的采集引擎
	sources, err := handler.GetAllSources()
	if err == nil {
		for _, src := range sources {
			status := handler.GetCollectStatus(src.SourceKey)
			if status != nil && status.Running {
				return true
			}
		}
	}
	if a.doubanScheduler != nil && a.doubanScheduler.IsRunning() {
		return true
	}
	if a.ciligouScheduler != nil && a.ciligouScheduler.IsRunning() {
		return true
	}
	return false
}

// ConfirmShutdown 确认退出：触发应用退出，清理由 ServiceShutdown 统一处理
func (a *App) ConfirmShutdown() {
	if a.app != nil {
		a.app.Quit()
	}
}

// GracefulShutdown 优雅关闭：等待所有采集任务完成当前页后退出
func (a *App) GracefulShutdown() {
	s := handler.GetScheduler(context.Background())
	s.Stop()
	if a.doubanScheduler != nil {
		a.doubanScheduler.Stop()
	}
	if a.ciligouScheduler != nil {
		a.ciligouScheduler.Stop()
	}
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// ======================== Cache Management ========================

// CacheInfo 缓存信息
type CacheInfo struct {
	LocalStorageBytes int64  `json:"local_storage_bytes"`
	IndexedDBBytes    int64  `json:"indexed_db_bytes"`
	DatabaseBytes     int64  `json:"database_bytes"`
	DatabasePath      string `json:"database_path"`
	DiskCacheDir      string `json:"disk_cache_dir"`
	DiskCacheBytes    int64  `json:"disk_cache_bytes"`
	LogFileBytes      int64  `json:"log_file_bytes"`
	LogFilePath       string `json:"log_file_path"`
}

// getDirSize 递归计算目录大小
func getDirSize(path string) int64 {
	var size int64
	filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size
}

// GetCacheInfo 获取缓存信息
func (a *App) GetCacheInfo() (*CacheInfo, error) {
	info := &CacheInfo{}

	// 数据库文件大小
	dataDir := a.getDataDir()
	dbPath := filepath.Join(dataDir, "cczj_video.db")
	if fi, err := os.Stat(dbPath); err == nil {
		info.DatabaseBytes = fi.Size()
		// 也包含 WAL 和 SHM 文件
		for _, ext := range []string{"-wal", "-shm"} {
			if fi2, err := os.Stat(dbPath + ext); err == nil {
				info.DatabaseBytes += fi2.Size()
			}
		}
	}
	info.DatabasePath = dbPath

	// 磁盘缓存目录（TsCache 下载的 TS 片段）
	diskCacheDir := filepath.Join(dataDir, "ts_cache")
	if fi, err := os.Stat(diskCacheDir); err == nil && fi.IsDir() {
		info.DiskCacheBytes = getDirSize(diskCacheDir)
	}
	info.DiskCacheDir = diskCacheDir

	// 日志文件
	logDir := filepath.Join(dataDir, "logs")
	if fi, err := os.Stat(logDir); err == nil && fi.IsDir() {
		info.LogFileBytes = getDirSize(logDir)
	}
	info.LogFilePath = logDir

	return info, nil
}

// ClearCacheReq 清除缓存请求
type ClearCacheReq struct {
	Type string `json:"type"` // "database" | "disk_cache" | "logs" | "all"
}

// ClearCache 清除指定类型的缓存
func (a *App) ClearCache(req ClearCacheReq) (bool, error) {
	dataDir := a.getDataDir()

	doClear := func(t string) error {
		switch t {
		case "disk_cache":
			diskCacheDir := filepath.Join(dataDir, "ts_cache")
			return os.RemoveAll(diskCacheDir)
		case "logs":
			logDir := filepath.Join(dataDir, "logs")
			return os.RemoveAll(logDir)
		case "database":
			// 数据库文件不能直接删除（正在使用），标记需要下次重启清理
			return fmt.Errorf("数据库文件正在使用中，请通过「重置数据库」功能操作")
		case "all":
			os.RemoveAll(filepath.Join(dataDir, "ts_cache"))
			os.RemoveAll(filepath.Join(dataDir, "logs"))
			return nil
		}
		return fmt.Errorf("未知缓存类型: %s", t)
	}

	if err := doClear(req.Type); err != nil {
		return false, err
	}
	return true, nil
}

// ProxyImage 代理获取远程图片，绕过 CORS 限制
// 返回 base64 编码的图片数据
func (a *App) ProxyImage(urlStr string) (string, error) {
	if strings.TrimSpace(urlStr) == "" {
		return "", fmt.Errorf("url is empty")
	}
	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("unsupported scheme: %s", u.Scheme)
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, urlStr, nil)
	if err != nil {
		return "", fmt.Errorf("build request failed: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video/1.0")
	req.Header.Set("Accept", "image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "identity")

	resp, err := imageProxyClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch image failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	contentType := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(contentType, "image/") {
		return "", fmt.Errorf("not an image: %s", contentType)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read image failed: %w", err)
	}

	base64Data := base64.StdEncoding.EncodeToString(data)
	return "data:" + contentType + ";base64," + base64Data, nil
}
