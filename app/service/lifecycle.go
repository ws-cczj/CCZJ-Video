package service

import (
	"context"
	"errors"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

// Cancel the parent before looking up its current mode so a concurrent switch
// cannot publish a new owner that accepts work after the shutdown snapshot.
func (a *App) stopOwnedWork(ctx context.Context) error {
	if a.background != nil {
		a.background.Cancel()
	}
	mode := a.modeWork()
	var failures []error
	if mode != nil && !mode.StopContext(ctx) {
		failures = append(failures, apperror.New(apperror.Timeout, "当前模式后台任务尚未停止"))
	}
	if a.background != nil && a.background != mode && !a.background.StopContext(ctx) {
		failures = append(failures, apperror.New(apperror.Timeout, "应用后台任务尚未停止"))
	}
	return errors.Join(failures...)
}

func (a *App) releaseRuntime() error {
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), shutdownDrainTimeout)
	drainErr := a.stopOwnedWork(drainCtx)
	cancelDrain()
	closeCtx, cancelClose := context.WithTimeout(context.Background(), 5*time.Second)
	storageErr := a.storageClosers.Close(closeCtx)
	independentErr := a.hostClosers.Close(closeCtx)
	cancelClose()
	blocked := errors.Join(drainErr, storageErr)
	if a.databaseOpen {
		if blocked == nil {
			db.Close()
			a.databaseOpen = false
		} else {
			checkpointCtx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			if err := db.PassiveCheckpoint(checkpointCtx); err != nil {
				applog.Warn("退出时 SQLite 被动归并未完成: %v", err)
			}
			cancel()
		}
	}
	if blocked == nil && a.releaseDetails != nil {
		a.releaseDetails()
		a.releaseDetails = nil
	}
	return errors.Join(blocked, independentErr)
}

func (a *App) rollbackStartup() error {
	err := a.releaseRuntime()
	if err == nil && a.logOpen {
		applog.Default().Close()
		a.logOpen = false
	}
	return err
}
