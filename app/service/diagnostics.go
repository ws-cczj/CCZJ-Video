package service

import (
	"context"
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
	cacheservice "cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	"cczjVideo/app/model"
	"cczjVideo/app/proxy"
	"cczjVideo/app/updater"
)

// ======================== 诊断台 ========================
//
// 设置页「诊断」分组的数据源。GetDiagnostics 全部只读：不写库、不改配置、
// 不触发抓取。主动发请求的只有源巡检（ProbeSources 与后台的 runSourcePatrol），
// 发的又是采集源本来就该应答的列表首页，等价于手动点一次「采集第 1 页」；
// 每次探测顺带在 source_health 留一条样本，健康度历史就是这么攒起来的。

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

	// sourcePatrolInterval 是后台巡检的节奏。一轮巡检每个启用的源只发一个列表首页
	// 请求——与手动点一次「探测」完全等量的流量。6 小时一天四趟，是"源悄悄下线了
	// 但没人采它所以永远没人知道"和"给源站添负担"之间偏保守的那一侧。
	sourcePatrolInterval = 6 * time.Hour
	// sourcePatrolFirstDelay 把启动后的第一趟巡检推后一点，避开启动阶段可能在跑的
	// 全量采集/补采，也让应用先把界面开出来。
	sourcePatrolFirstDelay = 45 * time.Second
	// sourceHealthWindow 是诊断页汇总健康度时看的样本条数。取 20 条是因为每个源
	// 保留的样本本就很少（见 db.sourceHealthKeep），再短就把偶发抖动读成趋势。
	sourceHealthWindow = 20
	// sourceHealthRecentDots 是界面上那条点阵历史画多少个点。
	sourceHealthRecentDots = 12
	// sourceDeadStreak 是连败多少次算"可能已失效"。一次失败可能是网络抖动，
	// 三次意味着跨过了至少一轮巡检，值得单独提示。
	sourceDeadStreak = 3
)

