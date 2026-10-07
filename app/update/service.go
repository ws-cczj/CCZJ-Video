// Package update coordinates application update state and frontend events.
package update

import (
	"context"
	"encoding/json"
	"runtime"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
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

	// installReport 是这一次启动读到的上一次热替换结论。只活在内存里：
	// 结论文件在 StartupCheck 里就被消费掉了，留着它正是为了在这次会话里说一句话，
	// 而不是每次都把同一次失败重新提醒一遍。
	installReport InstallReport
}

// InstallReport 是热替换脚本的回执，连同此刻真正在运行的版本号。
// 界面照它说话：Result 为空表示这次启动没拿到任何回执（首次安装、或上次没装过更新）。
type InstallReport struct {
	// Result 取 ok / swap_failed / verify_failed / rollback_ok / rollback_failed。
	Result  string `json:"result"`
	Running string `json:"running"`
	// Failed 为真表示这次更新没换成：界面该给一句"现在还是旧版本"，而不是沉默。
	Failed bool `json:"failed"`
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

// Install 立刻装：先记下方才答应的版本号（EffectiveVersion 在换上前要说得出口），
// 再把「退出时安装」那份安排撤掉——现在装就是取代它，留着只会在下次退出时把同一个包再换一遍。
func (s *Service) Install(filePath string) error {
	s.mu.Lock()
	if s.pending != nil {
		updater.RecordInstalledVersion(s.pending.LatestVer)
		s.pending = nil
	}
	s.mu.Unlock()
	if s.PendingInstallInfo().Available {
		if err := s.clearPendingInstall(); err != nil {
			applog.Warn("[Updater] 立即安装没能取消退出时安装记录: %v", err)
		} else {
			applog.Info("[Updater] 立即安装取代了退出时安装的安排")
		}
	}
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

// reportInstallVerdict 把热替换脚本回写的结论变成这次会话里的一份现场报告。
// 脚本是独立进程，本进程必须先让开才能换掉 exe，所以拿不到它的退出码；
// 不回写文件的话，安装失败在日志里连一点痕迹都不会留下——而只留在日志里，用户看到的
// 就还是「点了安装、程序自己关了、重新打开还是老版本」，跟从没提示过更新长得一模一样。
//
// 词表由 app/updater 的替换脚本写下：失败一律以 _failed 结尾（swap_failed /
// verify_failed / rollback_failed），其余算成事。判错这一条，失败就会继续沉默。
func (s *Service) reportInstallVerdict(verdict string) {
	report := InstallReport{
		Result:  verdict,
		Running: updater.Version,
		Failed:  strings.HasSuffix(verdict, failedVerdictSuffix),
	}
	if report.Failed {
		// 话要说得中性：swap_failed 是"新版没装上"，rollback_failed 是"旧版没退回去"，
		// 两边此刻在跑什么由 Running 字段给出，别在这里替用户猜。
		applog.Warn("[Updater] 上次替换脚本回执: %s（当前运行 %s）", verdict, updater.Version)
	} else {
		applog.Info("[Updater] 上次替换脚本回执: %s", verdict)
	}
	s.mu.Lock()
	s.installReport = report
	s.mu.Unlock()
}

// failedVerdictSuffix 是脚本结论里「没换成」的共同后缀。
const failedVerdictSuffix = "_failed"

// LastInstallReport 返回本次启动读到的上次安装结论；Result 为空表示没有结论
// （首次安装、或非 Windows 路径）。
func (s *Service) LastInstallReport() InstallReport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.installReport
}

// RollbackInfo 是「退回上一版」这个入口此刻的现场情况。
//
// 只有文件大小和时间，没有版本号：副本就是 exe 旁边那个 .old 文件，没有任何地方
// 记着它是从哪个版本抄来的。宁可让用户看时间自己判断，不可在这里编一个版本号出来。
type RollbackInfo struct {
	Available  bool  `json:"available"`
	SizeBytes  int64 `json:"size_bytes"`
	ModifiedAt int64 `json:"modified_at"`
}

// SwapBackupInfo 报告当前有没有可用的回滚副本。
func (s *Service) SwapBackupInfo() RollbackInfo {
	_, info, ok := updater.SwapBackupStat()
	if !ok {
		return RollbackInfo{}
	}
	return RollbackInfo{Available: true, SizeBytes: info.Size(), ModifiedAt: info.ModTime().Unix()}
}

// Rollback 起脚本后立刻返回，本进程在半秒后让位。界面拿到的是"脚本已经接手"，
// 换回去到底成没成，要看下一次启动读到的回执（见 LastInstallReport）。
func (s *Service) Rollback() error {
	return updater.RollbackUpdate()
}

// settingPendingInstall 存「退出时安装」这份还没兑现的安排，值是 pendingInstallRecord 的 JSON。
const settingPendingInstall = "pending_install"

// pendingInstallVersionLabelMaxLen 是给界面看的那个版本号的长度上限。version 由前端递进来，
// 只用来把安排说清楚（替换脚本从来不读它），认不出的长度就当没这个版本号。
const pendingInstallVersionLabelMaxLen = 40

// pendingInstallRecord 是一条安装安排：装哪个包、装完应该是哪个版本。
type pendingInstallRecord struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// PendingInstall 是「退出时安装」这个入口此刻的现场情况，直接给界面显示用。
//
// 这里报的是「安排了什么」，不是「装上了没有」——兑现发生在进程最后一次收尾时刻，
// 那次成没成由脚本的回执在下次启动说话（见 LastInstallReport）。
type PendingInstall struct {
	Available bool   `json:"available"`
	Path      string `json:"path"`
	Version   string `json:"version"`
}

// ScheduleInstallOnExit 记下这份安排，然后什么都不做：真正换 exe 的是关停流程
// （见 InstallOnShutdown）。为什么要分这两档、以及为什么默认没人替用户选，
// 见 docs/adr/0010-install-timing-tiers.md。
//
// 点下按钮这一刻就把包验一遍（路径是不是本更新器的产物、字节跟下载时记的摘要对不对），
// 让界面当场就能说清这个包能不能装，而不是等几天后退出时才发现装了个半截文件。
func (s *Service) ScheduleInstallOnExit(filePath, version string) error {
	if runtime.GOOS != "windows" {
		return apperror.New(apperror.Unavailable, "只有 Windows 版本支持在退出时安装更新")
	}
	if err := updater.ValidateInstallArtifact(filePath); err != nil {
		return err
	}
	version = strings.TrimSpace(version)
	if len(version) > pendingInstallVersionLabelMaxLen {
		version = ""
	}
	if err := s.writePendingInstall(pendingInstallRecord{Path: filePath, Version: version}); err != nil {
		return err
	}
	applog.Info("[Updater] 已安排退出时安装: %s（目标版本 %q）", filePath, version)
	return nil
}

// CancelInstallOnExit 撤掉安排：下次退出就不动 exe。档位的开关只有这两处，没有第三种。
func (s *Service) CancelInstallOnExit() error {
	if err := s.clearPendingInstall(); err != nil {
		return err
	}
	applog.Info("[Updater] 已取消退出时安装")
	return nil
}

// PendingInstallInfo 报告当下有没有一条安装安排。
func (s *Service) PendingInstallInfo() PendingInstall {
	record, ok := s.readPendingInstall()
	if !ok {
		return PendingInstall{}
	}
	return PendingInstall{Available: true, Path: record.Path, Version: record.Version}
}

// InstallOnShutdown 兑现「退出时安装」的安排，由应用的关停流程在最后一步调用。
//
// keep 为真表示这次退出其实是一次重启：RestartApp 已经把同一个 exe 又拉起一份，
// 新进程立刻重新锁住文件，这时候换不动，还会在下次启动留下一条 swap_failed
// ——用户看到的是"我没取消过的安排却报失败"。所以这次把安排原样留着，等一次真正的退出。
//
// 返回的 error 只供关停流程写日志：换不成不该拖住退出，更不该拦住窗口关闭。
func (s *Service) InstallOnShutdown(keep bool) error {
	record, ok := s.readPendingInstall()
	if !ok {
		return nil
	}
	if keep {
		applog.Info("[Updater] 这次退出是重启，退出时安装留到下次真正退出（目标版本 %q）", record.Version)
		return nil
	}
	// 先清记录再起脚本：这次要么兑现了，要么放弃了，下次启动不该再看到同一条安排。
	// 真没换成，脚本的回执会说话（见 LastInstallReport），更新也会被重新提示出来。
	if err := s.clearPendingInstall(); err != nil {
		applog.Warn("[Updater] 退出时安装记录没能清掉: %v", err)
	}
	if err := updater.SwapOnExit(record.Path); err != nil {
		applog.Warn("[Updater] 退出时安装没能交给脚本: %v", err)
		return err
	}
	applog.Info("[Updater] 退出时替换已交给脚本（目标版本 %q）", record.Version)
	return nil
}

// reconcilePendingInstall 清掉一条已经不需要兑现的安排：此刻跑着的正是当初安排要装的版本，
// 说明它已经生效（或者用户自己覆盖过 exe）。留着它，下次退出会把同一个包再换一遍。
func (s *Service) reconcilePendingInstall() {
	record, ok := s.readPendingInstall()
	if !ok || record.Version == "" || record.Version != updater.Version {
		return
	}
	if err := s.clearPendingInstall(); err != nil {
		applog.Warn("[Updater] 已生效的退出时安装记录没能清掉: %v", err)
		return
	}
	applog.Info("[Updater] 退出时安装已生效（当前运行 %s），安排记录已清理", record.Version)
}

func (s *Service) readPendingInstall() (pendingInstallRecord, bool) {
	raw, err := s.settings.Get(settingPendingInstall)
	if err != nil || strings.TrimSpace(raw) == "" {
		return pendingInstallRecord{}, false
	}
	var record pendingInstallRecord
	if err := json.Unmarshal([]byte(raw), &record); err != nil {
		applog.Warn("[Updater] 退出时安装记录解不开，按没有安排处理: %v", err)
		return pendingInstallRecord{}, false
	}
	if strings.TrimSpace(record.Path) == "" {
		applog.Warn("[Updater] 退出时安装记录没有包路径，按没有安排处理")
		return pendingInstallRecord{}, false
	}
	return record, true
}

func (s *Service) writePendingInstall(record pendingInstallRecord) error {
	raw, err := json.Marshal(record)
	if err != nil {
		return apperror.Wrap(apperror.Internal, err, "无法保存退出时安装安排")
	}
	return s.settings.Set(settingPendingInstall, string(raw))
}

func (s *Service) clearPendingInstall() error {
	return s.settings.Set(settingPendingInstall, "")
}

// StartupCheck performs the deferred startup update check and emits update events.
// The startup delay is cancellable so shutdown does not keep the task group alive.
func (s *Service) StartupCheck(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	effectiveVersion := reconcileInstalledMarker()
	if verdict, ok := updater.TakeInstallResult(); ok {
		s.reportInstallVerdict(verdict)
	}
	// 这里以前会在确认版本对上之后删掉 .old 回滚副本。副本一被删，「退回上一版」
	// 这个入口就永远是灰的；留着它的代价只是一个 30 MB 的文件，而下一次安装会用
	// copy /y 把它覆盖掉，所以机器上最多只落后一个版本。
	s.reconcilePendingInstall()

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
