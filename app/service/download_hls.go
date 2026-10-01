package service

import (
	downloadservice "cczjVideo/app/download"
	proxyservice "cczjVideo/app/proxy"
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

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
