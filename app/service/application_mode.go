package service

import (
	"context"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
)

const applicationModeKey = "application_mode"

func normalizeMode(mode string) string {
	if mode == "novel" || mode == "comic" {
		return mode
	}
	return "video"
}

func (a *App) currentMode() string {
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	return normalizeMode(a.mode)
}

func (a *App) modeWork() *lifecycle.Group {
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	if a.modeTasks != nil {
		return a.modeTasks
	}
	return a.background
}

func (a *App) startMode(mode string) {
	group := lifecycle.NewGroup()
	a.modeMu.Lock()
	a.mode, a.modeTasks = mode, group
	a.modeMu.Unlock()
	// The host owns shutdown; the mode owns work started while it is active.
	a.background.Go("mode:"+mode, func(ctx context.Context) {
		select {
		case <-ctx.Done():
			group.Stop(shutdownDrainTimeout)
		case <-group.Context().Done():
		}
	})
	if mode == "video" {
		group.Go("schedulers", a.schedulers.Start)
		group.Go("syncGlobalTypes", func(ctx context.Context) {
			if ctx.Err() != nil {
				return
			}
			if _, err := db.SyncGlobalTypesFromSources(); err != nil {
				applog.Warn("同步全局类型失败: %v", err)
			}
		})
	}
	if (mode == "novel" || mode == "comic") && a.library != nil {
		group.Go("reading-source-audit:"+mode, func(ctx context.Context) { a.library.RunSourceMaintenance(ctx, mode) })
	}
}

// A switch drains the old owner before persisting the next mode. Download
// checkpoints remain paused until an explicit ResumeDownload, including restart.
func (a *App) switchApplicationMode(next string) error {
	if next != "video" && next != "novel" && next != "comic" {
		return apperror.New(apperror.Validation, "未知应用模式")
	}
	a.modeSwitchMu.Lock()
	defer a.modeSwitchMu.Unlock()
	previous := a.currentMode()
	if next == previous {
		return a.settings.Set(applicationModeKey, next)
	}
	ctx, cancel := context.WithTimeout(context.Background(), shutdownDrainTimeout)
	defer cancel()
	if previous == "video" && a.downloads != nil {
		for _, task := range a.downloads.Snapshot() {
			s := task.snapshot()
			if s.Status == "queued" || s.Status == "downloading" {
				if _, err := a.PauseDownload(ctx, s.TaskId); err != nil {
					return err
				}
			}
		}
	}
	if a.recording != nil {
		if _, err := a.recording.StopReason(ctx, "mode_changed"); err != nil {
			return err
		}
	}
	if group := a.modeWork(); group != nil && group != a.background {
		if !group.Stop(shutdownDrainTimeout) {
			return apperror.New(apperror.Timeout, "当前模式仍在收尾，请稍后重试切换")
		}
	}
	if err := a.settings.Set(applicationModeKey, next); err != nil {
		a.startMode(previous)
		return err
	}
	a.startMode(next)
	applog.InfoFields("application mode switched", applog.Fields{"from": previous, "mode": next, "downloads": "paused_manual_resume"})
	return nil
}