// DiagEnv 是构建与运行环境，出问题时报备用的最小集合。
type DiagEnv struct {
	AppVersion      string         `json:"app_version"`
	InstalledMarker string         `json:"installed_marker"`
	SchemaVersion   int            `json:"schema_version"`
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

// DiagStorage 是各块磁盘占用的汇总，外加进程内派生缓存的条目数。
//
// 派生缓存（详情、热榜匹配、评论页）只活在内存里，以前这里报的是一个恒为 0 的
// "磁盘缓存" 占位 —— ts_cache 目录从来没有被创建过。改成报条目数之后，
// 「清除缓存」到底清掉了东西没有，在诊断页上就能直接看出来。
type DiagStorage struct {
	DatabasePath  string `json:"database_path"`
	DatabaseBytes int64  `json:"database_bytes"`
	LogDir        string `json:"log_dir"`
	LogBytes      int64  `json:"log_bytes"`
	LogFiles      int    `json:"log_files"`
	LogKeepDays   int    `json:"log_keep_days"`

	DetailEntries int   `json:"detail_entries"`
	DetailBytes   int64 `json:"detail_bytes"`
	ChartMatches  int   `json:"chart_matches"`
	CommentPages  int   `json:"comment_pages"`
}

// DiagDouban 是豆瓣补全的调度节奏、反爬状态与数据完整性。
type DiagDouban struct {
	Running             bool            `json:"running"`
	Updating            bool            `json:"updating"`
	IntervalMinutes     int             `json:"interval_minutes"`
	LastTickUnix        int64           `json:"last_tick_unix"`
	NextTickUnix        int64           `json:"next_tick_unix"`
	SilentRemainingSec  int64           `json:"silent_remaining_sec"`
	SilentStrikes       int             `json:"silent_strikes"`
	ChallengeAtUnix     int64           `json:"challenge_at_unix"`
	ChallengeDifficulty int             `json:"challenge_difficulty"`
	ChallengeElapsedMs  int64           `json:"challenge_elapsed_ms"`
	ChallengeSolved     bool            `json:"challenge_solved"`
	Health              db.DoubanHealth `json:"health"`
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

// DiagSourceRow 是单个采集源的目录统计与最近一次采集结果。
type DiagSourceRow struct {
	SourceKey  string `json:"source_key"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	APIUrl     string `json:"api_url"`
	VideoCount int    `json:"video_count"`

	Collecting         bool   `json:"collecting"`
	LastSaved          int    `json:"last_saved"`
	LastFetchFailed    int    `json:"last_fetch_failed_pages"`
	LastSaveFailed     int    `json:"last_save_failed_pages"`
	LastErrorKind      string `json:"last_error_kind"`
	LastError          string `json:"last_error"`
	LastElapsedMs      int64  `json:"last_elapsed_ms"`
	LastFinishedAtUnix int64  `json:"last_finished_at_unix"`

	// 增量水位线：covered_until 只在完整成功的采集后推进，last_attempt 无论成败都记。
	CoveredUntilUnix int64 `json:"covered_until_unix"`
	LastAttemptUnix  int64 `json:"last_attempt_unix"`

	// 健康度历史：采集运行与主动巡检各算各的样本。分开报是因为两者的 latency
	// 量级完全不同（整轮运行 vs 单次请求），合成一个数就两边都读不出来。
	CollectHealth db.SourceHealth `json:"collect_health"`
	PatrolHealth  db.SourceHealth `json:"patrol_health"`

	// 最近若干条样本，采集与巡检掺在一起按时间倒序。界面用它画成一小段点阵，
	// 于是"最近好端端的，从哪一次开始连续红"是看得出来的——只看成功率一个
	// 百分比会把"最近全红"和"很久以前红过一次"读成同一个数。
	Recent []db.SourceHealthSample `json:"recent_samples"`
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
	// schema 版本落后说明迁移没跑完，这比任何单项统计都优先，因为后面的读写
	// 可能已经踩在不存在的列上。
	if version, err := db.SchemaVersion(); err == nil {
		d.Env.SchemaVersion = version
		if latest := db.LatestSchemaVersion(); version < latest {
			note("数据库 schema 停在 v%d，仍有迁移未应用（目标 v%d）", version, latest)
		}
	} else {
		note("数据库 schema 版本读取失败: %v", err)
	}

	if info, err := a.cache.GetInfo(); err == nil {
		d.Storage = DiagStorage{
			DatabasePath:  info.DatabasePath,
			DatabaseBytes: info.DatabaseBytes,
			LogDir:        info.LogFilePath,
			LogBytes:      info.LogFileBytes,
		}
	} else {
		note("存储占用统计失败: %v", err)
	}
	cacheStats := cacheservice.Stats()
	d.Storage.DetailEntries = cacheStats.DetailEntries
	d.Storage.DetailBytes = cacheStats.DetailBytes
	d.Storage.ChartMatches = cacheStats.ChartMatches
	d.Storage.CommentPages = cacheStats.CommentPages
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
		row := DiagSourceRow{
			SourceKey:  src.SourceKey,
			Name:       src.Name,
			Enabled:    src.Enabled == 1,
			APIUrl:     src.ApiUrl,
			VideoCount: counts[src.SourceKey],
		}
		if st := a.GetCollectStatus(src.SourceKey); st != nil {
			row.Collecting = st.Running
			row.LastSaved = st.Saved
			row.LastFetchFailed = len(st.FetchFailures)
			row.LastSaveFailed = len(st.SaveFailures)
			row.LastErrorKind = st.ErrorKind
			row.LastError = st.Log
			row.LastElapsedMs = st.ElapsedMs
			row.LastFinishedAtUnix = st.FinishedAtUnix
		}
		if cursor, cursorErr := db.GetCollectCursor(src.SourceKey); cursorErr == nil {
			row.CoveredUntilUnix = cursor.CoveredUntilUnix
			row.LastAttemptUnix = cursor.LastAttemptUnix
		} else {
			note("采集源 %s 的增量水位线读取失败: %v", src.Name, cursorErr)
		}
		if health, err := db.GetSourceHealth(src.SourceKey, db.SourceHealthKindCollect, sourceHealthWindow); err == nil {
			row.CollectHealth = health
		} else {
			note("采集源 %s 的采集健康度读取失败: %v", src.Name, err)
		}
		if health, err := db.GetSourceHealth(src.SourceKey, db.SourceHealthKindPatrol, sourceHealthWindow); err == nil {
			row.PatrolHealth = health
		} else {
			note("采集源 %s 的巡检健康度读取失败: %v", src.Name, err)
		}
		if recent, err := db.ListSourceHealth(src.SourceKey, "", sourceHealthRecentDots); err == nil {
			row.Recent = recent
		} else {
			note("采集源 %s 的健康度历史读取失败: %v", src.Name, err)
		}
		// 只写了几条也算失败：整页丢掉的源必须出现在提示里，
		// 否则目录条数看着正常，实际缺数据。
		if row.LastFetchFailed > 0 || row.LastSaveFailed > 0 {
			note("采集源 %s 上次有 %d 页取页失败、%d 页入库失败", src.Name, row.LastFetchFailed, row.LastSaveFailed)
		}
		// 连败到一定次数就不再像抖动，更像接口已经下线。巡检看的是单页能否应答，
		// 所以它比采集更早发现"源还在跑但地址已经废了"。
		if row.PatrolHealth.FailStreak >= sourceDeadStreak {
			note("采集源 %s 接口连续 %d 次巡检失败，可能已失效：%s", src.Name, row.PatrolHealth.FailStreak, row.PatrolHealth.LastError)
		} else if row.CollectHealth.FailStreak >= sourceDeadStreak {
			note("采集源 %s 连续 %d 次采集失败，可能已失效：%s", src.Name, row.CollectHealth.FailStreak, row.CollectHealth.LastError)
		}
		rows = append(rows, row)
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
// 不把整个响应灌进内存。结果会落一条巡检样本进 source_health，
// 所以手动点一次「探测」与后台自动巡检共用同一份历史。
func (a *App) ProbeSources() ([]SourceProbe, error) {
	return a.probeAllSources(a.background.Context())
}

// probeAllSources 跑一轮探测并记账。ctx 取消时正在飞的请求立刻断开，
// 于是退出应用不会被一趟 10 秒超时的巡检拖住。
func (a *App) probeAllSources(ctx context.Context) ([]SourceProbe, error) {
	sources, err := db.GetAllSources()
	if err != nil {
		return nil, err
	}
	enabled := make([]*model.Source, 0, len(sources))
	for _, src := range sources {
		if src.Enabled == 1 {
			enabled = append(enabled, src)
		}
	}
	out := make([]SourceProbe, len(enabled))
	client := &http.Client{Timeout: sourceProbeTimeout}
	sem := make(chan struct{}, sourceProbeConcurrency)
	var wg sync.WaitGroup
	for i := range enabled {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-sem }()
			out[i] = probeSource(ctx, client, enabled[i])
			if dbErr := db.RecordSourceHealth(out[i].SourceKey, db.SourceHealthKindPatrol, out[i].OK,
				out[i].LatencyMS, 0, out[i].Error, time.Now()); dbErr != nil {
				applog.Warn("[Diag] 记录巡检样本失败（不影响探测结果）: %v", dbErr)
			}
		}(i)
	}
	wg.Wait()
	// 取消时 out 里会有没跑完的空探测，把它们当成"不应答"报出去只会让退出日志
	// 里多出一串假故障。
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return out, nil
}

// runSourcePatrol 是后台巡检循环。停在这里等 ctx 取消，所以它和调度器一样
// 由 lifecycle.Group 统一收尾，不需要额外的停止接口。
func (a *App) runSourcePatrol(ctx context.Context) {
	timer := time.NewTimer(sourcePatrolFirstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			probes, err := a.probeAllSources(ctx)
			if err != nil {
				applog.Warn("[Diag] 源巡检未完成: %v", err)
			} else {
				failed := 0
				for _, p := range probes {
					if !p.OK {
						failed++
					}
				}
				applog.Info("[Diag] 源巡检完成：%d 个源，%d 个不应答", len(probes), failed)
			}
			timer.Reset(sourcePatrolInterval)
		}
	}
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

func probeSource(ctx context.Context, client *http.Client, source *model.Source) SourceProbe {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
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
	// 内网放行必须在第一次代理请求之前恢复：播放器一就绪就会去取 m3u8，
	// 晚一步的话用户开着开关却看到第一次播放失败，还会以为开关没用。
	if raw, err := db.GetSetting(settingAllowPrivateNetwork); err == nil {
		proxy.SetAllowPrivateTargets(strings.TrimSpace(raw) == "1")
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
