package service

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	"cczjVideo/app/model"
	"cczjVideo/app/updater"
)

// ======================== 诊断台 ========================
//
// 设置页「诊断」分组的数据源。GetDiagnostics 全部只读：不写库、不改配置、
// 不触发抓取。唯一主动发请求的是 ProbeSources，而那本来就是采集源该应答的
// 列表接口，等价于手动点一次「采集第 1 页」。

// appStartedAt 用于计算运行时长，进程启动时定一次即可。
var appStartedAt = time.Now()

const (
	// settingDoubanIntervalMinutes 存豆瓣补全轮询间隔，缺省 30 分钟。
	settingDoubanIntervalMinutes = "douban_interval_minutes"
	// settingLogKeepDays 存日志保留天数，缺省沿用 applog 内置的 30 天。
	settingLogKeepDays = "log_keep_days"

	defaultDoubanIntervalMinutes = 30
	minDoubanIntervalMinutes     = 1

	sourceProbeConcurrency = 4
	sourceProbeTimeout     = 10 * time.Second
	sourceProbeSampleBytes = 4096
)

// DiagEnv 是构建与运行环境，出问题时报备用的最小集合。
type DiagEnv struct {
	AppVersion      string         `json:"app_version"`
	InstalledMarker string         `json:"installed_marker"`
	WailsVersion    string         `json:"wails_version"`
	GoVersion       string         `json:"go_version"`
	GOOS            string         `json:"goos"`
	GOARCH          string         `json:"goarch"`
	NumCPU          int            `json:"num_cpu"`
	Executable      string         `json:"executable"`
	DataDir         string         `json:"data_dir"`
	StartedAtUnix   int64          `json:"started_at_unix"`
	UptimeSeconds   int64          `json:"uptime_seconds"`
	Goroutines      int            `json:"goroutines"`
	HeapAllocKB     uint64         `json:"heap_alloc_kb"`
	SysMemKB        uint64         `json:"sys_mem_kb"`
	NumGC           uint32         `json:"num_gc"`
	BackgroundTasks map[string]int `json:"background_tasks"`
}

// DiagStorage 是各块磁盘占用的汇总。
type DiagStorage struct {
	DatabasePath   string `json:"database_path"`
	DatabaseBytes  int64  `json:"database_bytes"`
	DiskCacheDir   string `json:"disk_cache_dir"`
	DiskCacheBytes int64  `json:"disk_cache_bytes"`
	LogDir         string `json:"log_dir"`
	LogBytes       int64  `json:"log_bytes"`
	LogFiles       int    `json:"log_files"`
	LogKeepDays    int    `json:"log_keep_days"`
}

// DiagDouban 是豆瓣补全的调度节奏、反爬状态与数据完整性。
type DiagDouban struct {
	Running             bool                      `json:"running"`
	Updating            bool                      `json:"updating"`
	IntervalMinutes     int                       `json:"interval_minutes"`
	LastTickUnix        int64                     `json:"last_tick_unix"`
	NextTickUnix        int64                     `json:"next_tick_unix"`
	SilentRemainingSec  int64                     `json:"silent_remaining_sec"`
	SilentStrikes       int                       `json:"silent_strikes"`
	ChallengeAtUnix     int64                     `json:"challenge_at_unix"`
	ChallengeDifficulty int                       `json:"challenge_difficulty"`
	ChallengeElapsedMs  int64                     `json:"challenge_elapsed_ms"`
	ChallengeSolved     bool                      `json:"challenge_solved"`
	Health              db.DoubanHealth           `json:"health"`
	Duplicates          []db.DoubanDuplicateGroup `json:"duplicates"`
}

// DiagCollect 是采集调度器快照。
type DiagCollect struct {
	Running          bool  `json:"running"`
	Background       bool  `json:"background"`
	EverySeconds     int   `json:"every_seconds"`
	SourceGapSeconds int   `json:"source_gap_seconds"`
	PageGapSeconds   int   `json:"page_gap_seconds"`
	LastRunUnix      int64 `json:"last_run_unix"`
	LastExitUnix     int64 `json:"last_exit_unix"`
	ActiveCollects   int   `json:"active_collects"`
	ScheduledSources int   `json:"scheduled_sources"`
}

