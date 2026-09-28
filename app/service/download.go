package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	downloadservice "cczjVideo/app/download"
	fileservice "cczjVideo/app/files"
	proxyservice "cczjVideo/app/proxy"
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

// directResumeAttempts 是单连接下载一轮失败后最多自动续传的次数（含第一轮）。
const directResumeAttempts = 4

// downloadDirectSingle 单连接下载（回退方案）。返回非 nil 表示这一轮中断了，
// 调用方可以拿着 .part 的尾部再来一轮；返回 nil 且状态为 done/cancelled/paused
// 表示不需要再来。
func (a *App) downloadDirectSingle(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total, existingBytes int64, referer string) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return fmt.Errorf("build request failed: %w", err)
	}
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
	if referer != "" {
		httpReq.Header.Set("Referer", referer)
	}
	if existingBytes > 0 {
		httpReq.Header.Set("Range", fmt.Sprintf("bytes=%d-", existingBytes))
	}

	resp, err := task.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	if existingBytes > 0 {
		if resp.StatusCode == http.StatusOK {
			// The upstream ignored Range. Restart from zero instead of appending a
			// full body to the partial file.
			resp.Body.Close()
			if err := os.Truncate(tmpPath, 0); err != nil {
				return fmt.Errorf("reset partial file failed: %w", err)
			}
			task.mu.Lock()
			task.status.Downloaded = 0
			task.mu.Unlock()
			return a.downloadDirectSingle(ctx, task, urlStr, tmpPath, total, 0, referer)
		}
		if _, rangeErr := validateResumeContent(resp, existingBytes, total); rangeErr != nil {
			resp.Body.Close()
			return fmt.Errorf("invalid resume response: %w", rangeErr)
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return fmt.Errorf("HTTP %d", resp.StatusCode)
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
		return fmt.Errorf("open file failed: %w", err)
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
			return nil
		default:
		}

		if task.isPaused() {
			out.Close()
			a.emitProgress(task)
			return nil
		}

		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				out.Close()
				return fmt.Errorf("write file failed: %w", werr)
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
			return fmt.Errorf("read failed: %w", rerr)
		}
	}

	if cerr := out.Close(); cerr != nil {
		return fmt.Errorf("close file failed: %w", cerr)
	}
	if total > 0 && downloaded != total {
		return fmt.Errorf("incomplete response: got %d of %d bytes", downloaded, total)
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
	return nil
}

func (a *App) downloadDirectParallel(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total int64, numConnections int, referer string) {
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
			if referer != "" {
				req.Header.Set("Referer", referer)
			}
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

// resolvePlaylist 拿到落盘顺序的片段列表。
//
// 优先用内存/持久化里带密钥与字节区间的版本；只剩纯 URL 的老任务（旧版本写的
// downloads.json）退回按明文整文件处理 —— 那正是旧版本的行为，不能假装知道密钥。
// 两者都没有才去抓播放列表。
func (a *App) resolvePlaylist(ctx context.Context, task *downloadTask, m3u8URL, referer string) ([]downloadservice.Segment, error) {
	task.mu.Lock()
	planned := append([]downloadservice.Segment(nil), task.playlist...)
	legacy := append([]string(nil), task.segments...)
	task.mu.Unlock()
	if len(planned) > 0 {
		return planned, nil
	}
	if len(legacy) > 0 {
		return downloadservice.SegmentsFromURLs(legacy), nil
	}

	playlist, err := downloadservice.HTTPGetText(ctx, task.httpClient, m3u8URL, referer)
	if err != nil {
		return nil, fmt.Errorf("fetch m3u8 failed: %w", err)
	}
	parsed := downloadservice.ParsePlaylist(m3u8URL, playlist)
	// 主列表：广告过滤后的第一条若还是 m3u8，它就是变体地址，跟着再解一层。
	if len(parsed.Segments) > 0 && downloadservice.IsHLSURL(parsed.Segments[0].URL) {
		variant := parsed.Segments[0].URL
		sub, subErr := downloadservice.HTTPGetText(ctx, task.httpClient, variant, referer)
		if subErr != nil {
			return nil, fmt.Errorf("fetch sub m3u8 failed: %w", subErr)
		}
		parsed = downloadservice.ParsePlaylist(variant, sub)
	}
	if len(parsed.Segments) == 0 {
		return nil, fmt.Errorf("no segments found in m3u8")
	}
	sequence, seqIssues := downloadservice.WriteSequence(parsed.Segments)
	issues := append(append([]string(nil), parsed.Unsupported...), seqIssues...)
	if len(issues) > 0 {
		// 拼接一份解不了的列表只会得到一个「大小正常但放不出来」的文件，用户等完
		// 一整场才发现，比下载前直接报错难查得多。
		return nil, fmt.Errorf("playlist uses unsupported tags: %s", strings.Join(issues, "; "))
	}
	task.mu.Lock()
	task.playlist = append([]downloadservice.Segment(nil), sequence...)
	task.segments = segmentURLs(parsed.Segments)
	task.mu.Unlock()
	a.savePersistedTasks()
	return sequence, nil
}

// segmentURLs 只取明文列表里的片段地址，不含下载序列插入的初始化段。
func segmentURLs(segments []downloadservice.Segment) []string {
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		if seg.Init {
			continue
		}
		out = append(out, seg.URL)
	}
	return out
}

