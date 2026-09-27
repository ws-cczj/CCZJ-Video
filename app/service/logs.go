package service

import (
	"bufio"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ======================== 日志中心 ========================
//
// 这里的绑定统一面向设置页的「日志」分组：内存环形缓冲负责实时时间线，
// 日志文件负责历史回溯，两者共用同一条记录结构 applog.Record。

// maxLogFileScanBytes 限制单文件扫描的读取量，避免超大文件把界面卡死。
const maxLogFileScanBytes = 96 << 20

// maxExportBytes 限制单次导出的写出量。
const maxExportBytes = 256 << 20

// maxExportRecords 限制单次导出的记录条数，超过则要求用户缩小筛选范围。
const maxExportRecords = 200000

// LogPageReq 描述一次历史文件分页 / 检索。
type LogPageReq struct {
	Filename string   `json:"filename"`
	StartRow int      `json:"start_row"` // -1 表示从文件末尾往前取；否则取 [start_row-limit+1, start_row]
	Limit    int      `json:"limit"`
	Levels   []string `json:"levels"`
	Query    string   `json:"query"`
}

// LogPageResp 是一次历史文件查询结果。Row 为文件内的 0 起始行号。
type LogPageResp struct {
	Records    []applog.Record `json:"records"`
	TotalRows  int             `json:"total_rows"`
	MatchRows  int             `json:"match_rows"`
	FirstRow   int             `json:"first_row"`
	LastRow    int             `json:"last_row"`
	HasEarlier bool            `json:"has_earlier"`
	Filename   string          `json:"filename"`
	Truncated  bool            `json:"truncated"`
	SizeBytes  int64           `json:"size_bytes"`
}

// LogFileInfo 是日志文件的元信息。
type LogFileInfo struct {
	Name      string `json:"name"`
	Day       string `json:"day"`
	SizeBytes int64  `json:"size_bytes"`
	ModTs     int64  `json:"mod_ts"`
}

// LogStats 是日志面板概览卡的数据源。
type LogStats struct {
	Dir      string `json:"dir"`
	Level    string `json:"level"`
	Debug    bool   `json:"debug"`
	Buffered int    `json:"buffered"`
	Capacity int    `json:"capacity"`
	Dropped  uint64 `json:"dropped"`
	LastSeq  uint64 `json:"last_seq"`
	Files    int    `json:"files"`
	TotalKB  int64  `json:"total_kb"`
	KeepDays int    `json:"keep_days"`
	Goarch   string `json:"goarch"`
	Goos     string `json:"goos"`
}

// ExportLogsReq 描述一次导出。Paths 为空时导出内存缓冲。
type ExportLogsReq struct {
	Path     string   `json:"path"`
	Filename string   `json:"filename"` // 非空则导出该文件（配合筛选）
	Levels   []string `json:"levels"`
	Query    string   `json:"query"`
	Format   string   `json:"format"` // log / csv / json
	SinceSeq uint64   `json:"since_seq"`
}

// ExportLogsResult 回报实际写出的内容量。
type ExportLogsResult struct {
	Path  string `json:"path"`
	Lines int    `json:"lines"`
	Bytes int64  `json:"bytes"`
}

func normalizeLevelNames(levels []string) map[string]bool {
	if len(levels) == 0 {
		return nil
	}
	set := make(map[string]bool, len(levels))
	for _, level := range levels {
		name := strings.ToUpper(strings.TrimSpace(level))
		if name == "" {
			continue
		}
		set[name] = true
	}
	if len(set) == 0 {
		return nil
	}
	return set
}

func recordMatches(rec applog.Record, levels map[string]bool, query string) bool {
	if levels != nil && !levels[strings.ToUpper(rec.Level)] {
		return false
	}
	if query != "" && !strings.Contains(strings.ToLower(rec.Message), strings.ToLower(query)) &&
		!strings.Contains(strings.ToLower(rec.Caller), strings.ToLower(query)) {
		return false
	}
	return true
}

// GetLogLevel 返回当前生效的最低日志级别（DEBUG/INFO/WARN/ERROR）。
func (a *App) GetLogLevel() string {
	return applog.LevelName()
}

// SetLogLevel 运行时切换日志级别并持久化，立即生效、无需重启。
func (a *App) SetLogLevel(level string) (string, error) {
	parsed, err := parseLogLevel(level)
	if err != nil {
		return applog.LevelName(), err
	}
	applog.SetMinLevel(parsed)
	if err := db.SetSetting("log_level", parsed.String()); err != nil {
		applog.Warn("applog: 保存 log_level 设置失败: %v", err)
	}
	applog.Info("日志级别已切换为 %s", parsed.String())
	return parsed.String(), nil
}

func parseLogLevel(level string) (applog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return applog.LevelDebug, nil
	case "info":
		return applog.LevelInfo, nil
	case "warn", "warning":
		return applog.LevelWarn, nil
	case "error", "err":
		return applog.LevelError, nil
	default:
		return applog.LevelInfo, fmt.Errorf("未知的日志级别: %q", level)
	}
}

