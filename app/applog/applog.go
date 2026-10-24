package applog

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// Level 日志等级
type Level int

// Fields carries stable operation metadata for logs that need to be correlated
// across asynchronous work. Values are deliberately rendered as text so the
// existing line-oriented log format remains backward compatible.
type Fields map[string]any

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelInfo:
		return "INFO"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	}
	return "?"
}

// Logger 按"天"滚动日志文件。
// - 每天一个文件：cczj-2026-06-11.log
// - 自动清理超过指定天数的旧日志
// - 包含调用者文件:行号
// - 时间戳精确到毫秒
// - 每条落盘的日志同时进入内存环形缓冲，供界面实时查询（见 ring.go）
type Logger struct {
	mu          sync.Mutex
	logDir      string
	currentFile *os.File
	currentDay  string // YYYY-MM-DD
	keepDays    int    // 保留最近多少天的日志
	minLevel    Level  // 最低输出级别，低于此级别的日志被丢弃
	health      Health

	ring    []Record // 环形缓冲，存放最近 DefaultRingSize 条
	head    int      // 下一条写入位置（单调递增，取模使用）
	count   int      // 缓冲内有效条数
	seq     uint64   // 全局递增序号
	subs    map[int]chan Record
	subID   int
	dropped uint64 // 订阅者来不及消费而被丢弃的条数
}

var defaultLogger *Logger
var defaultMu sync.Mutex

// Init 初始化默认 Logger
func Init(logDir string) error {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultLogger == nil {
		defaultLogger = &Logger{logDir: logDir, keepDays: 30}
	}
	l := defaultLogger
	l.mu.Lock()
	if l.logDir != logDir {
		if l.currentFile != nil {
			_ = l.currentFile.Close()
			l.currentFile = nil
		}
		l.logDir, l.currentDay = logDir, ""
	}
	err := l.rotateIfNeeded()
	if err != nil {
		l.health.LastWriteError = DiagnosticText(err.Error(), 2048)
	}
	l.mu.Unlock()
	return err
}

// Default 返回已初始化的默认 logger
func Default() *Logger {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	if defaultLogger == nil {
		defaultLogger = &Logger{logDir: defaultLogDir(), keepDays: 30}
	}
	return defaultLogger
}

// defaultLogDir 决定「未经 Init 就被 Default() 懒建」时日志落在哪儿。
//
// 测试二进制必须避开 %APPDATA%：多数包的测试不调 Init，只要代码路径碰到一次
// applog.Info，就会把测试数据写进用户真实正在看的日志文件里（设置页的日志面板
// 跟着一起脏），也让「看真机日志验证」这条通道不可信。落到临时目录即可，
// 需要断言文件内容的测试仍用自己的 t.TempDir() 显式 Init。
func defaultLogDir() string {
	if testing.Testing() {
		return filepath.Join(os.TempDir(), "cczj-applog-test")
	}
	if dir, err := os.UserConfigDir(); err == nil && strings.TrimSpace(dir) != "" {
		return filepath.Join(dir, "CCZJ Video", "applog")
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Join(filepath.Dir(exe), "data", "applog")
	}
	return filepath.Join(".", "data", "applog")
}

// 便捷方法（带调用位置）
func Debug(format string, args ...interface{}) { Default().log(LevelDebug, 3, format, args...) }
func Info(format string, args ...interface{})  { Default().log(LevelInfo, 3, format, args...) }
func Warn(format string, args ...interface{})  { Default().log(LevelWarn, 3, format, args...) }
func Error(format string, args ...interface{}) { Default().log(LevelError, 3, format, args...) }

// InfoAt/WarnAt 供日志包装函数使用。包装链每多一层，caller 就会多指向上层一帧，
// extra 传"从本函数到真实业务调用点之间隔了几层"，让日志位置指回业务代码而不是包装函数。
func InfoAt(extra int, format string, args ...interface{}) {
	Default().log(LevelInfo, 3+extra, format, args...)
}

func WarnAt(extra int, format string, args ...interface{}) {
	Default().log(LevelWarn, 3+extra, format, args...)
}

