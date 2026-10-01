package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	downloadservice "cczjVideo/app/download"
	fileservice "cczjVideo/app/files"
	"cczjVideo/app/netstats"
	proxyservice "cczjVideo/app/proxy"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"time"
)

func (a *App) StartVideoDownload(req VideoDownloadReq) (*VideoDownloadStatus, error) {
	if req.TaskId == "" {
		return nil, apperror.New(apperror.Validation, "task_id is empty")
	}
	if strings.TrimSpace(req.Url) == "" {
		return nil, apperror.New(apperror.Validation, "url is empty")
	}

	// ========== 重复下载检测：同一 URL 已有任务 ==========
	var duplicateId string
	for _, existing := range a.downloads.Snapshot() {
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
			if oldTask, ok := a.downloads.Remove(duplicateId); ok {
				oldTask.cancel()
				// 清理临时/目标文件
				oldTask.mu.Lock()
				sp := oldTask.status.SavePath
				oldTask.mu.Unlock()
				if sp != "" {
					_ = os.Remove(sp)
					_ = os.Remove(sp + ".part")
					_ = os.Remove(directManifestPath(sp + ".part"))
				}
			}
			a.savePersistedTasks()
		} else {
			return nil, apperror.New(apperror.DownloadDuplicate, "该链接已有下载任务，是否覆盖？")
		}
	}

	// 确定保存目录。只接受应用已授权的两棵树：数据目录与用户设定的下载目录。
	// 前端 save_dir 传回来的就是 downloadDir 本身，正常调用不受影响；这一层挡住的
	// 是扩展包或错误参数把目录树和文件写到任意位置（同一层守卫已用于 OpenFolder、
	// OpenFileInExplorer）。
	dir := strings.TrimSpace(req.SaveDir)
	if dir == "" {
		dir = a.downloadDir.Get()
	} else if !fileservice.IsWithin(dir, a.getDataDir(), a.downloadDir.Get()) {
		return nil, apperror.New(apperror.Validation, "保存目录不在应用管理范围内")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "create download dir failed")
	}

	filename := sanitizeFilename(req.Filename)
	filename = ensureFilenameExt(filename, req.Url)
	savePath := filepath.Join(dir, filename)

	// 避免重复任务
	if t, ok := a.downloads.Get(req.TaskId); ok {
		s := t.snapshot()
		return &s, nil
	}
	ctx, cancel := context.WithCancel(a.background.Context())
	task := &downloadTask{
		cancel:   cancel,
		paused:   false,
		segIndex: 0,
		isM3u8:   downloadservice.IsHLSURL(req.Url),
		httpClient: &http.Client{
			Timeout: 4 * time.Hour, // 大文件限制 4 小时，防止无限挂起
			Transport: netstats.WrapTransport(netstats.CategoryDownload, &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
			}),
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
	if !a.downloads.Add(req.TaskId, task) {
		existing, _ := a.downloads.Get(req.TaskId)
		s := existing.snapshot()
		return &s, nil
	}

	a.background.Go("download:"+req.TaskId, func(_ context.Context) {
		a.runDownload(ctx, task)
	})

	a.savePersistedTasks()
	applog.InfoFields("download queued", applog.Fields{
		"task_id":   req.TaskId,
		"transport": map[bool]string{true: "hls", false: "http"}[task.isM3u8],
	})
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
	if downloadservice.IsHLSURL(urlStr) {
		a.downloadM3u8(ctx, task, urlStr, savePath)
		return
	}
	a.downloadDirect(ctx, task, urlStr, savePath)
}

// isM3u8URL 检测是否为 m3u8 播放列表
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
		task.mu.Lock()
		task.status.Status = "done"
		task.status.EndTime = time.Now().Unix()
		task.status.SpeedBps = 0
		task.status.EtaSec = 0
		task.mu.Unlock()
		a.emitProgress(task)
		if rerr := os.Rename(tmpPath, savePath); rerr != nil {
			task.setError("rename failed: " + rerr.Error())
			a.emitProgress(task)
			return
		}
		_ = os.Remove(directManifestPath(tmpPath))
		a.savePersistedTasks()
		return
	}

	// ========== 4) 判断走哪个分支：多连接并行 OR 单连接 ==========
	const numConnections = 6
	const minParallelSize = 2 * 1024 * 1024 // 小于 2MB 没必要并行
	remaining := total - existingBytes

	// Legacy .part files do not carry a range manifest. Resume those through
	// one verified connection; only new or manifest-backed tasks may be sparse.
	useParallel := supportsRange && total > 0 && remaining > minParallelSize && (existingBytes == 0 || parallelManifest)

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
				a.savePersistedTasks()
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

	// ========== 5) 完成（如果内部函数没有处理）==========
	if task.snapshot().Status == "done" {
		if rerr := os.Rename(tmpPath, savePath); rerr != nil {
			task.setError("rename failed: " + rerr.Error())
			a.emitProgress(task)
			return
		}
		_ = os.Remove(directManifestPath(tmpPath))
		a.savePersistedTasks()
	}
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
	if a.app != nil {
		a.app.Event.Emit("download:progress", s)
	}
}

