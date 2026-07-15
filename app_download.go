package main

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
	"sync/atomic"
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

	// ========== 3) 如果已有 total，则先在前端展示，避免 0/0 ==========
	if total > 0 {
		task.mu.Lock()
		task.status.Total = total
		task.status.Downloaded = existingBytes
		task.mu.Unlock()
	}

	// ========== 4) 判断走哪个分支：多连接并行 OR 单连接 ==========
	const numConnections = 6
	const minParallelSize = 2 * 1024 * 1024 // 小于 2MB 没必要并行
	remaining := total - existingBytes

	useParallel := supportsRange && total > 0 && remaining > minParallelSize

	if useParallel {
		a.downloadDirectParallel(ctx, task, urlStr, tmpPath, total, existingBytes, numConnections)
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
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		task.setError("HTTP " + strconv.Itoa(resp.StatusCode))
		a.emitProgress(task)
		return
	}

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
			task.setCancel()
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

// downloadDirectParallel 多连接并行下载（IDM 风格）
// 将文件分为 N 个区间，每个连接独立下载一个 Range，同时写入同一文件的不同 offset
func (a *App) downloadDirectParallel(ctx context.Context, task *downloadTask, urlStr, tmpPath string, total, existingBytes int64, numConnections int) {
	// ========== 1) 打开/创建输出文件（预分配大小以便 Seek） ==========
	out, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}
	// 预分配磁盘空间（避免多次扩缩开销）
	if total > 0 {
		_ = out.Truncate(total)
	}

	// ========== 2) 计算每个连接的字节区间 ==========
	remainingStart := existingBytes
	remainingBytes := total - remainingStart
	chunkSize := remainingBytes / int64(numConnections)
	if chunkSize <= 0 {
		chunkSize = 1
	}

	type chunkRange struct {
		id    int
		start int64
		end   int64 // inclusive
	}
	var chunks []chunkRange
	for i := 0; i < numConnections; i++ {
		start := remainingStart + int64(i)*chunkSize
		var end int64
		if i == numConnections-1 {
			end = total - 1 // 最后一块到文件尾
		} else {
			end = remainingStart + int64(i+1)*chunkSize - 1
		}
		if start < total {
			chunks = append(chunks, chunkRange{id: i, start: start, end: end})
		}
	}

	// ========== 3) 启动 N 个 worker ==========
	var (
		muProgress   sync.Mutex
		downloaded   = existingBytes
		lastEmit     = time.Now()
		lastBytes    = existingBytes
		emitInterval = 300 * time.Millisecond
		errCh        = make(chan error, numConnections)
		doneCh       = make(chan struct{})
		active       int32
		chunkDone    = make([]int64, len(chunks)) // 每个分块已下载字节
	)

	atomic.StoreInt32(&active, int32(len(chunks)))

	worker := func(chk chunkRange) {
		// 计算需要下载的字节数
		chunkTotal := chk.end - chk.start + 1
		if chunkTotal <= 0 {
			atomic.AddInt32(&active, -1)
			return
		}

		req, rerr := http.NewRequestWithContext(ctx, http.MethodGet, urlStr, nil)
		if rerr != nil {
			errCh <- rerr
			return
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-%d", chk.start, chk.end))

		resp, rerr := task.httpClient.Do(req)
		if rerr != nil {
			errCh <- rerr
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errCh <- fmt.Errorf("HTTP %d (chunk %d)", resp.StatusCode, chk.id)
			return
		}

		// 当前写入位置 = chk.start 起
		writeAt := chk.start
		buf := make([]byte, 256*1024) // 增大缓冲区提升吞吐

		for {
			if task.isPaused() {
				// 暂停：worker 退出，主循环检测到所有 worker 退出后退出整体
				// 下次 ResumeDownload 会从头续传（.part 文件已有部分内容）
				return
			}
			select {
			case <-ctx.Done():
				return
			default:
			}

			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := out.WriteAt(buf[:n], writeAt); werr != nil {
					errCh <- werr
					return
				}
				writeAt += int64(n)
				// 更新全局下载进度 & 分块进度
				muProgress.Lock()
				downloaded += int64(n)
				chunkDone[chk.id] += int64(n)
				cur := downloaded
				// 构建分块进度快照
				chunksSnap := make([]ChunkProgress, len(chunks))
				for i := range chunks {
					chunksSnap[i] = ChunkProgress{
						ID:    chunks[i].id,
						Start: chunks[i].start,
						End:   chunks[i].end,
						Done:  chunkDone[i],
					}
				}
				muProgress.Unlock()

				// 节流推送
				if time.Since(lastEmit) >= emitInterval {
					elapsed := time.Since(lastEmit).Seconds()
					var speed float64
					if elapsed > 0 {
						speed = float64(cur-lastBytes) / elapsed
					}
					task.mu.Lock()
					task.status.Downloaded = cur
					task.status.SpeedBps = speed
					task.status.Chunks = chunksSnap
					if total > 0 && speed > 0 {
						task.status.EtaSec = float64(total-cur) / speed
					}
					task.mu.Unlock()
					lastEmit = time.Now()
					lastBytes = cur
					a.emitProgress(task)
				}
			}
			if rerr != nil {
				if rerr == io.EOF {
					break
				}
				errCh <- rerr
				return
			}
		}

		atomic.AddInt32(&active, -1)
		if atomic.LoadInt32(&active) == 0 {
			close(doneCh)
		}
	}

	for _, chk := range chunks {
		go worker(chk)
	}

	// ========== 4) 主循环：等待完成 / 取消 / 暂停 / 错误 ==========
	for {
		select {
		case <-ctx.Done():
			out.Close()
			task.setCancel()
			a.emitProgress(task)
			return
		case <-doneCh:
			// 所有 chunk 完成
			if cerr := out.Close(); cerr != nil {
				task.setError("close file failed: " + cerr.Error())
				a.emitProgress(task)
				return
			}
			task.mu.Lock()
			task.status.Status = "done"
			task.status.EndTime = time.Now().Unix()
			task.status.SpeedBps = 0
			task.status.EtaSec = 0
			task.status.Total = total
			task.status.Downloaded = downloaded
			task.mu.Unlock()
			a.emitProgress(task)
			return
		case werr := <-errCh:
			out.Close()
			task.setError("parallel download failed: " + werr.Error())
			a.emitProgress(task)
			return
		default:
			// 周期性检测暂停
			if task.isPaused() {
				// 等待 worker 自然退出后再关闭文件
				out.Close()
				a.emitProgress(task)
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// downloadM3u8 解析并下载 m3u8 的所有分片，合并为一个 .ts 文件
// 核心改进：
//  1. 真正的 N 路并发下载（类似 IDM 思路），N 个 worker 并发下载分片
//  2. 续传时保持 total 估算不变，避免进度条跳变
//  3. 按分片索引顺序写入磁盘，支持任意顺序的并发返回
func (a *App) downloadM3u8(ctx context.Context, task *downloadTask, m3u8URL, savePath string) {
	tmpPath := savePath + ".part"
	const numWorkers = 5
	const totalLockAfter = 8 // 下载 8 个分片后锁定 total 估算，之后不再变动

	// ========== 1) 解析 m3u8（或使用缓存） ==========
	var segments []string
	task.mu.Lock()
	segments = task.segments
	hasCachedSegs := len(segments) > 0
	isResume := task.hasTotal && task.segIndex > 0
	task.mu.Unlock()

	if !hasCachedSegs {
		m3u8Text, err := downloadservice.HTTPGetText(ctx, task.httpClient, m3u8URL)
		if err != nil {
			task.setError("fetch m3u8 failed: " + err.Error())
			a.emitProgress(task)
			return
		}
		segments = downloadservice.ParseM3U8Segments(m3u8URL, m3u8Text)

		// 若首个 segment 也是 m3u8（多级 playlist），再深入一层
		if len(segments) > 0 && downloadservice.IsHLSURL(segments[0]) {
			subText, err := downloadservice.HTTPGetText(ctx, task.httpClient, segments[0])
			if err != nil {
				task.setError("fetch sub m3u8 failed: " + err.Error())
				a.emitProgress(task)
				return
			}
			segments = downloadservice.ParseM3U8Segments(segments[0], subText)
		}
		if len(segments) == 0 {
			task.setError("no segments found in m3u8")
			a.emitProgress(task)
			return
		}
		task.mu.Lock()
		task.segments = segments
		task.mu.Unlock()
	}

	totalSegs := len(segments)

	// ========== 2) 读取已下载字节数 / 起始分片索引 ==========
	var existingBytes int64 = 0
	if fi, err := os.Stat(tmpPath); err == nil {
		existingBytes = fi.Size()
	}

	startIdx := task.nextSegIdx()
	if startIdx >= totalSegs {
		startIdx = 0
	}

	// ========== 3) 打开文件（新建/追加） ==========
	flag := os.O_CREATE | os.O_WRONLY
	if existingBytes > 0 && startIdx > 0 {
		flag |= os.O_APPEND
	} else {
		flag |= os.O_TRUNC
		existingBytes = 0
		startIdx = 0
		isResume = false
	}
	out, err := os.OpenFile(tmpPath, flag, 0644)
	if err != nil {
		task.setError("open file failed: " + err.Error())
		a.emitProgress(task)
		return
	}

	// ========== 4) N 路并发下载 + 顺序写入 ==========
	type segResult struct {
		idx  int
		data []byte
		err  error
	}

	var (
		downloadedBytes = existingBytes
		lastEmit        = time.Now()
		lastBytes       = existingBytes
		emitInterval    = 300 * time.Millisecond
		// total 估算策略：
		//   - 续传：沿用已保存的 total（isResume=true），从不动它
		//   - 新下载：前 totalLockAfter 个分片里，用平均值估算 total；达到阈值后 total 锁定，
		//     后续不再更新 total，避免进度条数字跳动
		segSumBytes   float64 = 0
		segCountReady int64   = 0
		totalLocked   bool    = isResume // 续传时认为 total 已锁定
	)

	// 下载任务队列（每个 worker 从中取索引）
	idxCh := make(chan int, totalSegs)
	resultCh := make(chan segResult, totalSegs)
	// 取消信号：让所有 worker 停止
	stopCh := make(chan struct{})

	// worker 函数：并发下载分片
	worker := func() {
		for {
			select {
			case idx, ok := <-idxCh:
				if !ok {
					return
				}
				// 检查取消 / 暂停
				if task.isPaused() {
					resultCh <- segResult{idx: idx, err: nil, data: nil} // 空数据表示被暂停
					continue
				}
				select {
				case <-ctx.Done():
					return
				case <-stopCh:
					return
				default:
				}

				seg := segments[idx]
				req, err := http.NewRequestWithContext(ctx, http.MethodGet, seg, nil)
				if err != nil {
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d request: %w", idx+1, err)}
					continue
				}
				req.Header.Set("User-Agent", "Mozilla/5.0 CCZJ-Video-Downloader/1.0")
				resp, err := task.httpClient.Do(req)
				if err != nil {
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d do: %w", idx+1, err)}
					continue
				}
				if resp.StatusCode < 200 || resp.StatusCode >= 300 {
					resp.Body.Close()
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d HTTP %d", idx+1, resp.StatusCode)}
					continue
				}
				data, rerr := io.ReadAll(resp.Body)
				resp.Body.Close()
				if rerr != nil {
					resultCh <- segResult{idx: idx, err: fmt.Errorf("seg %d read: %w", idx+1, rerr)}
					continue
				}
				resultCh <- segResult{idx: idx, data: data, err: nil}

			case <-ctx.Done():
				return
			case <-stopCh:
				return
			}
		}
	}

	// 启动 N 个 worker
	for w := 0; w < numWorkers; w++ {
		go worker()
	}

	// 把分片索引发给 workers（从 startIdx 开始）
	go func() {
		for i := startIdx; i < totalSegs; i++ {
			select {
			case idxCh <- i:
			case <-ctx.Done():
				return
			case <-stopCh:
				return
			}
		}
		// 注意：不关闭 idxCh，避免 worker 收到已关闭的 channel panic
		// 由主循环根据已处理数量判断是否结束
	}()

	// 主循环：顺序写入
	// 用 pending map 暂存乱序到达的分片数据
	// 下一个期望写入的分片索引 = nextExpected
	pending := make(map[int][]byte)
	nextExpected := startIdx
	remaining := totalSegs - startIdx
	processed := 0

	for processed < remaining {
		select {
		case <-ctx.Done():
			// 取消：清理并退出
			close(stopCh)
			out.Close()
			task.setCancel()
			a.emitProgress(task)
			return
		case result, ok := <-resultCh:
			if !ok {
				close(stopCh)
				out.Close()
				task.setError("result channel closed unexpectedly")
				a.emitProgress(task)
				return
			}

			// 检查暂停：任意分片下载时检测到暂停就终止本轮
			if task.isPaused() {
				close(stopCh)
				task.setSegIdx(nextExpected)
				out.Close()
				a.emitProgress(task)
				return
			}

			if result.err != nil {
				close(stopCh)
				out.Close()
				task.setError(result.err.Error())
				a.emitProgress(task)
				return
			}

			// 结果到达；要么立即写入（若正好是 nextExpected），要么暂存
			if result.idx == nextExpected {
				if result.data != nil && len(result.data) > 0 {
					if _, werr := out.Write(result.data); werr != nil {
						close(stopCh)
						out.Close()
						task.setError("write segment failed: " + werr.Error())
						a.emitProgress(task)
						return
					}
					segSize := float64(len(result.data))
					downloadedBytes += int64(segSize)
					// 仅在 total 未锁定时累计用于估算
					if !totalLocked {
						segSumBytes += segSize
						segCountReady++
					}
				}
				nextExpected++
				processed++

				// 尝试把 pending 中连续的分片也写出去
				for {
					if data, exists := pending[nextExpected]; exists {
						if data != nil && len(data) > 0 {
							if _, werr := out.Write(data); werr != nil {
								close(stopCh)
								out.Close()
								task.setError("write segment failed: " + werr.Error())
								a.emitProgress(task)
								return
							}
							segSize := float64(len(data))
							downloadedBytes += int64(segSize)
							if !totalLocked {
								segSumBytes += segSize
								segCountReady++
							}
						}
						delete(pending, nextExpected)
						nextExpected++
						processed++
					} else {
						break
					}
				}

				// total 估算逻辑：
				//   1. 若已锁定，绝对不动 total
				//   2. 否则累计到 totalLockAfter 个分片后锁定；锁定前也可以粗略给用户展示
				if !totalLocked && segCountReady >= totalLockAfter {
					avgSeg := segSumBytes / float64(segCountReady)
					estTotal := int64(avgSeg * float64(totalSegs))
					if estTotal < downloadedBytes {
						estTotal = downloadedBytes
					}
					task.mu.Lock()
					task.status.Total = estTotal
					task.hasTotal = true
					task.mu.Unlock()
					totalLocked = true // 锁定后不再更新 total
				} else if !totalLocked && segCountReady >= 3 {
					// 锁定前粗略估算（每 3 个分片更新一次，避免 1 个分片时 total 变化过大）
					avgSeg := segSumBytes / float64(segCountReady)
					estTotal := int64(avgSeg * float64(totalSegs))
					if estTotal < downloadedBytes {
						estTotal = downloadedBytes
					}
					task.mu.Lock()
					task.status.Total = estTotal
					task.hasTotal = true
					task.mu.Unlock()
				}

				// 更新 downloaded
				task.mu.Lock()
				task.status.Downloaded = downloadedBytes
				task.segIndex = nextExpected
				task.mu.Unlock()

				// 节流推送进度
				if time.Since(lastEmit) >= emitInterval {
					elapsed := time.Since(lastEmit).Seconds()
					var speed float64
					if elapsed > 0 {
						speed = float64(downloadedBytes-lastBytes) / elapsed
					}
					// 续传场景也可以计算 eta（使用已保存的 total）
					var eta float64
					task.mu.Lock()
					totalForEta := task.status.Total
					task.status.SpeedBps = speed
					if totalForEta > 0 && speed > 0 && totalForEta > downloadedBytes {
						eta = float64(totalForEta-downloadedBytes) / speed
					}
					task.status.EtaSec = eta
					task.mu.Unlock()
					lastEmit = time.Now()
					lastBytes = downloadedBytes
					a.emitProgress(task)
				}

				// 定期持久化（每 20 个分片）
				if processed%20 == 0 {
					a.savePersistedTasks()
				}
			} else {
				// 乱序到达的分片：暂存到 pending
				if result.data != nil {
					pending[result.idx] = result.data
				} else {
					// 空结果 = 被暂停的分片标记：主循环终止
					close(stopCh)
					task.setSegIdx(nextExpected)
					out.Close()
					a.emitProgress(task)
					return
				}
			}
		}
	}

	close(stopCh)

	// ========== 5) 完成 ==========
	if cerr := out.Close(); cerr != nil {
		task.setError("close file failed: " + cerr.Error())
		a.emitProgress(task)
		return
	}
	if rerr := os.Rename(tmpPath, savePath); rerr != nil {
		task.setError("rename failed: " + rerr.Error())
		a.emitProgress(task)
		return
	}

	task.mu.Lock()
	task.status.Status = "done"
	task.status.EndTime = time.Now().Unix()
	task.status.SpeedBps = 0
	task.status.EtaSec = 0
	task.status.Total = downloadedBytes
	task.status.Downloaded = downloadedBytes
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
	a.app.Event.Emit("download:progress", s)
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