// InfoFields writes an informational message with deterministically ordered
// operation fields, for example task_id, operation_id, or source_key.
func InfoFields(message string, fields Fields) {
	Default().log(LevelInfo, 3, "%s%s", message, formatFields(fields))
}

func InfoFieldsAt(extra int, message string, fields Fields) {
	Default().log(LevelInfo, 3+extra, "%s%s", message, formatFields(fields))
}

// WarnFields writes a warning message with deterministically ordered fields.
func WarnFields(message string, fields Fields) {
	Default().log(LevelWarn, 3, "%s%s", message, formatFields(fields))
}

// ErrorFields writes an error message with deterministically ordered fields.
func ErrorFields(message string, fields Fields) {
	Default().log(LevelError, 3, "%s%s", message, formatFields(fields))
}

func formatFields(fields Fields) string {
	if len(fields) == 0 {
		return ""
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		value := fmt.Sprint(fields[key])
		switch strings.ToLower(key) {
		case "authorization", "cookie", "password", "token", "access_token", "api_key", "apikey", "secret":
			value = "[redacted]"
		}
		parts = append(parts, fmt.Sprintf("%s=%q", key, DiagnosticText(value, 2048)))
	}
	return " fields{" + strings.Join(parts, ",") + "}"
}

func (l *Logger) log(level Level, skip int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.writeInternal(level, msg, skip)
}

// Write 写入一条日志（公共方法，旧 API 兼容）
func (l *Logger) Write(level Level, msg string) {
	l.writeInternal(level, msg, 2)
}

func (l *Logger) WriteChecked(level Level, msg string, fields Fields) error {
	return l.writeInternal(level, msg+formatFields(fields), 2)
}

func (l *Logger) writeInternal(level Level, msg string, skip int) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// 级别过滤（与写盘同一把锁，避免 minLevel 数据竞争）
	if level < l.minLevel {
		return nil
	}

	now := time.Now()
	if l.health.SessionID == "" {
		var id [12]byte
		if _, err := rand.Read(id[:]); err == nil {
			l.health.SessionID = fmt.Sprintf("%x", id)
		} else {
			l.health.SessionID = fmt.Sprintf("%d-%d", os.Getpid(), now.UnixNano())
		}
	}
	ts := now.Format("2006-01-02 15:04:05.000")
	// 获取调用者信息
	_, file, line, ok := runtime.Caller(skip)
	caller := ""
	if ok {
		// 只保留文件名，不保留全路径
		file = filepath.Base(file)
		caller = fmt.Sprintf("%s:%d", file, line)
	}
	// 确保 msg 单行
	msg = strings.ReplaceAll(msg, "\r", " ")
	msg = strings.ReplaceAll(msg, "\n", " ")
	msg = DiagnosticText(msg, 16<<10)
	if l.health.LastWriteError != "" {
		msg += fmt.Sprintf(" log_writer_retry{previous_failures=%d}", l.health.WriteFailures)
	}
	msg = fmt.Sprintf("[session=%s seq=%d] %s", l.health.SessionID, l.seq+1, msg)
	var lineStr string
	if caller != "" {
		lineStr = fmt.Sprintf("[%s] [%s] [%s] %s\n", ts, level.String(), caller, msg)
	} else {
		lineStr = fmt.Sprintf("[%s] [%s] %s\n", ts, level.String(), msg)
	}
	err := l.rotateIfNeeded()
	if err == nil {
		_, err = l.currentFile.WriteString(lineStr)
	}
	if err != nil {
		l.health.WriteFailures++
		l.health.Unpersisted++
		l.health.LastWriteError = DiagnosticText(err.Error(), 2048)
		fmt.Fprintf(os.Stderr, "[applog] write failed: %v\n", err)
		if l.currentFile != nil {
			_ = l.currentFile.Close()
			l.currentFile = nil
		}
	} else {
		l.health.LastWriteError = ""
		l.health.LastWriteTS = now.UnixMilli()
	}
	// Failed writes stay available in memory and exports with persisted=false.
	_, message, _ := bracketField(msg)
	l.pushLocked(level, now, caller, strings.TrimPrefix(message, " "), err == nil)
	return err
}

