package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	cacheservice "cczjVideo/app/cache"
	"cczjVideo/app/db"
	"cczjVideo/app/douban"
	"cczjVideo/app/handler"
	"cczjVideo/app/model"
	"cczjVideo/app/netstats"
	"cczjVideo/app/proxy"
	"cczjVideo/app/updater"
)

// ======================== 诊断台 ========================
//
// 设置页「诊断」分组的数据源。GetDiagnostics 全部只读：不写库、不改配置、
// 不触发抓取。主动发请求的只有采集源页上的探测（ProbeSource 与 ProbeSources），
// 发的又是采集源本来就该应答的列表首页，等价于手动点一次「采集第 1 页」；
// 每次探测顺带在 source_health 留一条样本，健康度历史和点阵就是这么攒起来的。

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

	// sourceProbeMinInterval 是同一个源两次探测之间的最小间隔。一次探测等价于
	// 手动点一次「采集第 1 页」，源站并不希望它被反复戳；5 分钟也和采集调度
	// 允许的最小间隔同量级，界面上连点不会变成对源站的压力。
	//
	// 它同时是界面点阵一个圆点覆盖的时间窗：一个点 = 一次探测机会。
	sourceProbeMinInterval = 5 * time.Minute

	// sourceProbeSlotCount 是探测点阵画多少个点。10 个点铺开最近 50 分钟，
	// 够回答"这个源是刚坏还是这半小时一直不通"，也还塞得进卡片头部。
	sourceProbeSlotCount = 10

	// sourceHealthWindow 是诊断页汇总健康度时看的样本条数。取 20 条是因为每个源
	// 保留的样本本就很少（见 db.sourceHealthKeep），再短就把偶发抖动读成趋势。
	sourceHealthWindow = 20
	// sourceHealthRecentDots 是界面上那条点阵历史画多少个点。
	sourceHealthRecentDots = 12
	// sourceDeadStreak 引用策略层的阈值，不另写一份。判定"源可能已失效"和
	// 自动停用必须是同一个数字，否则诊断页提示的连败次数和实际停用条件对不上。
	sourceDeadStreak = handler.SourceDeadStreak
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

// DiagStorage 是各块磁盘占用的汇总。进程内缓存不在这里，它们归 Diagnostics.Cache：
// 条目数和命中率要成对读才有意义，拆在两个区块里只会让人找不到。
type DiagStorage struct {
	DatabasePath  string `json:"database_path"`
	DatabaseBytes int64  `json:"database_bytes"`
	LogDir        string `json:"log_dir"`
	LogBytes      int64  `json:"log_bytes"`
	LogFiles      int    `json:"log_files"`
	LogKeepDays   int    `json:"log_keep_days"`
}

// DiagNetwork 是这一趟进程的出网账本，来自 netstats 的计数 RoundTripper。
//
// 速率不是测速软件的读数，而是真实请求算出来的：播放中继、测速取样、更新包的 Range
// 探测各自贡献自己的字节与耗时。没跑过的类别是 0，界面对应行直接不画。
//
// AllowPrivateTargets 放在这里是因为它会改变上面所有数字的可比性：放行私网后，
// 同一类流量可能来自局域网 NAS，速度和公网源站完全不是一个量级。
type DiagNetwork struct {
	SinceUnix           int64           `json:"since_unix"`
	TotalBytes          int64           `json:"total_bytes"`
	AllowPrivateTargets bool            `json:"allow_private_targets"`
	Categories          []netstats.Stat `json:"categories"`
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
	Env     DiagEnv            `json:"env"`
	Storage DiagStorage        `json:"storage"`
	Network DiagNetwork        `json:"network"`
	Cache   []cacheservice.Row `json:"cache"`
	Douban  DiagDouban         `json:"douban"`
	Collect DiagCollect        `json:"collect"`
	Sources []DiagSourceRow    `json:"sources"`
	Tables  []db.TableStat     `json:"tables"`
	Notes   []string           `json:"notes"`
}