// DiagSourceRow 是单个采集源的目录统计。
type DiagSourceRow struct {
	SourceKey  string `json:"source_key"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	APIUrl     string `json:"api_url"`
	VideoCount int    `json:"video_count"`
}

// Diagnostics 是诊断页一次拉全的载荷。Notes 收集分项失败原因，
// 单项取不到时整页仍然可用，界面对应区块标灰，而不是报「诊断失败」。
type Diagnostics struct {
	Env     DiagEnv         `json:"env"`
	Storage DiagStorage     `json:"storage"`
	Douban  DiagDouban      `json:"douban"`
	Collect DiagCollect     `json:"collect"`
	Sources []DiagSourceRow `json:"sources"`
	Tables  []db.TableStat  `json:"tables"`
	Notes   []string        `json:"notes"`
}

// GetDiagnostics 汇总环境、存储、豆瓣、采集调度、源统计与表行数。
func (a *App) GetDiagnostics() (*Diagnostics, error) {
	d := &Diagnostics{Notes: []string{}}
	note := func(format string, args ...any) {
		d.Notes = append(d.Notes, fmt.Sprintf(format, args...))
	}

	d.Env = a.diagnosticsEnv()
	if marker, err := os.ReadFile(filepath.Join(a.getDataDir(), ".installed_version")); err == nil {
		d.Env.InstalledMarker = strings.TrimSpace(string(marker))
	}

	if info, err := a.cache.GetInfo(); err == nil {
		d.Storage = DiagStorage{
			DatabasePath:   info.DatabasePath,
			DatabaseBytes:  info.DatabaseBytes,
			DiskCacheDir:   info.DiskCacheDir,
			DiskCacheBytes: info.DiskCacheBytes,
			LogDir:         info.LogFilePath,
			LogBytes:       info.LogFileBytes,
		}
	} else {
		note("存储占用统计失败: %v", err)
	}
	if stats, err := a.GetLogStats(); err == nil {
		d.Storage.LogFiles = stats.Files
		d.Storage.LogKeepDays = stats.KeepDays
		if stats.Dir != "" {
			d.Storage.LogDir = stats.Dir
		}
	} else {
		note("日志统计失败: %v", err)
	}

	if tables, err := db.TableStats(); err == nil {
		d.Tables = tables
	} else {
		note("表行数统计失败: %v", err)
	}
	d.Sources = a.diagnosticsSources(note)
	d.Douban = a.diagnosticsDouban(note)
	d.Collect = a.diagnosticsCollect(note)

	return d, nil
}

func (a *App) diagnosticsEnv() DiagEnv {
	execPath, err := os.Executable()
	if err != nil {
		execPath = ""
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	env := DiagEnv{
		AppVersion:    updater.EffectiveVersion(),
		WailsVersion:  wailsVersionFromBuildInfo(),
		GoVersion:     runtime.Version(),
		GOOS:          runtime.GOOS,
		GOARCH:        runtime.GOARCH,
		NumCPU:        runtime.NumCPU(),
		Executable:    execPath,
		DataDir:       a.getDataDir(),
		StartedAtUnix: appStartedAt.Unix(),
		UptimeSeconds: int64(time.Since(appStartedAt).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		HeapAllocKB:   mem.HeapAlloc / 1024,
		SysMemKB:      mem.Sys / 1024,
		NumGC:         mem.NumGC,
	}
	if a.background != nil {
		env.BackgroundTasks = a.background.ActiveTasks()
	}
	return env
}

func (a *App) diagnosticsSources(note func(string, ...any)) []DiagSourceRow {
	sources, err := db.GetAllSources()
	if err != nil {
		note("采集源列表读取失败: %v", err)
		return nil
	}
	counts := map[string]int{}
	if stats, statErr := db.GetSourceStats(); statErr == nil {
		for _, stat := range stats {
			counts[stat.SourceKey] = stat.VideoCount
		}
	} else {
		note("采集源目录统计失败: %v", statErr)
	}
	rows := make([]DiagSourceRow, 0, len(sources))
	for _, src := range sources {
		rows = append(rows, DiagSourceRow{
			SourceKey:  src.SourceKey,
			Name:       src.Name,
			Enabled:    src.Enabled == 1,
			APIUrl:     src.ApiUrl,
			VideoCount: counts[src.SourceKey],
		})
	}
	return rows
}

func (a *App) diagnosticsDouban(note func(string, ...any)) DiagDouban {
	interval, running, lastTick, nextTick := a.schedulers.DoubanSchedule()
	_, updating := a.schedulers.DoubanStatus()
	silentRemaining, silentStrikes := douban.AntiCrawlState()
	challengeAt, challengeDiff, challengeElapsed, challengeOK := douban.LastChallenge()
	snap := DiagDouban{
		Running:             running,
		Updating:            updating,
		IntervalMinutes:     int(interval.Minutes()),
		LastTickUnix:        unixOrZero(lastTick),
		NextTickUnix:        unixOrZero(nextTick),
		SilentRemainingSec:  int64(silentRemaining.Seconds()),
		SilentStrikes:       silentStrikes,
		ChallengeAtUnix:     unixOrZero(challengeAt),
		ChallengeDifficulty: challengeDiff,
		ChallengeElapsedMs:  challengeElapsed.Milliseconds(),
		ChallengeSolved:     challengeOK,
	}
	if health, err := db.GetDoubanHealth(); err == nil {
		snap.Health = health
	} else {
		note("豆瓣数据完整性统计失败: %v", err)
	}
	if duplicates, err := db.ListDoubanDuplicateGroups(50); err == nil {
		snap.Duplicates = duplicates
	} else {
		note("重复豆瓣 ID 查询失败: %v", err)
	}
	return snap
}

func (a *App) diagnosticsCollect(note func(string, ...any)) DiagCollect {
	status := a.GetCollectSchedule()
	if status == nil {
		note("采集调度器状态不可用")
		return DiagCollect{}
	}
	snap := DiagCollect{
		Running:          status.Running,
		Background:       status.Background,
		EverySeconds:     status.BackgroundEverySeconds,
		SourceGapSeconds: status.SourceGapSeconds,
		PageGapSeconds:   status.PageGapSeconds,
		LastRunUnix:      status.LastRunUnix,
		LastExitUnix:     status.LastExitUnix,
	}
	for _, item := range status.SourceSchedules {
		if item.Enabled {
			snap.ScheduledSources++
		}
		if item.Running {
			snap.ActiveCollects++
		}
	}
	return snap
}

// ProbeSources 逐个探测采集源接口能否应答：带 ac=videolist&pg=1 发一次 GET，
// 记录状态码与首包耗时。只读前 4KB 用于判断返回的是不是列表结构，
// 不把整个响应灌进内存，也不落库。
func (a *App) ProbeSources() ([]SourceProbe, error) {
	sources, err := db.GetAllSources()
	if err != nil {
		return nil, err
	}
	out := make([]SourceProbe, len(sources))
	client := &http.Client{Timeout: sourceProbeTimeout}
	sem := make(chan struct{}, sourceProbeConcurrency)
	var wg sync.WaitGroup
	for i := range sources {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			out[i] = probeSource(client, sources[i])
		}(i)
	}
	wg.Wait()
	return out, nil
}

// SourceProbe 是单个采集源的一次连通性探测结果。
type SourceProbe struct {
	SourceKey  string `json:"source_key"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url"`
	OK         bool   `json:"ok"`
	StatusCode int    `json:"status_code"`
	LatencyMS  int64  `json:"latency_ms"`
	SampleKB   int    `json:"sample_kb"`
	Error      string `json:"error"`
}