// rotateIfNeeded 若当前日期变化则切换文件
func (l *Logger) rotateIfNeeded() error {
	now := time.Now()
	day := now.Format("2006-01-02")
	if day == l.currentDay && l.currentFile != nil {
		return nil
	}
	if l.currentFile != nil {
		_ = l.currentFile.Close()
		l.currentFile = nil
	}
	if err := os.MkdirAll(l.logDir, 0755); err != nil {
		return err
	}
	filename := filepath.Join(l.logDir, fmt.Sprintf("cczj-%s.log", day))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}

	// 检查文件是否为空，如果是空文件则写入UTF-8 BOM标记（Windows记事本兼容）
	info, err := f.Stat()
	if err == nil && info.Size() == 0 {
		_, err = f.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	if err != nil {
		_ = f.Close()
		return err
	}

	l.currentFile = f
	l.currentDay = day
	l.cleanOld()
	return nil
}

// cleanOld 删除超过 keepDays 的旧日志文件
func (l *Logger) cleanOld() {
	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return
	}
	now := time.Now()
	cutoff := now.AddDate(0, 0, -l.keepDays)
	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		name := fi.Name()
		if !strings.HasPrefix(name, "cczj-") || !strings.HasSuffix(name, ".log") {
			continue
		}
		part := strings.TrimPrefix(strings.TrimSuffix(name, ".log"), "cczj-")
		t, err := time.Parse("2006-01-02", part)
		if err != nil {
			continue
		}
		if t.Before(cutoff) {
			_ = os.Remove(filepath.Join(l.logDir, name))
		}
	}
}

// Close 关闭当前文件句柄
func (l *Logger) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.currentFile != nil {
		_ = l.currentFile.Close()
		l.currentFile = nil
	}
}

// Dir 返回日志目录
func (l *Logger) Dir() string { l.mu.Lock(); defer l.mu.Unlock(); return l.logDir }

// KeepDays 返回日志保留天数
func (l *Logger) KeepDays() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.keepDays
}

// SetKeepDays 改日志保留天数，并按新值立刻清理一次旧文件。
// cleanOld 不自己加锁（沿用 rotate 路径的约定），所以在这里持锁调用。
func SetKeepDays(days int) int {
	if days < 1 {
		days = 1
	}
	l := Default()
	if l == nil {
		return 0
	}
	l.mu.Lock()
	l.keepDays = days
	l.cleanOld()
	l.mu.Unlock()
	return days
}

// ListFiles 返回可用的日志文件名列表（按时间倒序）
func (l *Logger) ListFiles() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.listFilesLocked()
}

func (l *Logger) listFilesLocked() []string {
	files, err := os.ReadDir(l.logDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, fi := range files {
		if fi.IsDir() {
			continue
		}
		name := fi.Name()
		if strings.HasPrefix(name, "cczj-") && strings.HasSuffix(name, ".log") {
			names = append(names, name)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	return names
}

// ReadFile 返回指定日志文件的内容
// 参数 tailLines：只返回末尾 N 行（0 表示全部）
func (l *Logger) ReadFile(name string) (string, error) {
	clean := filepath.Base(name)
	p := filepath.Join(l.Dir(), clean)
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Clear 删除所有日志文件
func (l *Logger) Clear() int {
	count, _ := l.ClearChecked()
	return count
}

func (l *Logger) ClearChecked() (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	files := l.listFilesLocked()
	count := 0
	var failure error
	if l.currentFile != nil {
		failure = l.currentFile.Close()
		l.currentFile = nil
	}
	for _, name := range files {
		if err := os.Remove(filepath.Join(l.logDir, name)); err == nil {
			count++
		} else {
			failure = errors.Join(failure, err)
		}
	}
	failure = errors.Join(failure, l.rotateIfNeeded())
	if failure != nil {
		return count, failure
	}
	// 清空后内存时间线也归零，seq 保持单调以免前端出现重复序号
	for i := range l.ring {
		l.ring[i] = Record{}
	}
	l.head, l.count = 0, 0
	return count, nil
}
