package service

import (
	"os"
	"path/filepath"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	backupservice "cczjVideo/app/backup"
	"cczjVideo/app/db"
)

// 旧数据目录（2.1.0 之前：exe 旁边的 data）的找回入口。
//
// 启动时的自动迁移只在「新目录还没有库」时动手，而这一条件在真实升级里几乎总是不成立
// ——新库早在上一次首次启动时就建好了。那份旧库不能被自动覆盖，也不该被自动并进当前库
// （它会往用户的库里写东西），所以这里只负责把它读出来、报给用户，点合并的是用户。

// legacyMigration 是启动时那一步目录迁移的结论。
type legacyMigration int

const (
	legacyNone legacyMigration = iota
	legacyMigrated
	legacyBlocked
)

// 记在当前库里的两个键：旧数据被搬走过一次，就不该再拿同一件事提示用户。
const (
	legacyStatusKey      = "legacy_data_status"
	legacyAtKey          = "legacy_data_at"
	legacyStatusMigrated = "migrated"
	legacyStatusMerged   = "merged"
)

// recordLegacyDataStatus 写下处理结论与时刻。记录失败只影响下次是否再提示一次，
// 不影响数据本身，所以只出日志。
func recordLegacyDataStatus(status string) {
	if err := db.SetSetting(legacyStatusKey, status); err != nil {
		applog.Warn("[Legacy] 记录旧数据处理状态失败: %v", err)
		return
	}
	if err := db.SetSetting(legacyAtKey, time.Now().Format(time.RFC3339)); err != nil {
		applog.Warn("[Legacy] 记录旧数据处理时刻失败: %v", err)
	}
}

// LegacyNotice 是旧数据现场的完整描述，界面照它说话，不自己拼路径也不猜内容。
type LegacyNotice struct {
	Available  bool   `json:"available"`
	LegacyDir  string `json:"legacy_dir"`
	SizeBytes  int64  `json:"size_bytes"`
	ModifiedAt string `json:"modified_at"`
	Status     string `json:"status"`
	StatusAt   string `json:"status_at"`
	Favorites  int    `json:"favorites"`
	History    int    `json:"history"`
	Sources    int    `json:"sources"`
	Reason     string `json:"reason,omitempty"`
}

// legacyDataCandidateDir 返回可以当作旧落点的那个目录，没有则空串。
// 排除两种情况：拿不到 exe 路径，以及 getDataDir 退回 exe 旁边 data 的场景——
// 那时「旧目录」就是当前目录本身，提示用户合并自己既没意义也危险。
func (a *App) legacyDataCandidateDir() string {
	dir := legacyDataDir()
	if dir == "" {
		return ""
	}
	if filepath.Clean(dir) == filepath.Clean(a.getDataDir()) {
		return ""
	}
	if _, err := os.Stat(filepath.Join(dir, "cczj_video.db")); err != nil {
		return ""
	}
	return dir
}

// LegacyDataNotice 报告旧数据目录里那份库的现状：在不在、多大、能找回多少收藏历史与源。
// 已经自动迁移过或已合并过的，available 为假，但目录与状态照实返回，设置页仍能让用户看清。
func (a *App) LegacyDataNotice() (LegacyNotice, error) {
	notice := LegacyNotice{Status: legacyDataStatus()}
	dir := a.legacyDataCandidateDir()
	if dir == "" {
		return notice, nil
	}
	notice.LegacyDir = dir
	if info, err := os.Stat(filepath.Join(dir, "cczj_video.db")); err == nil {
		notice.SizeBytes = info.Size()
		notice.ModifiedAt = info.ModTime().Format(legacyTimeLayout)
	}
	if notice.Status != "" {
		// 已经处理过：不再重读旧库，只把当时的结论和时刻报给界面。
		notice.StatusAt = displayLegacyStatusAt(legacyDataStatusAt())
		return notice, nil
	}
	counts, err := backupservice.NewService().PeekLegacy(dir)
	if err != nil {
		notice.Reason = err.Error()
		applog.Warn("[Legacy] 旧库预读失败 %s: %v", dir, err)
		return notice, nil
	}
	notice.Favorites, notice.History, notice.Sources = counts.Favorites, counts.History, counts.Sources
	notice.Available = !counts.Empty()
	applog.Info("[Legacy] 发现可找回的旧数据库: %s（收藏 %d、历史 %d、源 %d）", dir, counts.Favorites, counts.History, counts.Sources)
	return notice, nil
}

func legacyDataStatus() string {
	value, err := db.GetSetting(legacyStatusKey)
	if err != nil {
		return ""
	}
	return value
}

func legacyDataStatusAt() string {
	value, err := db.GetSetting(legacyAtKey)
	if err != nil {
		return ""
	}
	return value
}

// legacyTimeLayout 是报给用户界面的时刻格式，与备份文件列表里的 modified_at 一致。
const legacyTimeLayout = "2006-01-02 15:04:05"

// displayLegacyStatusAt 把设置表里的 RFC3339 时刻改成界面要的样子。
// 解析不了就原样报出去：这条只是「上次是什么时候处理的」，不值得为它报错。
func displayLegacyStatusAt(raw string) string {
	if raw == "" {
		return ""
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.Format(legacyTimeLayout)
	}
	return raw
}

// LegacyDataMerge 把旧库里的采集源、收藏、历史与元数据并进当前库。
// 路径由后端自己算（同 LegacyDataNotice），前端递不进任意目录。
func (a *App) LegacyDataMerge() (backupservice.Result, error) {
	dir := a.legacyDataCandidateDir()
	if dir == "" {
		return backupservice.Result{}, apperror.New(apperror.NotFound, "没有找到旧数据目录里的数据库")
	}
	result, err := backupservice.NewService().MergeLegacy(dir)
	if err != nil {
		return backupservice.Result{}, err
	}
	recordLegacyDataStatus(legacyStatusMerged)
	applog.Info("[Legacy] 已合并旧数据库 %s: 源 +%d、收藏 +%d、历史 %d",
		dir, result.SourcesAdded, result.FavoritesAdded, result.HistoryApplied)
	return result, nil
}
