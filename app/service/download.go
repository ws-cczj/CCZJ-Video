package service

import (
	"cczjVideo/app/applog"
	downloadservice "cczjVideo/app/download"
	fileservice "cczjVideo/app/files"
	proxyservice "cczjVideo/app/proxy"
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

func (t *downloadTask) snapshot() VideoDownloadStatus {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := t.status
	return cp
}

func (a *App) runDownload(ctx context.Context, task *downloadTask) {
	defer task.httpClient.CloseIdleConnections()
	task.mu.Lock()
	urlStr := task.status.Url
	savePath := task.status.SavePath
	task.mu.Unlock()

	if downloadservice.IsHLSURL(urlStr) {
		a.downloadM3u8(ctx, task, urlStr, savePath)
		return
	}
	a.downloadDirect(ctx, task, urlStr, savePath)
}

func (a *App) downloadDirect(ctx context.Context, task *downloadTask, urlStr, savePath string) {
	tmpPath := savePath + ".part"
	referer := proxyservice.RefererOrigin(urlStr)

	// ========== 1) 探测文件大小 & Range 支持 ==========
	probeReq, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		probeReq, _ = http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	}
	probeReq.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	if referer != "" {
		probeReq.Header.Set("Referer", referer)
	}
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
	parallelManifest := false
	if total > 0 {
		if manifest, merr := loadDirectDownloadManifest(directManifestPath(tmpPath), urlStr, total); merr == nil {
			if fi, ferr := os.Stat(tmpPath); ferr == nil && fi.Size() >= manifest.requiredFileSize() {
				parallelManifest = true
				existingBytes = manifest.downloaded()
			} else {
				// A manifest without its matching sparse file cannot be trusted. Start
				// over rather than claiming bytes that are not on disk.
				_ = os.Remove(directManifestPath(tmpPath))
				_ = os.Truncate(tmpPath, 0)
				existingBytes = 0
			}
		} else if !os.IsNotExist(merr) {
			// Corrupt or stale metadata is never used to resume a sparse file.
			_ = os.Remove(directManifestPath(tmpPath))
			_ = os.Truncate(tmpPath, 0)
			existingBytes = 0
		}
	}
	if total > 0 && existingBytes >= total && !parallelManifest {
		// An old preallocated .part file is indistinguishable from a complete
		// file without its manifest. Never append to it.
		_ = os.Truncate(tmpPath, 0)
		existingBytes = 0
	}

	// ========== 3) 如果已有 total，则先在前端展示，避免 0/0 ==========
	if total > 0 {
		task.mu.Lock()
		task.status.Total = total
		task.status.Downloaded = existingBytes
		task.mu.Unlock()
	}
	if parallelManifest && existingBytes == total {
		a.finalizeDownload(task, tmpPath, savePath)
		return
	}

	// ========== 4) 判断走哪个分支：多连接并行 OR 单连接 ==========
	const numConnections = 6
	remaining := total - existingBytes
	if parallelManifest && !supportsRange {
		// 上游这次不认 Range 了（换 CDN 节点是常态）。稀疏 .part 的进度只有 manifest 说得出
		// 来，单连接只会拿文件大小当连续前缀往尾部追加：补够字节数就判定成功，把中间的空洞
		// 改名成成品。宁可整条重来，也不续写一个没法验证的文件。
		applog.Warn("下载 %s：稀疏文件还剩 %d 字节但上游不再支持 Range，从头开始", filepath.Base(savePath), remaining)
		_ = os.Remove(directManifestPath(tmpPath))
		_ = os.Truncate(tmpPath, 0)
		parallelManifest = false
		existingBytes = 0
		remaining = total
	}
	useParallel := useParallelDirectDownload(supportsRange, parallelManifest, remaining)

	if useParallel {
		a.downloadDirectParallel(ctx, task, urlStr, tmpPath, total, numConnections, referer)
	} else {
		// 长连接被 CDN 中途掐掉是常态，而 .part 的尾部就是续传点：只要这一轮真的往前
		// 写了字节，就退避后接着下，最多 directResumeAttempts 次。字节没动（404、上游
		// 不认 Range）说明不是抖动，立刻把错误交出去，别把一次失败拖成四次。
		for attempt := 0; ; attempt++ {
			err := a.downloadDirectSingle(ctx, task, urlStr, tmpPath, total, existingBytes, referer)
			if err == nil {
				break
			}
			snap := task.snapshot()
			info, statErr := os.Stat(tmpPath)
			if snap.Status == "paused" || snap.Status == "cancelled" || ctx.Err() != nil ||
				statErr != nil || attempt >= directResumeAttempts-1 || info.Size() <= existingBytes {
				task.setError(err.Error())
				a.checkpoint(task)
				a.emitProgress(task)
				return
			}
			applog.Warn("download interrupted, resuming from %d bytes: %v", info.Size(), err)
			existingBytes = info.Size()
			timer := time.NewTimer(time.Duration(500<<uint(attempt)) * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
		}
	}

	task.mu.Lock()
	complete := task.transferComplete
	task.mu.Unlock()
	if complete {
		a.finalizeDownload(task, tmpPath, savePath)
	}
}

func (t *downloadTask) setError(msg string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.paused || t.cancelRequested || t.persistenceFailed {
		return
	}
	t.status.Status = "error"
	t.status.Error = msg
	t.status.EndTime = time.Now().Unix()
}
func (t *downloadTask) setCancel() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.persistenceFailed {
		return
	}
	if t.cancelRequested {
		t.status.Status = "cancelled"
	} else {
		t.paused = true
		t.status.Status = "paused"
	}
	t.status.EndTime = time.Now().Unix()
}

// isPaused 原子读取暂停状态
func (t *downloadTask) isPaused() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.paused
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
	if a.downloads != nil {
		if current, ok := a.downloads.Get(s.TaskId); !ok || current != task {
			return
		}
	}
	if a.app != nil {
		a.app.Event.Emit("download:progress", s)
	}
}

// GetDownloadDir 返回默认下载目录
func (a *App) GetDownloadDir() string {
	return a.downloadDir.Get()
}

// SetDownloadDir 设置自定义下载目录（空字符串则重置为默认）
func (a *App) SetDownloadDir(dir string) string {
	resolved, err := a.downloadDir.Set(dir)
	if err != nil {
		applog.Warn("set download directory failed: %v", err)
	}
	return resolved
}

// OpenFileInExplorer 在系统文件管理器中打开该文件（定位到文件）
func (a *App) OpenFileInExplorer(path string) bool {
	if path == "" {
		return false
	}
	if !fileservice.IsWithin(path, a.getDataDir(), a.downloadDir.Get()) {
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
