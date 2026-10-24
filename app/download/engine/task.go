package engine

import (
	downloadservice "cczjVideo/app/download"
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"
)

// Request 前端发起的下载请求
type Request struct {
	TaskId   string `json:"task_id"`
	Url      string `json:"url"`
	Filename string `json:"filename"` // 不含目录的文件名（含扩展名）
	SaveDir  string `json:"save_dir"` // 可选：自定义保存目录
	Force    bool   `json:"force"`    // 强制覆盖已有任务（用于重复下载确认后）
}

// Status 下载状态（用于 Progress 与事件推送）
type Status struct {
	TaskId     string          `json:"task_id"`
	Url        string          `json:"url"`
	Filename   string          `json:"filename"`
	SavePath   string          `json:"save_path"`
	Total      int64           `json:"total"`
	Downloaded int64           `json:"downloaded"`
	SpeedBps   float64         `json:"speed_bps"`
	EtaSec     float64         `json:"eta_sec"`
	Status     string          `json:"status"` // queued / downloading / paused / done / error / cancelled
	Error      string          `json:"error,omitempty"`
	StartTime  int64           `json:"start_time"`
	EndTime    int64           `json:"end_time,omitempty"`
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
	operation         context.Context
	mu                sync.Mutex
	status            Status
	run               *downloadservice.Run // protected by Engine.mu
	cancelRequested   bool
	persistenceFailed bool
	transferComplete  bool
	finalizing        bool
	finalSize         int64
	boundarySynced    bool
	checkpointBytes   int64
	checkpointIndex   int
	httpClient        *http.Client
	paused            bool     // 是否处于暂停状态
	segIndex          int      // 下一个要下载的分片索引（m3u8 断点恢复用）
	segments          []string // m3u8 的分片 URL 列表
	// playlist 是带密钥/初始化段/字节区间的分片列表，落盘顺序。segments 只剩 URL 的
	// 老任务读不出这些，所以两者并存：有 playlist 用它，否则退回 segments。
	playlist []downloadservice.Segment
	isM3u8   bool // 是否 m3u8 任务
	hasTotal bool // 是否已估算出 total（续传时不再重算）
}

type persistedTask = downloadservice.Checkpoint

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
	// Windows rejects paths longer than this in practice, so cap the byte length
	// while keeping multibyte titles intact.
	const maxFilenameBytes = 180
	if len(out) > maxFilenameBytes {
		ext := filepath.Ext(out)
		// A name like "a." + 200 chars yields an extension longer than the cap;
		// keeping it would push the base slice below zero.
		if len(ext) >= maxFilenameBytes/2 {
			ext = ""
		}
		out = truncateUTF8(strings.TrimSuffix(out, ext), maxFilenameBytes-len(ext)) + ext
		// Truncation can re-expose a trailing dot or space, both illegal on Windows.
		out = strings.TrimRight(out, " .")
	}
	if out == "" {
		out = "video"
	}
	return out
}

// truncateUTF8 cuts to maxBytes on a rune boundary so a Chinese title is never
// left with half a character.
func truncateUTF8(s string, maxBytes int) string {
	if maxBytes <= 0 {
		return ""
	}
	if len(s) <= maxBytes {
		return s
	}
	for maxBytes > 0 && !utf8.RuneStart(s[maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
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
