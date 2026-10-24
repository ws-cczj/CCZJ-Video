package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

func (a *Engine) persistPath() string { return filepath.Join(a.dataDir(), "downloads.json") }

func (a *Engine) toPersisted(t *downloadTask) persistedTask {
	t.mu.Lock()
	defer t.mu.Unlock()
	cp := persistedTask{
		TaskId: t.status.TaskId, Url: t.status.Url, Filename: t.status.Filename,
		SavePath: t.status.SavePath, Total: t.status.Total, Downloaded: t.status.Downloaded,
		Status: t.status.Status, StartTime: t.status.StartTime, EndTime: t.status.EndTime,
		ErrorMsg: t.status.Error, SegIndex: t.segIndex, IsM3u8: t.isM3u8,
		Segments: append([]string(nil), t.segments...), SegmentDetails: append(t.playlist[:0:0], t.playlist...),
		HasTotal: t.hasTotal, FinalSize: t.finalSize, BoundarySynced: t.boundarySynced,
	}
	if t.isM3u8 && t.boundarySynced {
		cp.Downloaded, cp.SegIndex = t.checkpointBytes, t.checkpointIndex
	}
	if t.finalizing && cp.Status != "paused" && cp.Status != "cancelled" && cp.Status != "error" {
		cp.Status = "finalizing"
	}
	return cp
}

// mu serializes journal mutations with admission, removal and publication.
// Transport code must release task.mu before acquiring it.
func (a *Engine) persistDownloadLocked(t *downloadTask) error {
	cp := a.toPersisted(t)
	if a.tasks != nil {
		if current, ok := a.tasks.Get(cp.TaskId); !ok || current != t {
			return nil
		}
	}
	if a.store == nil {
		return fmt.Errorf("download journal is unavailable")
	}
	// A final checkpoint must outlive cancellation of its transfer, but remains
	// bounded and carries the same diagnostic identity. Engine.Close joins it
	// before the host is allowed to close SQLite.
	parent := t.operation
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer cancel()
	return a.store.Save(ctx, cp)
}

func (a *Engine) persistenceErrorLocked(t *downloadTask, err error) {
	t.mu.Lock()
	t.persistenceFailed = true
	t.status.Status, t.status.Error = "error", "下载状态保存失败，请检查存储后重试"
	t.status.SpeedBps, t.status.EtaSec = 0, 0
	t.mu.Unlock()
	t.run.Cancel()
	applog.Error("download checkpoint failed: %v", err)
}

func (a *Engine) checkpoint(t *downloadTask) bool {
	a.mu.Lock()
	err := a.persistDownloadLocked(t)
	if err != nil {
		a.persistenceErrorLocked(t, err)
	}
	a.mu.Unlock()
	if err != nil {
		a.emitProgress(t)
	}
	return err == nil
}

// Restore publishes no tasks until the entire persisted journal has validated.
// A startup failure is returned by download RPCs instead of displaying an empty list.
func (a *Engine) loadPersistedTasks(ctx context.Context) error {
	items, err := a.store.Restore(ctx, a.persistPath())
	if err != nil {
		return err
	}
	for _, cp := range items {
		task := &downloadTask{
			status: Status{TaskId: cp.TaskId, Url: cp.Url, Filename: cp.Filename,
				SavePath: cp.SavePath, Total: cp.Total, Downloaded: cp.Downloaded,
				Status: cp.Status, StartTime: cp.StartTime, EndTime: cp.EndTime, Error: cp.ErrorMsg},
			segIndex: cp.SegIndex, segments: cp.Segments, playlist: cp.SegmentDetails,
			isM3u8: cp.IsM3u8, hasTotal: cp.HasTotal, paused: cp.Status == "paused",
			cancelRequested: cp.Status == "cancelled", finalSize: cp.FinalSize,
			boundarySynced: cp.BoundarySynced, checkpointBytes: cp.Downloaded, checkpointIndex: cp.SegIndex,
		}
		a.tasks.Add(cp.TaskId, task)
	}
	return nil
}

// File publication is a recoverable two-step protocol: durable intent, then
// rename, then durable completion. A restart can reconcile the middle window.
func (a *Engine) finalizeDownload(t *downloadTask, partial, final string) {
	a.mu.Lock()
	defer func() { a.mu.Unlock(); a.emitProgress(t) }()
	if a.tasks != nil {
		if current, ok := a.tasks.Get(t.snapshot().TaskId); !ok || current != t {
			return
		}
	}
	s := t.snapshot()
	if s.Status == "paused" || s.Status == "cancelled" || s.Status == "error" {
		return
	}
	fail := func(err error) {
		t.setError(err.Error())
		if saveErr := a.persistDownloadLocked(t); saveErr != nil {
			a.persistenceErrorLocked(t, saveErr)
		}
	}
	file, err := os.OpenFile(partial, os.O_RDWR, 0)
	if err != nil {
		fail(fmt.Errorf("打开下载文件失败: %w", err))
		return
	}
	info, err := file.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("下载目标不是普通文件")
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		fail(fmt.Errorf("同步下载文件失败: %w", err))
		return
	}
	if _, err := os.Lstat(final); !os.IsNotExist(err) {
		fail(fmt.Errorf("目标文件已存在或无法检查，请明确选择覆盖"))
		return
	}
	t.mu.Lock()
	t.finalizing, t.finalSize = true, info.Size()
	t.mu.Unlock()
	if err := a.persistDownloadLocked(t); err != nil {
		a.persistenceErrorLocked(t, err)
		return
	}
	if err := os.Rename(partial, final); err != nil {
		t.mu.Lock()
		t.finalizing = false
		t.mu.Unlock()
		fail(fmt.Errorf("下载文件落位失败: %w", err))
		return
	}
	t.mu.Lock()
	t.finalizing, t.transferComplete = false, false
	t.status.Status, t.status.EndTime = "done", time.Now().Unix()
	t.status.Downloaded, t.status.Total = info.Size(), info.Size()
	t.status.SpeedBps, t.status.EtaSec = 0, 0
	t.checkpointBytes = info.Size()
	t.mu.Unlock()
	if err := a.persistDownloadLocked(t); err != nil {
		// Leave the persisted finalizing record intact. The final file is usable;
		// the next restore verifies its size and commits the completion record.
		t.mu.Lock()
		t.status.Error = "文件已保存，完成状态写入失败；重启后将重新核对"
		t.mu.Unlock()
		applog.Error("download completion checkpoint failed: %v", err)
	}
	_ = os.Remove(directManifestPath(partial))
}

func (a *Engine) downloadReadyLocked() error {
	if a.restoreErr != nil {
		return apperror.Wrap(apperror.Storage, a.restoreErr, "下载记录恢复失败，请检查存储后重启")
	}
	if a.tasks == nil || a.store == nil || a.executor == nil || a.directory == nil || a.dataDir == nil || a.owner == nil {
		return apperror.New(apperror.Unavailable, "下载服务尚未就绪")
	}
	return nil
}
