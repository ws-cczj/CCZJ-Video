package service

import (
	CCZJ "cczjVideo/app/cczj"
	"context"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
)

const applicationModeKey = CCZJ.SettingApplicationMode

// ApplicationModeStatus describes the active owner independently of persistence.
type ApplicationModeStatus struct {
	Mode  string `json:"mode"`
	Phase string `json:"phase"`
}

// GetApplicationModeStatus remains available while the previous owner drains.
func (a *App) GetApplicationModeStatus() ApplicationModeStatus {
	a.modeMu.RLock()
	defer a.modeMu.RUnlock()
	status := ApplicationModeStatus{Mode: normalizeMode(a.mode), Phase: "ready"}
	if a.background.Context().Err() != nil {
		status.Phase = "stopping"
	} else if a.modeChanging {
		status.Phase = "switching"
	} else if a.modeTasks != nil && a.modeTasks.Context().Err() != nil {
		status.Phase = "draining"
	}
	return status
}

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
	group := lifecycle.NewGroupWithContext(a.background.Context())
	a.modeMu.Lock()
	a.mode, a.modeTasks = mode, group
	a.modeMu.Unlock()
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

func modeFinishing() error {
	return apperror.New(apperror.ModeFinishing, "当前模式仍在收尾，请稍后重试")
}

// Keep the cancelled group published until it drains. A successful switch or
// explicit recovery replaces it, making this fallback a no-op.
func (a *App) recoverMode(previous string, group *lifecycle.Group) {
	a.background.Go("mode-recovery", func(ctx context.Context) {
		select {
		case <-ctx.Done():
			return
		case <-group.Done():
		}
		a.modeChangeMu.Lock()
		defer a.modeChangeMu.Unlock()
		a.modeSwitchMu.Lock()
		defer a.modeSwitchMu.Unlock()
		if ctx.Err() == nil && a.modeWork() == group {
			a.startMode(previous)
			applog.InfoFields("application mode recovered", applog.Fields{"mode": previous})
		}
	})
}

// A switch drains the old owner before persisting the next mode. Download
// checkpoints remain paused until an explicit ResumeDownload, including restart.
func (a *App) switchApplicationMode(next string) error {
	if next != "video" && next != "novel" && next != "comic" {
		return apperror.New(apperror.Validation, "未知应用模式")
	}
	release, accepted := a.background.Track("mode-switch")
	if !accepted {
		return context.Canceled
	}
	defer release()
	if !a.modeChangeMu.TryLock() {
		return modeFinishing()
	}
	defer a.modeChangeMu.Unlock()
	a.modeMu.Lock()
	a.modeChanging = true
	a.modeMu.Unlock()
	defer func() {
		a.modeMu.Lock()
		a.modeChanging = false
		a.modeMu.Unlock()
	}()

	host := a.background.Context()
	previous := a.currentMode()
	group := a.modeWork()
	draining := group != a.background && group.Context().Err() != nil
	if next == previous && !draining {
		return a.settings.SetContext(host, applicationModeKey, next)
	}
	if !draining {
		if err := a.beginModeDrain(host, previous, group); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(host, shutdownDrainTimeout)
	defer cancel()
	if group != a.background && !group.StopContext(ctx) {
		if err := host.Err(); err != nil {
			return err
		}
		return modeFinishing()
	}

	a.modeSwitchMu.Lock()
	defer a.modeSwitchMu.Unlock()
	if err := host.Err(); err != nil {
		return err
	}
	if next == previous {
		a.startMode(previous)
		return nil
	}
	if previous == "video" && a.cast != nil {
		a.cast.ReleaseSessions()
	}
	if err := a.settings.SetContext(host, applicationModeKey, next); err != nil {
		if host.Err() == nil {
			a.startMode(previous)
		}
		return err
	}
	a.startMode(next)
	applog.InfoFields("application mode switched", applog.Fields{"from": previous, "mode": next, "downloads": "paused_manual_resume"})
	return nil
}

func (a *App) beginModeDrain(host context.Context, previous string, group *lifecycle.Group) error {
	a.modeSwitchMu.Lock()
	defer a.modeSwitchMu.Unlock()
	ctx, cancel := context.WithTimeout(host, shutdownDrainTimeout)
	defer cancel()
	if previous == "video" && a.downloads != nil {
		if err := a.downloads.PauseAll(ctx); err != nil {
			return err
		}
	}
	if a.recording != nil {
		if _, err := a.recording.StopReason(ctx, "mode_changed"); err != nil {
			return err
		}
	}
	if group != a.background {
		group.Cancel()
		a.recoverMode(previous, group)
	}
	return nil
}