// GetLogRecords 增量读取内存环形缓冲。sinceSeq 传 0 表示取最近 limit 条。
func (a *App) GetLogRecords(sinceSeq uint64, limit int) []applog.Record {
	return applog.Default().Since(sinceSeq, limit)
}

// GetLogStats 返回日志系统的运行状况，供概览卡展示。
func (a *App) GetLogStats() (LogStats, error) {
	logger := applog.Default()
	stats := LogStats{
		Dir:      logger.Dir(),
		Level:    applog.LevelName(),
		Debug:    applog.IsDebug(),
		Buffered: logger.RingLen(),
		Capacity: logger.RingCap(),
		Dropped:  logger.Dropped(),
		LastSeq:  logger.LatestSeq(),
		KeepDays: logger.KeepDays(),
		Goarch:   goruntime.GOARCH,
		Goos:     goruntime.GOOS,
	}
	var total int64
	for _, info := range a.logFiles() {
		stats.Files++
		total += info.SizeBytes
	}
	stats.TotalKB = total / 1024
	return stats, nil
}

// GetLogFiles 返回日志文件列表（按日期倒序）及各自大小。
func (a *App) GetLogFiles() []LogFileInfo {
	return a.logFiles()
}

func (a *App) logFiles() []LogFileInfo {
	logger := applog.Default()
	dir := logger.Dir()
	names := logger.ListFiles()
	out := make([]LogFileInfo, 0, len(names))
	for _, name := range names {
		info := LogFileInfo{Name: name, Day: strings.TrimSuffix(strings.TrimPrefix(name, "cczj-"), ".log")}
		if stat, err := os.Stat(filepath.Join(dir, name)); err == nil {
			info.SizeBytes = stat.Size()
			info.ModTs = stat.ModTime().UnixMilli()
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name > out[j].Name })
	return out
}

