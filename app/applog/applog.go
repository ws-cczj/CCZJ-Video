package applog

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Level 日志等级
type Level int

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
type Logger struct {
	mu          sync.Mutex
	logDir      string
	currentFile *os.File
	currentDay  string // YYYY-MM-DD
	keepDays    int    // 保留最近多少天的日志
	minLevel    Level  // 最低输出级别，低于此级别的日志被丢弃
}

var defaultLogger *Logger
var once sync.Once

// Init 初始化默认 Logger
func Init(logDir string) error {
	var err error
	once.Do(func() {
		os.MkdirAll(logDir, 0755)
		defaultLogger = &Logger{
			logDir:   logDir,
			keepDays: 30,
		}
		err = defaultLogger.rotateIfNeeded()
		if err == nil {
			defaultLogger.cleanOld()
			defaultLogger.writeInternal(LevelInfo, "===== 应用启动 =====", 3)
		}
	})
	return err
}

// Default 返回已初始化的默认 logger
func Default() *Logger {
	if defaultLogger == nil {
		exe, _ := os.Executable()
		dir := filepath.Join(filepath.Dir(exe), "data", "applog")
		_ = Init(dir)
	}
	return defaultLogger
}

// 便捷方法（带调用位置）
func Debug(format string, args ...interface{}) { Default().log(LevelDebug, 3, format, args...) }
func Info(format string, args ...interface{})  { Default().log(LevelInfo, 3, format, args...) }
func Warn(format string, args ...interface{})  { Default().log(LevelWarn, 3, format, args...) }
func Error(format string, args ...interface{}) { Default().log(LevelError, 3, format, args...) }

func (l *Logger) log(level Level, skip int, format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	l.writeInternal(level, msg, skip+1)
}

// Write 写入一条日志（公共方法，旧 API 兼容）
func (l *Logger) Write(level Level, msg string) {
	l.writeInternal(level, msg, 3)
}

// SetMinLevel 设置最低日志级别，低于此级别的日志不会写入文件
func SetMinLevel(level Level) {
	if defaultLogger != nil {
		defaultLogger.minLevel = level
	}
}

// IsDebug 返回是否处于调试模式
func IsDebug() bool {
	return defaultLogger != nil && defaultLogger.minLevel <= LevelDebug
}

func (l *Logger) writeInternal(level Level, msg string, skip int) {
	// 级别过滤
	if level < l.minLevel {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := l.rotateIfNeeded(); err != nil {
		fmt.Fprintf(os.Stderr, "[applog] rotate failed: %v\n", err)
		return
	}
	if l.currentFile == nil {
		return
	}

	ts := time.Now().Format("2006-01-02 15:04:05.000")
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
	var lineStr string
	if caller != "" {
		lineStr = fmt.Sprintf("[%s] [%s] [%s] %s\n", ts, level.String(), caller, msg)
	} else {
		lineStr = fmt.Sprintf("[%s] [%s] %s\n", ts, level.String(), msg)
	}
	if _, err := l.currentFile.WriteString(lineStr); err != nil {
		fmt.Fprintf(os.Stderr, "[applog] write failed: %v\n", err)
	}
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
	filename := filepath.Join(l.logDir, fmt.Sprintf("cczj-%s.log", day))
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	
	// 检查文件是否为空，如果是空文件则写入UTF-8 BOM标记（Windows记事本兼容）
	info, err := f.Stat()
	if err == nil && info.Size() == 0 {
		_, _ = f.Write([]byte{0xEF, 0xBB, 0xBF})
	}
	
	l.currentFile = f
	l.currentDay = day
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
func (l *Logger) Dir() string { return l.logDir }

// ListFiles 返回可用的日志文件名列表（按时间倒序）
func (l *Logger) ListFiles() []string {
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
	p := filepath.Join(l.logDir, clean)
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// ReadFileTail 读取日志文件末尾指定行数
func (l *Logger) ReadFileTail(name string, tailLines int) (string, error) {
	full, err := l.ReadFile(name)
	if err != nil {
		return "", err
	}
	if tailLines <= 0 {
		return full, nil
	}
	lines := strings.Split(full, "\n")
	if len(lines) <= tailLines {
		return full, nil
	}
	return strings.Join(lines[len(lines)-tailLines:], "\n"), nil
}

// ReadFileTailBytes 读取文件末尾指定字节数
func (l *Logger) ReadFileTailBytes(name string, maxBytes int64) (string, error) {
	clean := filepath.Base(name)
	p := filepath.Join(l.logDir, clean)
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	size := info.Size()
	if size > maxBytes {
		if _, err := f.Seek(size-maxBytes, io.SeekStart); err != nil {
			return "", err
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// Clear 删除所有日志文件
func (l *Logger) Clear() int {
	files := l.ListFiles()
	count := 0
	for _, name := range files {
		if name == fmt.Sprintf("cczj-%s.log", l.currentDay) {
			l.mu.Lock()
			if l.currentFile != nil {
				_ = l.currentFile.Close()
				l.currentFile = nil
			}
			_ = os.Remove(filepath.Join(l.logDir, name))
			_ = l.rotateIfNeeded()
			l.mu.Unlock()
			count++
			continue
		}
		if err := os.Remove(filepath.Join(l.logDir, name)); err == nil {
			count++
		}
	}
	return count
}