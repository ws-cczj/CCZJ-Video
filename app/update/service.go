// Package update coordinates application update state and frontend events.
package update

import (
	"context"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/settings"
	"cczjVideo/app/updater"
)

// EmitFunc sends a named event to the frontend.
type EmitFunc func(name string, data any)

// Service owns pending update state and update-related persisted settings.
type Service struct {
	settings *settings.Service
	emit     EmitFunc

	mu      sync.Mutex
	pending *updater.UpdateInfo
}

// NewService creates an update service.
func NewService(settingService *settings.Service, emit EmitFunc) *Service {
	return &Service{
		settings: settingService,
		emit:     emit,
	}
}

// Version returns the effective application version.
func (s *Service) Version() string {
	return updater.EffectiveVersion()
}

// Check retrieves the latest available update information.
func (s *Service) Check() (*updater.UpdateInfo, error) {
	return updater.CheckUpdate()
}

// Download downloads an update package and emits progress updates.
func (s *Service) Download(downloadURL string) (string, error) {
	return updater.DownloadUpdate(downloadURL, func(downloaded, total int64, speedBps float64) {
		s.emitEvent("update:download:progress", map[string]any{
			"downloaded": downloaded,
			"total":      total,
			"speed_bps":  speedBps,
		})
	})
}

// Install records the pending version before installing the update package.
func (s *Service) Install(filePath string) error {
	s.mu.Lock()
	if s.pending != nil {
		updater.RecordInstalledVersion(s.pending.LatestVer)
		s.pending = nil
	}
	s.mu.Unlock()
	return updater.InstallUpdate(filePath)
}

// IgnoreVersion persists a version that should not be presented again.
func (s *Service) IgnoreVersion(version string) error {
	return s.settings.Set("ignored_version", version)
}

// IgnoredVersion returns the version currently ignored by the user.
func (s *Service) IgnoredVersion() string {
	version, _ := s.settings.Get("ignored_version")
	return version
}

// LastStartVersion returns the version stored during the previous startup.
func (s *Service) LastStartVersion() string {
	version, _ := s.settings.Get("last_start_version")
	return version
}

// SaveCurrentVersion records the current effective version for the next startup.
func (s *Service) SaveCurrentVersion() {
	_ = s.settings.Set("last_start_version", updater.EffectiveVersion())
}

// Pending returns the update detected during startup, if any.
func (s *Service) Pending() *updater.UpdateInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending
}

// ClearPending removes the update detected during startup.
func (s *Service) ClearPending() {
	s.mu.Lock()
	s.pending = nil
	s.mu.Unlock()
}

// StartupCheck performs the deferred startup update check and emits update events.
// The startup delay is cancellable so shutdown does not keep the task group alive.
func (s *Service) StartupCheck(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	effectiveVersion := updater.EffectiveVersion()
	if effectiveVersion != updater.Version {
		applog.Info("[Updater] 安装版本标记存在 (effective=%s, compiled=%s)，保留标记", effectiveVersion, updater.Version)
	} else {
		updater.ClearInstalledVersion()
	}

	lastVersion := s.LastStartVersion()
	s.SaveCurrentVersion()
	if lastVersion != "" && lastVersion != effectiveVersion {
		applog.Info("[Updater] 版本变化: %s -> %s", lastVersion, effectiveVersion)
		s.emitEvent("update:version:changed", map[string]string{
			"old_version": lastVersion,
			"new_version": effectiveVersion,
		})
	}

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return
	case <-timer.C:
	}
	if ctx.Err() != nil {
		return
	}
	info, err := updater.CheckUpdate()
	if err != nil {
		applog.Debug("[Updater] 启动时检查更新失败: %v", err)
		return
	}
	if !info.HasUpdate || info.LatestVer == s.IgnoredVersion() {
		return
	}

	s.mu.Lock()
	s.pending = info
	s.mu.Unlock()
	applog.Info("[Updater] 发现新版本 %s，推送到前端", info.LatestVer)
	s.emitEvent("update:available", info)
}

func (s *Service) emitEvent(name string, data any) {
	if s.emit != nil {
		s.emit(name, data)
	}
}