// ReadLogPage 从指定日志文件按行读取一页结构化记录，支持级别过滤与关键词检索。
// 逐行流式扫描，内存占用与页大小相关而与文件大小无关。
func (a *App) ReadLogPage(req LogPageReq) (*LogPageResp, error) {
	name := strings.TrimSpace(req.Filename)
	if name == "" {
		name = fmt.Sprintf("cczj-%s.log", time.Now().Format("2006-01-02"))
	}
	logger := applog.Default()
	path := filepath.Join(logger.Dir(), filepath.Base(name))
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("日志文件不可读: %s", filepath.Base(name))
	}
	limit := req.Limit
	if limit <= 0 || limit > 2000 {
		limit = 200
	}
	levels := normalizeLevelNames(req.Levels)

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	resp := &LogPageResp{Filename: filepath.Base(name), FirstRow: -1, LastRow: -1}
	window := make([]applog.Record, 0, limit)
	rows := make([]int, 0, limit)
	_, stopped, err := scanLogLines(file, func(rec applog.Record, row int) bool {
		resp.TotalRows = row + 1
		if !recordMatches(rec, levels, req.Query) {
			return true
		}
		resp.MatchRows++
		if req.StartRow >= 0 && row >= req.StartRow {
			return true
		}
		window = append(window, rec)
		rows = append(rows, row)
		if len(window) > limit {
			window = window[1:]
			rows = rows[1:]
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	resp.Truncated = stopped
	if len(window) > 0 {
		resp.FirstRow = rows[0]
		resp.LastRow = rows[len(rows)-1]
		resp.HasEarlier = rows[0] > 0
	}
	resp.Records = window
	return resp, nil
}

// scanLogLines 按行解析日志文件，回调携带记录与 0 起始行号；回调返回 false 可提前结束。
// 读取字节数超过 maxLogFileScanBytes 时停止，并把 stopped 置为 true。
func scanLogLines(r io.Reader, onRecord func(applog.Record, int) bool) (lines int, stopped bool, err error) {
	reader := bufio.NewReader(r)
	var scanned int64
	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			scanned += int64(len(line))
			if scanned > maxLogFileScanBytes {
				return lines, true, nil
			}
			trimmed := strings.TrimRight(line, "\r\n")
			trimmed = strings.TrimPrefix(trimmed, "\uFEFF")
			lines++
			if trimmed != "" {
				if rec, ok := applog.ParseRecordLine(trimmed); ok {
					if !onRecord(rec, lines-1) {
						return lines, false, nil
					}
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return lines, false, nil
			}
			return lines, false, readErr
		}
	}
}

// collectFileRecords 流式收集一个日志文件中匹配筛选条件的记录（旧→新）。
// 返回的 truncated 为 true 表示命中数或读取量达到上限，结果不完整。
func (a *App) collectFileRecords(name string, levels map[string]bool, query string, maxRecords int) ([]applog.Record, bool, error) {
	path := filepath.Join(applog.Default().Dir(), filepath.Base(name))
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	out := make([]applog.Record, 0, 256)
	truncated := false
	_, stopped, err := scanLogLines(file, func(rec applog.Record, _ int) bool {
		if !recordMatches(rec, levels, query) {
			return true
		}
		if maxRecords > 0 && len(out) >= maxRecords {
			truncated = true
			return false
		}
		out = append(out, rec)
		return true
	})
	if err != nil {
		return nil, false, err
	}
	if stopped {
		truncated = true
	}
	return out, truncated, nil
}

// ExportLogs 按筛选条件把日志写到用户选定的路径。
// 目标路径由前端 Dialogs.SaveFile 选取，这里只校验扩展名与大小。
func (a *App) ExportLogs(req ExportLogsReq) (*ExportLogsResult, error) {
	target := strings.TrimSpace(req.Path)
	if target == "" {
		return nil, fmt.Errorf("未选择导出路径")
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	if format == "" {
		format = "log"
	}
	ext := map[string]string{"log": ".log", "csv": ".csv", "json": ".json"}[format]
	if ext == "" {
		return nil, fmt.Errorf("不支持的导出格式: %s", format)
	}
	if !strings.HasSuffix(strings.ToLower(target), ext) {
		target += ext
	}
	levels := normalizeLevelNames(req.Levels)

	var records []applog.Record
	if strings.TrimSpace(req.Filename) == "" {
		snapshot := applog.Default().Snapshot()
		records = make([]applog.Record, 0, len(snapshot))
		for _, rec := range snapshot {
			if rec.Seq <= req.SinceSeq {
				continue
			}
			if recordMatches(rec, levels, req.Query) {
				records = append(records, rec)
			}
		}
	} else {
		fileRecords, truncated, err := a.collectFileRecords(req.Filename, levels, req.Query, maxExportRecords)
		if err != nil {
			return nil, err
		}
		if truncated {
			return nil, fmt.Errorf("日志文件过大，请追加关键词或级别筛选后再导出")
		}
		records = fileRecords
	}

	out, err := os.Create(target)
	if err != nil {
		return nil, fmt.Errorf("创建导出文件失败: %w", err)
	}
	defer out.Close()

	buf := &strings.Builder{}
	switch format {
	case "json":
		encoded, jsonErr := json.MarshalIndent(records, "", "  ")
		if jsonErr != nil {
			return nil, jsonErr
		}
		buf.Write(encoded)
		buf.WriteString("\n")
	case "csv":
		writer := csv.NewWriter(buf)
		_ = writer.Write([]string{"seq", "time", "level", "caller", "message"})
		for _, rec := range records {
			_ = writer.Write([]string{
				fmt.Sprint(rec.Seq),
				time.UnixMilli(rec.TS).Format("2006-01-02 15:04:05.000"),
				rec.Level,
				rec.Caller,
				rec.Message,
			})
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return nil, err
		}
	default:
		for _, rec := range records {
			buf.WriteString(formatExportLine(rec))
			buf.WriteString("\n")
		}
	}
	data := []byte(buf.String())
	if int64(len(data)) > maxExportBytes {
		return nil, fmt.Errorf("导出内容过大（%d MB），请缩小筛选范围", len(data)>>20)
	}
	if _, err := out.Write(data); err != nil {
		return nil, err
	}
	applog.Info("日志已导出：%s（%d 条）", target, len(records))
	return &ExportLogsResult{Path: target, Lines: len(records), Bytes: int64(len(data))}, nil
}

func formatExportLine(rec applog.Record) string {
	ts := time.UnixMilli(rec.TS).Format("2006-01-02 15:04:05.000")
	if rec.Caller == "" {
		return fmt.Sprintf("[%s] [%s] %s", ts, rec.Level, rec.Message)
	}
	return fmt.Sprintf("[%s] [%s] [%s] %s", ts, rec.Level, rec.Caller, rec.Message)
}

// ---------- 实时日志事件流 ----------

var logStream struct {
	mu     sync.Mutex
	cancel context.CancelFunc
	count  int
}

// StartLogStream 把新产生的日志以 app:log 事件推送给前端。多次调用共享一个转发协程。
func (a *App) StartLogStream() (bool, error) {
	logStream.mu.Lock()
	defer logStream.mu.Unlock()
	logStream.count++
	if logStream.count > 1 {
		return true, nil
	}
	records, cancel := applog.Default().Subscribe()
	ctx, stop := context.WithCancel(context.Background())
	logStream.cancel = stop
	app := application.Get()
	if app == nil {
		cancel()
		logStream.count--
		logStream.cancel = nil
		return false, fmt.Errorf("应用尚未就绪")
	}
	go func() {
		defer cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case rec := <-records:
				app.Event.Emit("app:log", rec)
			}
		}
	}()
	return true, nil
}

// StopLogStream 释放一次订阅；最后一个调用方退出时才真正停止转发。
func (a *App) StopLogStream() (bool, error) {
	logStream.mu.Lock()
	defer logStream.mu.Unlock()
	if logStream.count <= 1 {
		logStream.count = 0
		if logStream.cancel != nil {
			logStream.cancel()
			logStream.cancel = nil
		}
		return true, nil
	}
	logStream.count--
	return true, nil
}