// RuntimeMetrics 是界面每隔两三秒轮一次的轻量快照：只有内存里的计数器读数，
// 不查库、不遍历目录、不做全表 COUNT。真实使用中的网络与缓存数字一直在涨，
// 手动刷新一次的快照看不到趋势，所以把这两块单独开一个便宜口。
type RuntimeMetrics struct {
	UptimeSeconds int64              `json:"uptime_seconds"`
	Goroutines    int                `json:"goroutines"`
	Network       DiagNetwork        `json:"network"`
	Cache         []cacheservice.Row `json:"cache"`
}

func (a *App) runtimeNetwork() DiagNetwork {
	return DiagNetwork{
		SinceUnix:           netstats.SinceUnix(),
		TotalBytes:          netstats.TotalBytes(),
		AllowPrivateTargets: proxy.AllowPrivateTargets(),
		Categories:          netstats.Snapshot(),
	}
}

func (a *App) GetRuntimeMetrics() *RuntimeMetrics {
	return &RuntimeMetrics{
		UptimeSeconds: int64(time.Since(appStartedAt).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		Network:       a.runtimeNetwork(),
		Cache:         cacheservice.Stats(),
	}
}

// GetDiagnostics 汇总环境、存储、豆瓣、采集调度、源统计与表行数。
func (a *App) GetDiagnostics() (*Diagnostics, error) {
	d := &Diagnostics{Notes: []string{}}
	note := func(format string, args ...any) {
		d.Notes = append(d.Notes, fmt.Sprintf(format, args...))
	}

	d.Env = a.diagnosticsEnv()
	// 标记在应用目录（exe 旁边），且启动时读一次就被清掉，所以取本进程记下的那个值。
	// 以前这里去数据目录找同名文件，那个位置从来没人写过，字段永远空着。
	if marker := updater.InstalledMarkerSeenThisRun(); marker != "" {
		d.Env.InstalledMarker = marker
	}
	// 回执只活在这次会话里（结论文件启动时就被消费掉了），所以这是失败之后唯一的现场。
	// 结论词表里 install 与 rollback 两类失败共用 _failed 后缀，现场话要说清是哪一边。
	if report := a.update.LastInstallReport(); report.Failed {
		note("上次替换没有生效：脚本回执 %s，当前运行 %s", report.Result, report.Running)
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
	d.Network = a.runtimeNetwork()
	d.Cache = cacheservice.Stats()
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
// 所以手动点一次「探测」和页面自动探到的那一次是同一份历史。
//
// 每个源最多 sourceProbeMinInterval 探一次：还在冷却里的源直接不发请求，
// 把上一条巡检样本和还需等待的秒数回给界面。
func (a *App) ProbeSources() ([]SourceProbe, error) {
	return a.probeAllSources(a.background.Context())
}

// ProbeSource 探测单个采集源。与「探测全部」共用同一道闸门、同一份巡检样本，
// 所以单独点某一个源不会绕过节流，也不会让健康度历史分成两套口径。
func (a *App) ProbeSource(sourceKey string) (*SourceProbe, error) {
	source, err := db.GetSourceByKey(sourceKey)
	if err != nil {
		return nil, err
	}
	last, retryAfter, allowed := admitSourceProbe(source.SourceKey, time.Now())
	if !allowed {
		probe := coolingProbe(source, last, retryAfter)
		return &probe, nil
	}
	client := &http.Client{Timeout: sourceProbeTimeout, Transport: netstats.WrapTransport(netstats.CategoryCollect, nil)}
	ctx := a.background.Context()
	probe := probeSource(ctx, client, source)
	recordPatrolSample(ctx, probe)
	return &probe, nil
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
	client := &http.Client{Timeout: sourceProbeTimeout, Transport: netstats.WrapTransport(netstats.CategoryCollect, nil)}
	sem := make(chan struct{}, sourceProbeConcurrency)
	var wg sync.WaitGroup
	for i := range enabled {
		// 取决策在派发之前：冷却中的源连信号量都不占，一轮"探测全部"点的
		// 越快越不会给源站叠请求。
		last, retryAfter, allowed := admitSourceProbe(enabled[i].SourceKey, time.Now())
		if !allowed {
			out[i] = coolingProbe(enabled[i], last, retryAfter)
			continue
		}
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
			recordPatrolSample(ctx, out[i])
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

// recordPatrolSample 把一次真实探测落成巡检样本。记账失败只写日志：
// 探测结果本身已经拿到，不该因为观测面写不进去让调用方以为探测失败了。
// 连败到阈值时的自动停用由 handler 那侧统一判定，采集与巡检共用一套阈值。
//
// ctx 已取消时一条都不落：关停窗口里没跑完的探测会在 client.Do 上返回
// "context canceled"，记成失败就等于"开着采集源页退出三次"能把一个正常源判成死源。
func recordPatrolSample(ctx context.Context, probe SourceProbe) {
	if ctx.Err() != nil {
		return
	}
	handler.RecordSourceHealthSample(probe.SourceKey, db.SourceHealthKindPatrol, probe.OK,
		probe.LatencyMS, 0, probe.Error)
}

// SourceProbeSlot 是探测点阵里的一个时间窗。窗口宽度就是节流间隔，
// 所以「没探测」不是失败，只是这段时间里没人戳过它——界面要能分清这两种灰色。
type SourceProbeSlot struct {
	BucketUnix int64  `json:"bucket_unix"`
	Probed     bool   `json:"probed"`
	OK         bool   `json:"ok"`
	LatencyMS  int64  `json:"latency_ms"`
	TsUnix     int64  `json:"ts_unix"`
	Error      string `json:"error"`
}

// SourceProbeTimeline 是一个源最近 sourceProbeSlotCount 个时间窗的探测记录，
// 按时间从旧到新排列。
type SourceProbeTimeline struct {
	SourceKey string            `json:"source_key"`
	SlotSecs  int64             `json:"slot_secs"`
	Slots     []SourceProbeSlot `json:"slots"`
}

// SourceProbeTimeline 把已落库的巡检样本铺成点阵给界面画。纯读库、不发请求：
// 真正发请求的是 ProbeSources / ProbeSource，而它们只在采集源页被打开时才调，
// 所以离开这一页就不会再有任何探测流量。
func (a *App) SourceProbeTimeline() ([]SourceProbeTimeline, error) {
	sources, err := db.GetAllSources()
	if err != nil {
		return nil, err
	}
	slotSecs := int64(sourceProbeMinInterval / time.Second)
	firstBucket := time.Now().Unix()/slotSecs - int64(sourceProbeSlotCount-1)
	out := make([]SourceProbeTimeline, 0, len(sources))
	for _, src := range sources {
		// 取两倍条数：同一格里挤进多条时，后来的旧样本仍要把格子填上。
		samples, err := db.ListSourceHealth(src.SourceKey, db.SourceHealthKindPatrol, sourceProbeSlotCount*2)
		if err != nil {
			return nil, err
		}
		out = append(out, SourceProbeTimeline{
			SourceKey: src.SourceKey,
			SlotSecs:  slotSecs,
			Slots:     bucketProbeSamples(samples, firstBucket, slotSecs, sourceProbeSlotCount),
		})
	}
	return out, nil
}

// bucketProbeSamples 把「新→旧」的巡检样本铺进 count 个等宽时间窗。
// 单独拆出来是为了可测：分桶边界（同一格取最新那条、窗口外的样本要丢掉）
// 是点阵唯一会读错的地方，不该只在真机上看。
func bucketProbeSamples(samples []db.SourceHealthSample, firstBucket, slotSecs int64, count int) []SourceProbeSlot {
	slots := make([]SourceProbeSlot, count)
	for i := range slots {
		slots[i].BucketUnix = (firstBucket + int64(i)) * slotSecs
	}
	for _, sample := range samples {
		idx := int(sample.TsUnix/slotSecs - firstBucket)
		if idx < 0 || idx >= count || slots[idx].Probed {
			continue
		}
		slots[idx].Probed = true
		slots[idx].OK = sample.OK
		slots[idx].LatencyMS = sample.LatencyMS
		slots[idx].TsUnix = sample.TsUnix
		slots[idx].Error = sample.Err
	}
	return slots
}

// SourceProbe 是单个采集源的一次连通性探测结果。
//
// Skipped 为真表示这次没发请求（该源还在冷却里），此时 OK/Error/ProbeTimeUnix
// 沿用上一条巡检样本，RetryAfterSec 告诉界面还要等多久。
type SourceProbe struct {
	SourceKey     string `json:"source_key"`
	Name          string `json:"name"`
	Enabled       bool   `json:"enabled"`
	URL           string `json:"url"`
	OK            bool   `json:"ok"`
	StatusCode    int    `json:"status_code"`
	LatencyMS     int64  `json:"latency_ms"`
	SampleKB      int    `json:"sample_kb"`
	Error         string `json:"error"`
	Skipped       bool   `json:"skipped"`
	RetryAfterSec int    `json:"retry_after_sec"`
	ProbeTimeUnix int64  `json:"probe_time_unix"`
}

var (
	// sourceProbeMu 保护闸门：连点两次「探测」时，两个调用必须看到同一份"已经开始"
	// 记录，否则都会判定冷却已过、各发一遍请求。
	sourceProbeMu sync.Mutex
	// sourceProbeStartedAt 记住本轮探测的开始时刻。它和 source_health 里最后一条
	// 巡检样本取较后者：前者挡住同一毫秒内的并发，后者让节流跨过重启仍然成立。
	sourceProbeStartedAt = map[string]time.Time{}
)

// admitSourceProbe 判定某源此刻能否发探测请求。允许时顺手登记开始时间，
// 所以判定与登记是同一把锁里的一次动作。
func admitSourceProbe(sourceKey string, now time.Time) (db.SourceHealth, int, bool) {
	sourceProbeMu.Lock()
	defer sourceProbeMu.Unlock()

	var last db.SourceHealth
	if health, err := db.GetSourceHealth(sourceKey, db.SourceHealthKindPatrol, 1); err == nil {
		last = health
	}
	lastProbe := sourceProbeStartedAt[sourceKey]
	if sample := last.LastSampleUnix; sample > 0 {
		if at := time.Unix(sample, 0); at.After(lastProbe) {
			lastProbe = at
		}
	}
	retryAfter, allowed := probeRetryAfter(now, lastProbe, sourceProbeMinInterval)
	if !allowed {
		return last, retryAfter, false
	}
	sourceProbeStartedAt[sourceKey] = now
	return last, 0, true
}

// probeRetryAfter 是纯判定：lastProbe 为零值表示从没探过，直接放行；
// 否则不足最小间隔时向上取整返回还需等待的秒数。
func probeRetryAfter(now, lastProbe time.Time, minInterval time.Duration) (int, bool) {
	if lastProbe.IsZero() {
		return 0, true
	}
	elapsed := now.Sub(lastProbe)
	if elapsed >= minInterval {
		return 0, true
	}
	remaining := minInterval - elapsed
	seconds := int((remaining + time.Second - 1) / time.Second)
	return seconds, false
}

// coolingProbe 给冷却中的源拼一条结果：不发请求，所以没有状态码和延迟，
// 但把上次探到的结果和时间带上，界面上不会出现"刚点过却什么都看不到"。
func coolingProbe(source *model.Source, last db.SourceHealth, retryAfterSec int) SourceProbe {
	return SourceProbe{
		SourceKey:     source.SourceKey,
		Name:          source.Name,
		Enabled:       source.Enabled == 1,
		URL:           collectProbeURL(source.ApiUrl),
		OK:            last.LastOK,
		Error:         last.LastError,
		Skipped:       true,
		RetryAfterSec: retryAfterSec,
		ProbeTimeUnix: last.LastSampleUnix,
	}
}

func probeSource(ctx context.Context, client *http.Client, source *model.Source) SourceProbe {
	probe := SourceProbe{
		SourceKey:     source.SourceKey,
		Name:          source.Name,
		Enabled:       source.Enabled == 1,
		ProbeTimeUnix: time.Now().Unix(),
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
		return minutes, apperror.Wrap(apperror.Storage, err, "间隔已生效但保存失败")
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
		return applied, apperror.Wrap(apperror.Storage, err, "保留天数已生效但保存失败")
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