// GetDownloadProgress 查询单个任务状态
func (a *App) GetDownloadProgress(taskId string) (*VideoDownloadStatus, error) {
	t, ok := a.downloads.Get(taskId)
	if !ok {
		return nil, apperror.Newf(apperror.NotFound, "task not found: %s", taskId)
	}
	s := t.snapshot()
	return &s, nil
}

// ListDownloads 返回所有任务快照
func (a *App) ListDownloads() []VideoDownloadStatus {
	tasks := a.downloads.Snapshot()
	out := make([]VideoDownloadStatus, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.snapshot())
	}
	return out
}

// CancelDownload 取消一个下载任务
func (a *App) CancelDownload(taskId string) bool {
	t, ok := a.downloads.Get(taskId)
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
		TaskId:         t.status.TaskId,
		Url:            t.status.Url,
		Filename:       t.status.Filename,
		SavePath:       t.status.SavePath,
		Total:          t.status.Total,
		Downloaded:     t.status.Downloaded,
		Status:         t.status.Status,
		SegIndex:       t.segIndex,
		IsM3u8:         t.isM3u8,
		Segments:       t.segments,
		SegmentDetails: t.playlist,
		StartTime:      t.status.StartTime,
		ErrorMsg:       t.status.Error,
		HasTotal:       t.hasTotal,
	}
}

// savePersistedTasks 把当前所有下载任务写入磁盘 JSON
func (a *App) savePersistedTasks() {
	if a.downloads == nil {
		return
	}
	tasks := a.downloads.Snapshot()
	items := make([]persistedTask, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, a.toPersisted(t))
	}

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
			// 有结构化列表就用它：断点位置是按它编号的，只拿 URL 列表会把加密流当成明文。
			playlist: it.SegmentDetails,
			isM3u8:   it.IsM3u8,
			hasTotal: it.HasTotal,
			paused:   true,
			httpClient: &http.Client{
				Timeout: 4 * time.Hour, // 大文件限制 4 小时，防止无限挂起
				Transport: netstats.WrapTransport(netstats.CategoryDownload, &http.Transport{
					MaxIdleConns:        10,
					IdleConnTimeout:     60 * time.Second,
					TLSHandshakeTimeout: 15 * time.Second,
				}),
			},
		}
		a.downloads.Add(it.TaskId, task)
	}
}

// ======================== 下载控制 ========================

// PauseDownload 暂停一个下载任务
func (a *App) PauseDownload(taskId string) bool {
	t, ok := a.downloads.Get(taskId)
	if !ok {
		return false
	}
	s := t.snapshot()
	if s.Status != "downloading" && s.Status != "queued" {
		return false
	}
	t.setPaused(true)
	t.mu.Lock()
	cancel := t.cancel
	t.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	a.emitProgress(t)
	a.savePersistedTasks()
	return true
}

// ResumeDownload 恢复一个暂停或失败的下载任务
func (a *App) ResumeDownload(taskId string) bool {
	t, ok := a.downloads.Get(taskId)
	if !ok {
		return false
	}
	s := t.snapshot()
	// 失败的任务也走这里：自动重试打到上限后状态停在 error，而 .part 和断点索引还在，
	// 用户点「重试」就是接着下，没有理由要求他重启应用才能救回一次下载。
	if s.Status != "paused" && s.Status != "error" {
		return false
	}
	// 更新状态 + 启动新的 goroutine 继续下载
	t.setPaused(false)
	t.mu.Lock()
	t.status.Error = ""
	t.mu.Unlock()
	// 为新的下载周期创建新的 context（因为之前的可能已被 cancel 关联）
	ctx, cancel := context.WithCancel(a.background.Context())
	t.mu.Lock()
	t.cancel = cancel
	isM3u8 := t.isM3u8
	urlStr := t.status.Url
	savePath := t.status.SavePath
	t.mu.Unlock()

	if isM3u8 {
		a.background.Go("download:"+taskId, func(_ context.Context) {
			a.downloadM3u8(ctx, t, urlStr, savePath)
		})
	} else {
		a.background.Go("download:"+taskId, func(_ context.Context) {
			a.downloadDirect(ctx, t, urlStr, savePath)
		})
	}
	a.emitProgress(t)
	return true
}

// RemoveDownload 从列表移除任务记录（不会删除已下载文件）
func (a *App) RemoveDownload(taskId string) bool {
	if t, ok := a.downloads.Remove(taskId); ok {
		if t != nil && t.cancel != nil {
			t.cancel()
		}
		if t != nil {
			s := t.snapshot()
			_ = os.Remove(directManifestPath(s.SavePath + ".part"))
		}
		a.savePersistedTasks()
		return true
	}
	return false
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
