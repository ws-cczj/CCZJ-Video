package service

import (
	"cczjVideo/app/netpolicy"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"cczjVideo/app/apperror"
	downloadservice "cczjVideo/app/download"
	fileservice "cczjVideo/app/files"
	"cczjVideo/app/netstats"
)

func downloadPathKey(path string) string {
	path = filepath.Clean(path)
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func runFinished(run *downloadservice.Run) bool {
	if run == nil {
		return true
	}
	select {
	case <-run.Done():
		return true
	default:
		return false
	}
}

func (a *App) launchDownloadLocked(t *downloadTask) error {
	run, err := a.downloadExecutor.Submit("download:"+t.snapshot().TaskId, func(ctx context.Context) {
		defer func() {
			if failure := recover(); failure != nil {
				t.setError(fmt.Sprintf("下载任务异常: %v", failure))
			}
			s := t.snapshot()
			if s.Status == "downloading" || s.Status == "queued" {
				t.setCancel()
			}
			if s.Status != "done" {
				a.checkpoint(t)
			}
			a.emitProgress(t)
		}()
		a.downloadMu.Lock()
		s := t.snapshot()
		if ctx.Err() != nil || s.Status != "queued" {
			a.downloadMu.Unlock()
			return
		}
		t.mu.Lock()
		t.status.Status = "downloading"
		t.httpClient = &http.Client{Timeout: 4 * time.Hour, Transport: netpolicy.WrapTransport(string(netstats.CategoryDownload), netstats.WrapTransport(netstats.CategoryDownload, &http.Transport{
			MaxIdleConns: 10, IdleConnTimeout: time.Minute, TLSHandshakeTimeout: 15 * time.Second,
		}))}
		if a.networkBudget != nil {
			t.httpClient.Transport = a.networkBudget.Wrap("download", t.httpClient.Transport)
		}
		t.mu.Unlock()
		err := a.persistDownloadLocked(t)
		if err != nil {
			a.persistenceErrorLocked(t, err)
		}
		a.downloadMu.Unlock()
		if err != nil {
			t.httpClient.CloseIdleConnections()
			return
		}
		a.runDownload(ctx, t)
	})
	if err != nil {
		t.setError("下载队列已满或应用正在退出，请稍后重试")
		if saveErr := a.persistDownloadLocked(t); saveErr != nil {
			a.persistenceErrorLocked(t, saveErr)
			return apperror.Wrap(apperror.Storage, saveErr, "下载状态保存失败")
		}
		return apperror.Wrap(apperror.Unavailable, err, "下载队列已满或应用正在退出")
	}
	t.run = run
	if a.downloadPaths == nil {
		a.downloadPaths = make(map[string]*downloadservice.Run)
	}
	a.downloadPaths[downloadPathKey(t.snapshot().SavePath)] = run
	return nil
}

func (a *App) StartVideoDownload(ctx context.Context, req VideoDownloadReq) (*VideoDownloadStatus, error) {
	a.modeSwitchMu.Lock()
	defer a.modeSwitchMu.Unlock()
	if a.currentMode() != "video" {
		return nil, apperror.New(apperror.Conflict, "请切换到视频模式后手动开始下载")
	}
	if strings.TrimSpace(req.TaskId) == "" {
		return nil, apperror.New(apperror.Validation, "task_id is empty")
	}
	parsed, err := url.Parse(strings.TrimSpace(req.Url))
	if err != nil || parsed.Hostname() == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, apperror.New(apperror.Validation, "下载地址必须是 HTTP 或 HTTPS")
	}
	dir := strings.TrimSpace(req.SaveDir)
	if dir == "" {
		dir = a.downloadDir.Get()
	}
	if !filepath.IsAbs(dir) || !fileservice.IsWithin(dir, a.getDataDir(), a.downloadDir.Get()) {
		return nil, apperror.New(apperror.Validation, "保存目录不在应用管理范围内")
	}
	filename := ensureFilenameExt(sanitizeFilename(req.Filename), req.Url)
	savePath := filepath.Join(dir, filename)
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	if err := a.downloadReadyLocked(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var duplicate *downloadTask
	for _, existing := range a.downloads.Snapshot() {
		s := existing.snapshot()
		if s.TaskId == req.TaskId || strings.TrimSpace(s.Url) == strings.TrimSpace(req.Url) || downloadPathKey(s.SavePath) == downloadPathKey(savePath) {
			if s.TaskId == req.TaskId && s.Url == req.Url && downloadPathKey(s.SavePath) == downloadPathKey(savePath) && !req.Force {
				return &s, nil
			}
			if duplicate != nil {
				return nil, apperror.New(apperror.Conflict, "多个任务占用该链接或文件，请先移除旧记录")
			}
			duplicate = existing
		}
	}
	if duplicate != nil {
		if !req.Force {
			return nil, apperror.New(apperror.DownloadDuplicate, "链接或目标文件已有下载任务，是否覆盖？")
		}
		old := duplicate.snapshot()
		if !fileservice.IsWithin(old.SavePath, a.getDataDir(), a.downloadDir.Get()) {
			return nil, apperror.New(apperror.Validation, "旧任务文件已不在应用管理范围，请手动处理")
		}
		cp := a.toPersisted(duplicate)
		cp.Status = "cancelled"
		if err := a.downloadStore.Save(ctx, cp); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "保存覆盖意图失败")
		}
		duplicate.mu.Lock()
		duplicate.cancelRequested = true
		duplicate.status.Status = "cancelled"
		duplicate.mu.Unlock()
		run := duplicate.run
		run.Cancel()
		a.downloadMu.Unlock()
		err := run.Wait(ctx)
		a.downloadMu.Lock()
		if err != nil {
			return nil, err
		}
		current, exists := a.downloads.Get(old.TaskId)
		if !exists || current != duplicate || current.run != run {
			return nil, apperror.New(apperror.Conflict, "旧任务已变化，请重新操作")
		}
		for _, path := range []string{old.SavePath, old.SavePath + ".part", directManifestPath(old.SavePath + ".part")} {
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return nil, apperror.Wrap(apperror.Storage, err, "清理覆盖文件失败")
			}
		}
		if err := a.downloadStore.Delete(ctx, old.TaskId); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "移除旧下载记录失败")
		}
		a.downloads.Remove(old.TaskId)
	}
	key := downloadPathKey(savePath)
	if run := a.downloadPaths[key]; !runFinished(run) {
		return nil, apperror.New(apperror.Conflict, "旧下载仍在释放文件，请稍后重试")
	}
	for path, run := range a.downloadPaths {
		if runFinished(run) {
			delete(a.downloadPaths, path)
		}
	}
	// Unregistered files also require explicit overwrite intent. A leftover .part
	// has no trustworthy URL identity after its task record has been removed.
	for _, path := range []string{savePath, savePath + ".part", directManifestPath(savePath + ".part")} {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "检查下载文件失败")
		}
		if !req.Force {
			return nil, apperror.New(apperror.DownloadDuplicate, "目标已有文件，是否覆盖？")
		}
		if err := os.Remove(path); err != nil {
			return nil, apperror.Wrap(apperror.Storage, err, "覆盖目标文件失败")
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, apperror.Wrap(apperror.Storage, err, "创建下载目录失败")
	}
	t := &downloadTask{isM3u8: downloadservice.IsHLSURL(req.Url), status: VideoDownloadStatus{
		TaskId: req.TaskId, Url: req.Url, Filename: filename, SavePath: savePath, Status: "queued", StartTime: time.Now().Unix(),
	}}
	a.downloads.Add(req.TaskId, t)
	if err := a.persistDownloadLocked(t); err != nil {
		a.downloads.Remove(req.TaskId)
		return nil, apperror.Wrap(apperror.Storage, err, "保存下载任务失败")
	}
	if err := a.launchDownloadLocked(t); err != nil {
		return nil, err
	}
	s := t.snapshot()
	return &s, nil
}

