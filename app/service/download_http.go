package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

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