func probeSource(client *http.Client, source *model.Source) SourceProbe {
	probe := SourceProbe{
		SourceKey: source.SourceKey,
		Name:      source.Name,
		Enabled:   source.Enabled == 1,
	}
	target := collectProbeURL(source.ApiUrl)
	probe.URL = target
	if target == "" {
		probe.Error = "接口地址为空"
		return probe
	}

	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		probe.Error = err.Error()
		return probe
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/137.0.0.0 Safari/537.36 Edg/137.0.0.0")
	req.Header.Set("Accept", "application/json,text/plain,*/*")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		probe.LatencyMS = time.Since(start).Milliseconds()
		probe.Error = err.Error()
		return probe
	}
	defer resp.Body.Close()

	buf := make([]byte, sourceProbeSampleBytes)
	read, readErr := io.ReadFull(resp.Body, buf)
	probe.StatusCode = resp.StatusCode
	probe.LatencyMS = time.Since(start).Milliseconds()
	probe.SampleKB = (read + 1023) / 1024
	body := string(buf[:read])
	switch {
	case resp.StatusCode != http.StatusOK:
		probe.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	case !strings.Contains(body, `"list`):
		probe.Error = "响应里没有 list 字段，接口可能已改版或被拦截"
		if readErr != nil && read == 0 {
			probe.Error = "响应为空"
		}
	default:
		probe.OK = true
	}
	return probe
}

