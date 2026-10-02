// Package update coordinates application update state and frontend events.
package update

import (
	"context"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/settings"
	"cczjVideo/app/updater"
)

// EmitFunc sends a named event to the frontend.
type EmitFunc func(name string, data any)

// settingLicenseTermsVersion 由前端在用户同意条款时写入（值是当前条款版本）。
// 空着就代表这台机器还没签过 —— 键名必须与前端 stores/licenseState.ts 保持一致。
const settingLicenseTermsVersion = "license_terms_version"

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

// reconcileInstalledMarker 校正安装版本标记，返回此刻真正在运行的版本号。
//
// 标记高于当前二进制的编译版本时只剩一种解释：上次安装声称换成了那个版本，但此刻跑着的
// 仍是旧文件——替换根本没成功。留着它，Version() 就报出一个并没有装上的版本号，检查更新
// 据此回答「已是最新」，用户看到的就是「还跑着 2.1.0，它却说我是 2.2.0 且已最新」。
// 现在清掉标记并记下警告，让下一次检查照常把这个更新提示出来，而不是替失败的安装保密。
func reconcileInstalledMarker() string {
	effectiveVersion := updater.EffectiveVersion()
	if effectiveVersion != updater.Version {
		applog.Warn("[Updater] 上次更新未真正装上：标记 %s 高于当前二进制 %s，已清除标记",
			effectiveVersion, updater.Version)
	}
	updater.ClearInstalledVersion()
	return updater.Version
}

// reportInstallVerdict 读掉热替换脚本留下的结论。
// 脚本是独立进程，本进程必须先让开才能换掉 exe，所以拿不到它的退出码；
// 不回写文件的话，安装失败在日志里连一点痕迹都不会留下。
func reportInstallVerdict() {
	verdict, ok := updater.TakeInstallResult()
	if !ok {
		return
	}
	if verdict == "ok" {
		applog.Info("[Updater] 上次热替换脚本回执: %s", verdict)
		return
	}
	applog.Warn("[Updater] 上次热替换脚本回执: %s（当前仍在运行旧版本）", verdict)
}

// StartupCheck performs the deferred startup update check and emits update events.
// The startup delay is cancellable so shutdown does not keep the task group alive.
func (s *Service) StartupCheck(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	// 这个判断必须在清理标记之前做：标记一被清掉，就再也分不出"装上了新版本"和
	// "替换失败、跑的还是旧版"。
	runningInstalledTarget := updater.EffectiveVersion() == updater.Version
	effectiveVersion := reconcileInstalledMarker()
	reportInstallVerdict()
	if runningInstalledTarget {
		// .old 是热替换留下的唯一回滚副本，脚本换完就撤，删不删由这里决定：
		// 只有确认此刻跑着的确实是安装的那个版本，才丢得掉这条退路。
		updater.ClearSwapBackup()
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

	// 条款还没签（新装、或条款升过版）就不替用户出网：更新检查排在同意之后。
	// 上面那一段是本机记账（安装回执、版本变化），跟同意与否无关，不能被跳过。
	// 用户点下「同意并继续」的那一刻，前端会立刻自己触发一次检查，所以提示不会丢。
	if v, _ := s.settings.Get(settingLicenseTermsVersion); strings.TrimSpace(v) == "" {
		applog.Info("[Updater] 尚未同意使用条款，本次启动跳过更新检查")
		return
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
