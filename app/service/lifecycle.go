package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

// stopOwnedWork signals every owner before waiting. The mode watcher itself is
// application-owned, so waiting for one owner before cancelling the other would
// unnecessarily serialize shutdown and can strand work on a partial failure.
func (a *App) stopOwnedWork(ctx context.Context) error {
	mode := a.modeWork()
	if mode != nil {
		mode.Cancel()
	}
	if a.background != nil {
		a.background.Cancel()
	}
	var failures []error
	if mode != nil && !mode.StopContext(ctx) {
		failures = append(failures, apperror.New(apperror.Timeout, "当前模式后台任务尚未停止"))
	}
	if a.background != nil && a.background != mode && !a.background.StopContext(ctx) {
		failures = append(failures, apperror.New(apperror.Timeout, "应用后台任务尚未停止"))
	}
	return errors.Join(failures...)
}

// closeOwnedResources is shared by startup rollback and normal shutdown. A failed
// close must not prevent independent resources from receiving their close signal.
func (a *App) closeOwnedResources(ctx context.Context) error {
	var failures []error
	closeResource := func(name string, close func(context.Context) error) {
		if err := close(ctx); err != nil {
			failures = append(failures, fmt.Errorf("close %s: %w", name, err))
		}
	}
	if a.mobileMedia != nil {
		closeResource("mobile media", func(context.Context) error { return a.mobileMedia.Close() })
	}
	if a.downloads != nil {
		closeResource("downloads", a.downloads.Close)
	}
	if a.media != nil {
		closeResource("video details", a.media.Close)
	}
	if a.readingImages != nil {
		closeResource("reading images", a.readingImages.Close)
	}
	if a.danmaku != nil {
		closeResource("danmaku", a.danmaku.Close)
	}
	if a.danmakuHTTP != nil {
		a.danmakuHTTP.CloseIdleConnections()
	}
	if a.library != nil {
		closeResource("library", a.library.Close)
	}
	if a.hlsProxy != nil {
		a.hlsProxy.Close()
	}
	if a.requestPolicy != nil {
		closeResource("network policy", a.requestPolicy.Close)
	}
	if a.outboundPool != nil {
		a.outboundPool.Close()
	}
	if a.stopComments != nil {
		closeResource("comments", a.stopComments)
	}
	return errors.Join(failures...)
}

func (a *App) releaseRuntime() error {
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), shutdownDrainTimeout)
	drainErr := a.stopOwnedWork(drainCtx)
	cancelDrain()
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	closeErr := a.closeOwnedResources(closeCtx)
	cancelClose()
	if err := errors.Join(drainErr, closeErr); err != nil {
		// Do not close storage while an owned writer may still be using it. The
		// host receives a failure, rather than a false shutdown-complete signal.
		return err
	}
	if a.databaseOpen {
		db.Close()
		a.databaseOpen = false
	}
	if a.releaseDetails != nil {
		a.releaseDetails()
		a.releaseDetails = nil
	}
	return nil
}

func (a *App) rollbackStartup() error {
	err := a.releaseRuntime()
	if err == nil && a.logOpen {
		applog.Default().Close()
		a.logOpen = false
	}
	return err
}