func (a *App) GetDownloadProgress(taskId string) (*VideoDownloadStatus, error) {
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	if err := a.downloadReadyLocked(); err != nil {
		return nil, err
	}
	t, ok := a.downloads.Get(taskId)
	if !ok {
		return nil, apperror.New(apperror.NotFound, "下载任务不存在")
	}
	s := t.snapshot()
	return &s, nil
}

func (a *App) ListDownloads() ([]VideoDownloadStatus, error) {
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	if err := a.downloadReadyLocked(); err != nil {
		return nil, err
	}
	out := make([]VideoDownloadStatus, 0)
	for _, t := range a.downloads.Snapshot() {
		out = append(out, t.snapshot())
	}
	return out, nil
}

func (a *App) stopDownload(ctx context.Context, id, state string) (bool, error) {
	a.downloadMu.Lock()
	if err := a.downloadReadyLocked(); err != nil {
		a.downloadMu.Unlock()
		return false, err
	}
	t, ok := a.downloads.Get(id)
	if !ok {
		a.downloadMu.Unlock()
		return false, nil
	}
	s := t.snapshot()
	if s.Status == "done" || s.Status == "cancelled" || (state == "paused" && s.Status != "queued" && s.Status != "downloading") {
		a.downloadMu.Unlock()
		return false, nil
	}
	cp := a.toPersisted(t)
	cp.Status, cp.EndTime = state, time.Now().Unix()
	if err := a.downloadStore.Save(ctx, cp); err != nil {
		a.downloadMu.Unlock()
		return false, apperror.Wrap(apperror.Storage, err, "保存下载控制状态失败")
	}
	t.mu.Lock()
	t.paused, t.cancelRequested = state == "paused", state == "cancelled"
	t.status.Status, t.status.EndTime = state, cp.EndTime
	t.status.SpeedBps, t.status.EtaSec = 0, 0
	t.mu.Unlock()
	run := t.run
	run.Cancel()
	a.downloadMu.Unlock()
	a.emitProgress(t)
	return true, run.Wait(ctx)
}

