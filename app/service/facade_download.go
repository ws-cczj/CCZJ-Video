package service

import (
	"context"

	"cczjVideo/app/apperror"
)

// StartVideoDownload validates application-mode admission; the download engine
// owns the accepted task independently from this short binding call.
func (a *App) StartVideoDownload(ctx context.Context, req VideoDownloadReq) (*VideoDownloadStatus, error) {
	if a.downloads == nil {
		return nil, downloadUnavailable()
	}
	return videoAdmission(a, ctx, "download.StartVideoDownload", func(ctx context.Context) (*VideoDownloadStatus, error) {
		return a.downloads.Start(ctx, req)
	})
}

func (a *App) GetDownloadProgress(taskID string) (*VideoDownloadStatus, error) {
	if a.downloads == nil {
		return nil, downloadUnavailable()
	}
	return a.downloads.Progress(taskID)
}

func (a *App) ListDownloads() ([]VideoDownloadStatus, error) {
	if a.downloads == nil {
		return nil, downloadUnavailable()
	}
	return a.downloads.List()
}

func (a *App) PauseDownload(ctx context.Context, taskID string) (bool, error) {
	if a.downloads == nil {
		return false, downloadUnavailable()
	}
	return runRequest(a, ctx, "download.PauseDownload", func(ctx context.Context) (bool, error) { return a.downloads.Pause(ctx, taskID) })
}

func (a *App) CancelDownload(ctx context.Context, taskID string) (bool, error) {
	if a.downloads == nil {
		return false, downloadUnavailable()
	}
	return runRequest(a, ctx, "download.CancelDownload", func(ctx context.Context) (bool, error) { return a.downloads.Cancel(ctx, taskID) })
}

func (a *App) ResumeDownload(ctx context.Context, taskID string) (bool, error) {
	if a.downloads == nil {
		return false, downloadUnavailable()
	}
	return videoAdmission(a, ctx, "download.ResumeDownload", func(ctx context.Context) (bool, error) { return a.downloads.Resume(ctx, taskID) })
}

func (a *App) RemoveDownload(ctx context.Context, taskID string) (bool, error) {
	if a.downloads == nil {
		return false, downloadUnavailable()
	}
	return runRequest(a, ctx, "download.RemoveDownload", func(ctx context.Context) (bool, error) { return a.downloads.Remove(ctx, taskID) })
}

func downloadUnavailable() error {
	return apperror.New(apperror.Unavailable, "下载服务尚未就绪")
}
