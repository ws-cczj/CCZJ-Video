package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	downloadservice "cczjVideo/app/download"
	fileservice "cczjVideo/app/files"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

func (a *App) StartVideoDownload(req VideoDownloadReq) (*VideoDownloadStatus, error) {
	if req.TaskId == "" {
		return nil, fmt.Errorf("task_id is empty")
	}
	if strings.TrimSpace(req.Url) == "" {
		return nil, fmt.Errorf("url is empty")
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

	// 确定保存目录
	dir := strings.TrimSpace(req.SaveDir)
	if dir == "" {
		dir = a.downloadDir.Get()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create download dir failed: %w", err)
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
			Transport: &http.Transport{
				MaxIdleConns:        10,
				IdleConnTimeout:     60 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
			},
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
	defer func() {
		if task.snapshot().Status != "done" {
			// 保留 .part 文件，以便断点续传
		}
	}()

	// ========== 1) 探测文件大小 & Range 支持 ==========
	probeReq, err := http.NewRequestWithContext(ctx, http.MethodHead, urlStr, nil)
	if err != nil {
		probeReq, _ = http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	}
	probeReq.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
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
		a.downloadDirectParallel(ctx, task, urlStr, tmpPath, total, numConnections)
	} else {
		a.downloadDirectSingle(ctx, task, urlStr, tmpPath, total, existingBytes)
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

// downloadDirectSingle 单连接下载（回退方案）
func (a *App) downloadDirectSingle(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total, existingBytes int64) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		task.setError("build request failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	if existingBytes > 0 {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingBytes))
	}

	resp, err := task.httpClient.Do(httpReq)
	if err != nil {
		task.setError("request failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	if existingBytes > 0 {
		if resp.StatusCode == http.StatusOK {
			// The upstream ignored Range. Restart from zero instead of appending a
			// full body to the partial file.
			resp.Body.Close()
			if err := os.Truncate(tmpPath, 0); err != nil {
				task.setError("reset partial file failed: " + err.Error())
				a.emitProgress(task)
				return
			}
			task.mu.Lock()
			task.status.Downloaded = 0
			task.mu.Unlock()
			a.downloadDirectSingle(ctx, task, urlStr, tmpPath, total, 0)
			return
		}
		if _, rangeErr := validateResumeContent(resp, existingBytes, total); rangeErr != nil {
			resp.Body.Close()
			task.setError("invalid resume response: " + rangeErr.Error())
			a.emitProgress(task)
			return
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		task.setError("HTTP " + strconv.Itoa(resp.StatusCode))
		a.emitProgress(task)
		return
	}
	defer resp.Body.Close()

	// 更新 total（如果响应里有）
	if resp.ContentLength > 0 {
		computedTotal := resp.ContentLength + existingBytes
		if computedTotal > total {
			total = computedTotal
		}
	} else if cr := resp.Header.Get("Content-Range"); cr != "" {
		if slash := strings.LastIndex(cr, "/"); slash >= 0 {
			if v, perr := strconv.ParseInt(strings.TrimSpace(cr[slash+1:]), 10, 64); perr == nil {
				total = v
			}
		}
	}
	if total > 0 {
		task.mu.Lock()
		task.status.Total = total
		task.mu.Unlock()
	}

	// 打开文件
	flag := os.O_CREATE | os.O_WRONLY
	if existingBytes > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
	}
	out, err := os.OpenFile(tmpPath, flag, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}

	var (
		buf          = make([]byte, 128*1024)
		downloaded   = existingBytes
		lastEmit     = time.Now()
		lastBytes    = existingBytes
		emitInterval = 300 * time.Millisecond
	)

	for {
		select {
		case <-ctx.Done():
			out.Close()
			if !task.isPaused() {
				task.setCancel()
			}
			a.emitProgress(task)
			return
		default:
		}

		if task.isPaused() {
			out.Close()
			a.emitProgress(task)
			return
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				task.setError("write file failed: " + werr.Error())
				a.emitProgress(task)
				return
			}
			downloaded += int64(n)
			task.mu.Lock()
			task.status.Downloaded = downloaded
			task.mu.Unlock()

			if time.Since(lastEmit) >= emitInterval {
				elapsed := time.Since(lastEmit).Seconds()
				var speed float64
				if elapsed > 0 {
					speed = float64(downloaded-lastBytes) / elapsed
				}
				task.mu.Lock()
				task.status.SpeedBps = speed
				if total > 0 && speed > 0 {
					task.status.EtaSec = float64(total-downloaded) / speed
				}
				task.mu.Unlock()
				lastEmit = time.Now()
				lastBytes = downloaded
				a.emitProgress(task)
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				break
			}
			out.Close()
			task.setError("read failed: " + rerr.Error())
			a.emitProgress(task)
			return
		}
	}

	if cerr := out.Close(); cerr != nil {
		task.setError("close file failed: " + cerr.Error())
		a.emitProgress(task)
		return
	}
	if total > 0 && downloaded != total {
		task.setError(fmt.Sprintf("incomplete response: got %d of %d bytes", downloaded, total))
		a.emitProgress(task)
		return
	}

	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	if task.status.Total <= 0 {
		task.status.Total = downloaded
	}
	task.status.Downloaded = downloaded
	task.mu.Unlock()
	a.emitProgress(task)
}

func (a *App) downloadDirectParallel(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total int64, numConnections int) {
	manifestPath := directManifestPath(tmpPath)
	manifest, err := loadDirectDownloadManifest(manifestPath, urlStr, total)
	if err != nil {
		if !os.IsNotExist(err) {
			task.setError("load download manifest failed: " + err.Error())
			a.emitProgress(task)
			return
		}
		manifest, err = newDirectDownloadManifest(urlStr, total, numConnections)
		if err != nil {
			task.setError(err.Error())
			a.emitProgress(task)
			return
		}
		if err := saveDirectDownloadManifest(manifestPath, manifest); err != nil {
			task.setError("save download manifest failed: " + err.Error())
			a.emitProgress(task)
			return
		}
	} else if fi, statErr := os.Stat(tmpPath); statErr != nil || fi.Size() < manifest.requiredFileSize() {
		task.setError("parallel download data does not match its manifest")
		a.emitProgress(task)
		return
	}

	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	defer out.Close()

	var stateMu sync.Mutex
	downloaded := manifest.downloaded()
	lastPersisted := downloaded
	lastEmit := time.Now()
	lastBytes := downloaded

	persistLocked := func() error {
		if err := saveDirectDownloadManifest(manifestPath, manifest.clone()); err != nil {
			return err
		}
		lastPersisted = downloaded
		return nil
	}
	updateProgress := func(force bool) {
		stateMu.Lock()
		if !force && time.Since(lastEmit) < 300*time.Millisecond {
			stateMu.Unlock()
			return
		}
		current := downloaded
		chunks := make([]ChunkProgress, len(manifest.Chunks))
		for i, chunk := range manifest.Chunks {
			chunks[i] = ChunkProgress{ID: i, Start: chunk.Start, End: chunk.End, Done: chunk.Done}
		}
		elapsed := time.Since(lastEmit).Seconds()
		speed := 0.0
		if elapsed > 0 {
			speed = float64(current-lastBytes) / elapsed
		}
		lastEmit = time.Now()
		lastBytes = current
		stateMu.Unlock()

		task.mu.Lock()
		task.status.Downloaded = current
		task.status.Total = total
		task.status.SpeedBps = speed
		task.status.Chunks = chunks
		if speed > 0 && total > current {
			task.status.EtaSec = float64(total-current) / speed
		}
		task.mu.Unlock()
		a.emitProgress(task)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, len(manifest.Chunks))
	var workers sync.WaitGroup
	for index := range manifest.Chunks {
		index := index
		workers.Add(1)
		go func() {
			defer workers.Done()
			stateMu.Lock()
			chunk := manifest.Chunks[index]
			stateMu.Unlock()
			start := chunk.Start + chunk.Done
			if start > chunk.End {
				return
			}
			wantBytes := chunk.End - start + 1
			req, reqErr := http.NewRequestWithContext(runCtx, http.MethodGet, urlStr, nil)
			if reqErr != nil {
				errCh <- reqErr
				cancel()
				return
			}
			req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
			req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", start, chunk.End))
			resp, respErr := task.httpClient.Do(req)
			if respErr != nil {
				if runCtx.Err() == nil {
					errCh <- respErr
					cancel()
				}
				return
			}
			defer resp.Body.Close()
			if rangeErr := validatePartialContent(resp, start, chunk.End, total); rangeErr != nil {
				errCh <- rangeErr
				cancel()
				return
			}

			reader := io.LimitReader(resp.Body, wantBytes+1)
			buffer := make([]byte, 256*1024)
			var written int64
			for written < wantBytes {
				readSize := len(buffer)
				if remaining := wantBytes - written; int64(readSize) > remaining {
					readSize = int(remaining)
				}
				n, readErr := reader.Read(buffer[:readSize])
				if n > 0 {
					if _, writeErr := out.WriteAt(buffer[:n], start+written); writeErr != nil {
						errCh <- writeErr
						cancel()
						return
					}
					written += int64(n)
					stateMu.Lock()
					manifest.Chunks[index].Done += int64(n)
					downloaded += int64(n)
					shouldPersist := downloaded-lastPersisted >= 1<<20 || manifest.Chunks[index].Done == manifest.Chunks[index].End-manifest.Chunks[index].Start+1
					var persistErr error
					if shouldPersist {
						persistErr = persistLocked()
					}
					stateMu.Unlock()
					if persistErr != nil {
						errCh <- fmt.Errorf("persist download progress: %w", persistErr)
						cancel()
						return
					}
					updateProgress(false)
				}
				if readErr != nil {
					if readErr == io.EOF && written == wantBytes {
						break
					}
					if runCtx.Err() == nil {
						errCh <- fmt.Errorf("read range: %w", readErr)
						cancel()
					}
					return
				}
			}
			// The extra byte exposed by LimitReader detects a body longer than the
			// range promised in Content-Range.
			extra := make([]byte, 1)
			if n, _ := reader.Read(extra); n != 0 {
				errCh <- fmt.Errorf("range response exceeds requested bytes")
				cancel()
				return
			}
			stateMu.Lock()
			persistErr := persistLocked()
			stateMu.Unlock()
			if persistErr != nil {
				errCh <- fmt.Errorf("persist completed range: %w", persistErr)
				cancel()
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		cancel()
		<-done
		if task.isPaused() {
			updateProgress(true)
			return
		}
		task.setCancel()
		a.emitProgress(task)
		return
	case downloadErr := <-errCh:
		cancel()
		<-done
		task.setError("parallel download failed: " + downloadErr.Error())
		a.emitProgress(task)
		return
	case <-done:
		select {
		case downloadErr := <-errCh:
			task.setError("parallel download failed: " + downloadErr.Error())
			a.emitProgress(task)
			return
		default:
		}
	}

	stateMu.Lock()
	complete := manifest.complete()
	persistErr := persistLocked()
	stateMu.Unlock()
	if persistErr != nil {
		task.setError("persist completed download failed: " + persistErr.Error())
		a.emitProgress(task)
		return
	}
	if !complete {
		task.setError("parallel download stopped before all ranges completed")
		a.emitProgress(task)
		return
	}
	if err := out.Close(); err != nil {
		task.setError("close file failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	updateProgress(true)
	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	task.status.Total = total
	task.status.Downloaded = total
	task.mu.Unlock()
	a.emitProgress(task)
}

func (a *App) downloadM3u8(ctx context.Context, task *downloadTask, m3u8URL, savePath string) {
	const (
		workers                = 5
		window                 = workers * 2
		totalLockAfter         = 8
		maxDownloadSegmentSize = 64 << 20
	)
	tmpPath := savePath + ".part"

	task.mu.Lock()
	segments := append([]string(nil), task.segments...)
	hasCachedSegments := len(segments) > 0
	isResume := task.hasTotal && task.segIndex > 0
	task.mu.Unlock()
	if !hasCachedSegments {
		playlist, err := downloadservice.HTTPGetText(ctx, task.httpClient, m3u8URL)
		if err != nil {
			task.setError("fetch m3u8 failed: " + err.Error())
			a.emitProgress(task)
			return
		}
		segments = downloadservice.ParseM3U8Segments(m3u8URL, playlist)
		if len(segments) > 0 && downloadservice.IsHLSURL(segments[0]) {
			playlist, err = downloadservice.HTTPGetText(ctx, task.httpClient, segments[0])
			if err != nil {
				task.setError("fetch sub m3u8 failed: " + err.Error())
				a.emitProgress(task)
				return
			}
			segments = downloadservice.ParseM3U8Segments(segments[0], playlist)
		}
		if len(segments) == 0 {
			task.setError("no segments found in m3u8")
			a.emitProgress(task)
			return
		}
		task.mu.Lock()
		task.segments = append([]string(nil), segments...)
		task.mu.Unlock()
	}

	var existingBytes int64
	if info, err := os.Stat(tmpPath); err == nil {
		existingBytes = info.Size()
	}
	startIdx := task.nextSegIdx()
	if startIdx < 0 || startIdx >= len(segments) {
		startIdx = 0
	}
	flags := os.O_CREATE | os.O_WRONLY
	if existingBytes > 0 && startIdx > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
		existingBytes = 0
		startIdx = 0
		isResume = false
	}
	out, err := os.OpenFile(tmpPath, flags, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	defer out.Close()

	type segmentResult struct {
		index int
		data  []byte
		err   error
	}
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan int, window)
	results := make(chan segmentResult, window)
	var wait sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for {
				select {
				case <-runCtx.Done():
					return
				case index, ok := <-jobs:
					if !ok {
						return
					}
					req, reqErr := http.NewRequestWithContext(runCtx, http.MethodGet, segments[index], nil)
					if reqErr != nil {
						select {
						case results <- segmentResult{index: index, err: reqErr}:
						case <-runCtx.Done():
						}
						continue
					}
					req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
					resp, fetchErr := task.httpClient.Do(req)
					if fetchErr != nil {
						if runCtx.Err() == nil {
							select {
							case results <- segmentResult{index: index, err: fmt.Errorf("segment %d: %w", index+1, fetchErr)}:
							case <-runCtx.Done():
							}
						}
						continue
					}
					if resp.StatusCode < 200 || resp.StatusCode >= 300 {
						resp.Body.Close()
						select {
						case results <- segmentResult{index: index, err: fmt.Errorf("segment %d HTTP %d", index+1, resp.StatusCode)}:
						case <-runCtx.Done():
						}
						continue
					}
					if resp.ContentLength > maxDownloadSegmentSize {
						resp.Body.Close()
						select {
						case results <- segmentResult{index: index, err: fmt.Errorf("segment %d exceeds size limit", index+1)}:
						case <-runCtx.Done():
						}
						continue
					}
					data, readErr := io.ReadAll(io.LimitReader(resp.Body, maxDownloadSegmentSize+1))
					resp.Body.Close()
					if readErr != nil || len(data) > maxDownloadSegmentSize {
						if readErr == nil {
							readErr = fmt.Errorf("segment exceeds size limit")
						}
						select {
						case results <- segmentResult{index: index, err: fmt.Errorf("read segment %d: %w", index+1, readErr)}:
						case <-runCtx.Done():
						}
						continue
					}
					select {
					case results <- segmentResult{index: index, data: data}:
					case <-runCtx.Done():
					}
				}
			}
		}()
	}
	var closeJobs sync.Once
	shutdown := func() {
		cancel()
		closeJobs.Do(func() { close(jobs) })
		wait.Wait()
	}
	defer shutdown()

	nextExpected := startIdx
	nextScheduled := startIdx
	schedule := func() bool {
		for nextScheduled < len(segments) && nextScheduled < nextExpected+window {
			select {
			case jobs <- nextScheduled:
				nextScheduled++
			case <-runCtx.Done():
				return false
			}
		}
		return true
	}
	if !schedule() {
		return
	}

	downloaded := existingBytes
	lastEmit := time.Now()
	lastBytes := downloaded
	segmentBytes := float64(0)
	segmentCount := int64(0)
	totalLocked := isResume
	pending := make(map[int][]byte, window)
	for nextExpected < len(segments) {
		select {
		case <-ctx.Done():
			shutdown()
			if task.isPaused() {
				task.setSegIdx(nextExpected)
				a.savePersistedTasks()
				a.emitProgress(task)
				return
			}
			task.setCancel()
			a.emitProgress(task)
			return
		case result := <-results:
			if result.err != nil {
				shutdown()
				task.setError(result.err.Error())
				a.emitProgress(task)
				return
			}
			pending[result.index] = result.data
			for {
				data, ready := pending[nextExpected]
				if !ready {
					break
				}
				if _, writeErr := out.Write(data); writeErr != nil {
					shutdown()
					task.setError("write segment failed: " + writeErr.Error())
					a.emitProgress(task)
					return
				}
				delete(pending, nextExpected)
				downloaded += int64(len(data))
				if !totalLocked {
					segmentBytes += float64(len(data))
					segmentCount++
				}
				nextExpected++
				task.setSegIdx(nextExpected)
			}
			if !totalLocked && segmentCount > 0 {
				estimated := int64(segmentBytes / float64(segmentCount) * float64(len(segments)))
				task.mu.Lock()
				task.status.Total = estimated
				if segmentCount >= totalLockAfter {
					task.hasTotal = true
					totalLocked = true
				}
				task.mu.Unlock()
			}
			if time.Since(lastEmit) >= 300*time.Millisecond {
				elapsed := time.Since(lastEmit).Seconds()
				speed := 0.0
				if elapsed > 0 {
					speed = float64(downloaded-lastBytes) / elapsed
				}
				task.mu.Lock()
				task.status.Downloaded = downloaded
				task.status.SpeedBps = speed
				if speed > 0 && task.status.Total > downloaded {
					task.status.EtaSec = float64(task.status.Total-downloaded) / speed
				}
				task.mu.Unlock()
				lastEmit = time.Now()
				lastBytes = downloaded
				a.emitProgress(task)
			}
			if nextExpected%20 == 0 {
				a.savePersistedTasks()
			}
			if !schedule() {
				return
			}
		}
	}
	shutdown()
	if err := out.Close(); err != nil {
		task.setError("close file failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	if err := os.Rename(tmpPath, savePath); err != nil {
		task.setError("rename failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.Total = downloaded
	task.status.Downloaded = downloaded
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	task.mu.Unlock()
	a.emitProgress(task)
	a.savePersistedTasks()
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
		return nil, fmt.Errorf("task not found: %s", taskId)
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
		TaskId:     t.status.TaskId,
		Url:        t.status.Url,
		Filename:   t.status.Filename,
		SavePath:   t.status.SavePath,
		Total:      t.status.Total,
		Downloaded: t.status.Downloaded,
		Status:     t.status.Status,
		SegIndex:   t.segIndex,
		IsM3u8:     t.isM3u8,
		Segments:   t.segments,
		StartTime:  t.status.StartTime,
		ErrorMsg:   t.status.Error,
		HasTotal:   t.hasTotal,
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
			isM3u8:   it.IsM3u8,
			hasTotal: it.HasTotal,
			paused:   true,
			httpClient: &http.Client{
				Timeout: 4 * time.Hour, // 大文件限制 4 小时，防止无限挂起
				Transport: &http.Transport{
					MaxIdleConns:        10,
					IdleConnTimeout:     60 * time.Second,
					TLSHandshakeTimeout: 15 * time.Second,
				},
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

// ResumeDownload 恢复一个暂停的下载任务
func (a *App) ResumeDownload(taskId string) bool {
	t, ok := a.downloads.Get(taskId)
	if !ok {
		return false
	}
	s := t.snapshot()
	if s.Status != "paused" {
		return false
	}
	// 更新状态 + 启动新的 goroutine 继续下载
	t.setPaused(false)
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