func (a *App) PauseDownload(ctx context.Context, taskId string) (bool, error) {
	return a.stopDownload(ctx, taskId, "paused")
}
func (a *App) CancelDownload(ctx context.Context, taskId string) (bool, error) {
	return a.stopDownload(ctx, taskId, "cancelled")
}

func (a *App) ResumeDownload(ctx context.Context, taskId string) (bool, error) {
	a.modeSwitchMu.Lock()
	defer a.modeSwitchMu.Unlock()
	if a.currentMode() != "video" {
		return false, apperror.New(apperror.Conflict, "请切换到视频模式后手动开始下载")
	}
	a.downloadMu.Lock()
	defer a.downloadMu.Unlock()
	if err := a.downloadReadyLocked(); err != nil {
		return false, err
	}
	t, ok := a.downloads.Get(taskId)
	if !ok {
		return false, nil
	}
	s := t.snapshot()
	if s.Status != "paused" && s.Status != "error" {
		return false, nil
	}
	if !runFinished(t.run) {
		return false, apperror.New(apperror.Conflict, "下载仍在释放文件，请稍后继续")
	}
	cp := a.toPersisted(t)
	cp.Status, cp.ErrorMsg, cp.EndTime = "queued", "", 0
	if err := a.downloadStore.Save(ctx, cp); err != nil {
		return false, apperror.Wrap(apperror.Storage, err, "保存继续下载状态失败")
	}
	t.mu.Lock()
	t.paused, t.cancelRequested, t.persistenceFailed, t.transferComplete, t.finalizing = false, false, false, false, false
	t.status.Status, t.status.Error, t.status.EndTime = "queued", "", 0
	t.mu.Unlock()
	if err := a.launchDownloadLocked(t); err != nil {
		return false, err
	}
	return true, nil
}

// Removal is durable before unregistering; a stale worker cannot recreate it.
func (a *App) RemoveDownload(ctx context.Context, taskId string) (bool, error) {
	a.downloadMu.Lock()
	if err := a.downloadReadyLocked(); err != nil {
		a.downloadMu.Unlock()
		return false, err
	}
	t, ok := a.downloads.Get(taskId)
	if !ok {
		a.downloadMu.Unlock()
		return true, nil // idempotent: the record is already absent
	}
	if err := a.downloadStore.Delete(ctx, taskId); err != nil {
		a.downloadMu.Unlock()
		return false, apperror.Wrap(apperror.Storage, err, "移除下载记录失败")
	}
	a.downloads.Remove(taskId)
	t.mu.Lock()
	t.cancelRequested = true
	t.status.Status = "cancelled"
	t.mu.Unlock()
	run := t.run
	run.Cancel()
	a.downloadMu.Unlock()
	return true, run.Wait(ctx)
}