// collectProbeURL 在源地址上补齐列表参数，已有 query 时用 & 续接。
func collectProbeURL(apiURL string) string {
	apiURL = strings.TrimSpace(apiURL)
	if apiURL == "" {
		return ""
	}
	separator := "?"
	if strings.Contains(apiURL, "?") {
		separator = "&"
	}
	return apiURL + separator + "ac=videolist&pg=1"
}

// GetDoubanIntervalMinutes 返回当前生效的豆瓣补全轮询间隔（分钟）。
func (a *App) GetDoubanIntervalMinutes() int {
	interval, _, _, _ := a.schedulers.DoubanSchedule()
	if interval > 0 {
		return int(interval.Minutes())
	}
	return storedDoubanIntervalMinutes()
}

// SetDoubanIntervalMinutes 立刻改豆瓣轮询间隔并持久化，返回真正生效的分钟数。
// 下限 1 分钟：豆瓣的反爬只认「够慢」，这里不给更小的值兜底空间。
func (a *App) SetDoubanIntervalMinutes(minutes int) (int, error) {
	if minutes < minDoubanIntervalMinutes {
		minutes = minDoubanIntervalMinutes
	}
	applied := a.schedulers.SetDoubanInterval(time.Duration(minutes) * time.Minute)
	if effective := int(applied.Minutes()); effective > 0 {
		minutes = effective
	}
	if err := db.SetSetting(settingDoubanIntervalMinutes, strconv.Itoa(minutes)); err != nil {
		return minutes, fmt.Errorf("间隔已生效但保存失败: %w", err)
	}
	applog.Info("豆瓣补全轮询间隔设为 %d 分钟", minutes)
	return minutes, nil
}

// GetLogKeepDays 返回当前生效的日志保留天数。
func (a *App) GetLogKeepDays() int {
	return applog.Default().KeepDays()
}

// SetLogKeepDays 改日志保留天数，立刻按新值清理一次并持久化。
func (a *App) SetLogKeepDays(days int) (int, error) {
	applied := applog.SetKeepDays(days)
	if err := db.SetSetting(settingLogKeepDays, strconv.Itoa(applied)); err != nil {
		return applied, fmt.Errorf("保留天数已生效但保存失败: %w", err)
	}
	applog.Info("日志保留天数设为 %d 天", applied)
	return applied, nil
}

// applyPersistedDiagnosticsSettings 在 DB 就绪后恢复诊断页可改的两项设置。
// 传豆瓣间隔给调度器的构造点之外再应用一次，保证设置页改完重启仍然有效。
func (a *App) applyPersistedDiagnosticsSettings() {
	if minutes := storedDoubanIntervalMinutes(); minutes > 0 {
		a.schedulers.SetDoubanInterval(time.Duration(minutes) * time.Minute)
	}
	if raw, err := db.GetSetting(settingLogKeepDays); err == nil {
		if days, convErr := strconv.Atoi(strings.TrimSpace(raw)); convErr == nil && days >= 1 {
			applog.SetKeepDays(days)
		}
	}
}

func storedDoubanIntervalMinutes() int {
	raw, err := db.GetSetting(settingDoubanIntervalMinutes)
	if err != nil {
		return defaultDoubanIntervalMinutes
	}
	minutes, convErr := strconv.Atoi(strings.TrimSpace(raw))
	if convErr != nil || minutes < minDoubanIntervalMinutes {
		return defaultDoubanIntervalMinutes
	}
	return minutes
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

// wailsVersionFromBuildInfo 从构建信息里读 Wails 版本，dev 下常是 (devel)。
func wailsVersionFromBuildInfo() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, dep := range info.Deps {
		if dep != nil && strings.HasPrefix(dep.Path, "github.com/wailsapp/wails/") {
			return dep.Version
		}
	}
	return ""
}
