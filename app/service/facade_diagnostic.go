package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

// ======================== 日志 ========================

// LogEntry 前端写入的日志条目
type LogEntry struct {
	Fields  map[string]string `json:"fields,omitempty"`
	Level   string            `json:"level"`   // INFO / WARN / ERROR
	Message string            `json:"message"` // 消息
	Source  string            `json:"source"`  // 可选：来源（组件/文件）
	Detail  string            `json:"detail"`  // 可选：详细堆栈或上下文
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
	level := applog.LevelInfo
	switch entry.Level {
	case "WARN", "warn", "warning":
		level = applog.LevelWarn
	case "ERROR", "error", "err":
		level = applog.LevelError
	}
	fields := applog.Fields{}
	for key, value := range entry.Fields {
		if len(fields) < 32 {
			fields[key] = value
		}
	}
	if err := applog.Default().WriteChecked(level, msg, fields); err != nil {
		return false, apperror.Wrap(apperror.Storage, err, "日志仅保留在内存，写入文件失败")
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
	n, err := applog.Default().ClearChecked()
	if err != nil {
		return n, apperror.Wrap(apperror.Storage, err, "部分日志未能清除")
	}
	return n, nil
}