// keyCache 按 URI 缓存 HLS 密钥。一个播放列表通常只有一把密钥，但密钥服务经常是
// 整条链路上最容易被打挂的一方，逐片取会把请求量翻几十倍。
//
// 5 个下载 worker 是同时起步的，所以光有缓存不够：未命中的那几个并发请求要收敛到
// 同一次取密钥上（single-flight），否则头几片照样会打出 5 次。
type keyCache struct {
	mu      sync.Mutex
	client  *http.Client
	referer string
	calls   map[string]*keyCall
}

type keyCall struct {
	done chan struct{}
	data []byte
	err  error
}

func (k *keyCache) get(ctx context.Context, uri string) ([]byte, error) {
	k.mu.Lock()
	call, pending := k.calls[uri]
	if !pending {
		call = &keyCall{done: make(chan struct{})}
		k.calls[uri] = call
	}
	k.mu.Unlock()

	if !pending {
		call.data, call.err = k.fetch(ctx, uri)
		close(call.done)
	} else {
		<-call.done
	}
	if call.err != nil {
		k.discard(uri, call)
		return nil, call.err
	}
	return call.data, nil
}

// discard 丢掉失败的这次调用：密钥服务整条链路最容易 5xx，把错误永久缓存下来会让
// 续传也解不开，而重试时密钥可能已经恢复。
func (k *keyCache) discard(uri string, call *keyCall) {
	k.mu.Lock()
	if k.calls[uri] == call {
		delete(k.calls, uri)
	}
	k.mu.Unlock()
}

func (k *keyCache) fetch(ctx context.Context, uri string) ([]byte, error) {
	data, err := downloadservice.Get(ctx, k.client, uri, k.referer, "", 4096)
	if err != nil {
		return nil, fmt.Errorf("fetch key failed: %w", err)
	}
	if len(data) != 16 {
		return nil, fmt.Errorf("key must be 16 bytes, got %d", len(data))
	}
	return data, nil
}

// fetchSegment 取一个片段并按需解密、按需只取字节区间。
func (a *App) fetchSegment(ctx context.Context, task *downloadTask, seg downloadservice.Segment, referer string, keys *keyCache, maxBytes int64) ([]byte, error) {
	rangeHeader := ""
	if seg.Range != nil {
		rangeHeader = seg.Range.Header()
	}
	data, err := downloadservice.Get(ctx, task.httpClient, seg.URL, referer, rangeHeader, maxBytes)
	if err != nil {
		return nil, err
	}
	if seg.Range != nil && int64(len(data)) != seg.Range.Length {
		return nil, fmt.Errorf("range returned %d bytes, want %d", len(data), seg.Range.Length)
	}
	if seg.Key == nil {
		return data, nil
	}
	key, keyErr := keys.get(ctx, seg.Key.URI)
	if keyErr != nil {
		return nil, keyErr
	}
	plain, decErr := downloadservice.DecryptAES128(key, downloadservice.SegmentIV(seg.Key, seg.Seq), data)
	if decErr != nil {
		return nil, fmt.Errorf("decrypt segment %d: %w", seg.Seq, decErr)
	}
	return plain, nil
}

func (a *App) failSegmentTask(task *downloadTask, err error) {
	task.setError(err.Error())
	// 进度落盘：失败位置之后的片段还没写，segIndex 停在断点上是用户点「继续」
	// 能接着下的唯一依据。
	a.savePersistedTasks()
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
	// 引用页取任务自己那条 URL 的域：不少 CDN 会拿它当鉴权依据，裸请求直接 403。
	referer := proxyservice.RefererOrigin(m3u8URL)

	segments, err := a.resolvePlaylist(ctx, task, m3u8URL, referer)
	if err != nil {
		a.failSegmentTask(task, err)
		return
	}

	task.mu.Lock()
	isResume := task.hasTotal && task.segIndex > 0
	task.mu.Unlock()

	var existingBytes int64
	if info, statErr := os.Stat(tmpPath); statErr == nil {
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
		a.failSegmentTask(task, fmt.Errorf("open file failed: %w", err))
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
	keys := &keyCache{client: task.httpClient, referer: referer, calls: make(map[string]*keyCall)}
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
					data, fetchErr := a.fetchSegment(runCtx, task, segments[index], referer, keys, maxDownloadSegmentSize)
					if fetchErr != nil {
						if runCtx.Err() == nil {
							select {
							case results <- segmentResult{index: index, err: fmt.Errorf("segment %d: %w", index+1, fetchErr)}:
							case <-runCtx.Done():
							}
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
				task.setSegIdx(nextExpected)
				a.failSegmentTask(task, result.err)
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
					a.failSegmentTask(task, fmt.Errorf("write segment failed: %w", writeErr))
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
		a.failSegmentTask(task, fmt.Errorf("close file failed: %w", err))
		return
	}
	if err := os.Rename(tmpPath, savePath); err != nil {
		a.failSegmentTask(task, fmt.Errorf("rename failed: %w", err))
		return
	}
	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.Total = downloaded
	task.status.Downloaded = downloaded
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	task.playlist = nil
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
