package ciligou

import (
	"sync"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

// Updater 磁力链接更新器
type Updater struct {
	batchSize     int
	mu            sync.Mutex
	enabled       bool
	running       bool
	stopRequested bool
}

// NewUpdater 创建更新器
func NewUpdater() *Updater {
	return &Updater{
		batchSize: 3,
		enabled:   true,
	}
}

// Enable 启用/禁用更新器
func (u *Updater) Enable(enable bool) {
	u.mu.Lock()
	u.enabled = enable
	u.mu.Unlock()
	applog.Info("[Ciligou] Updater %s", map[bool]string{true: "enabled", false: "disabled"}[enable])
}

// IsRunning 是否正在运行
func (u *Updater) IsRunning() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.running
}

// RequestStop 请求停止
func (u *Updater) RequestStop() {
	u.mu.Lock()
	u.stopRequested = true
	u.mu.Unlock()
	applog.Info("[Ciligou] Updater graceful stop requested")
}

func (u *Updater) isStopRequested() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.stopRequested
}

func (u *Updater) resetStop() {
	u.mu.Lock()
	u.stopRequested = false
	u.mu.Unlock()
}

// UpdateBatch 批量更新磁力链接
func (u *Updater) UpdateBatch() (int, error) {
	u.mu.Lock()
	if !u.enabled || u.running {
		if !u.enabled {
			applog.Info("[Ciligou] UpdateBatch skipped: updater is disabled")
		} else {
			applog.Info("[Ciligou] UpdateBatch skipped: already running")
		}
		u.mu.Unlock()
		return 0, nil
	}
	u.running = true
	u.stopRequested = false
	u.mu.Unlock()

	defer func() {
		u.mu.Lock()
		u.running = false
		u.mu.Unlock()
	}()

	// 检查是否有启用的类型
	enabledTypes, err := db.GetEnabledGlobalTypes()
	if err != nil {
		applog.Error("[Ciligou] Failed to get enabled types: %v", err)
		return 0, err
	}
	if len(enabledTypes) == 0 {
		applog.Info("[Ciligou] No enabled types for magnet fetching, skipping")
		return 0, nil
	}

	applog.Info("[Ciligou] Starting batch update, enabled types: %d", len(enabledTypes))

	// 获取缺少磁力链接的记录
	missingLinks, err := db.GetVideosMissingMagnetLink(u.batchSize)
	if err != nil {
		applog.Error("[Ciligou] Failed to get videos missing magnet link: %v", err)
		return 0, err
	}

	applog.Info("[Ciligou] Found %d records missing magnet link", len(missingLinks))

	totalUpdated := 0
	for i, row := range missingLinks {
		if u.isStopRequested() {
			applog.Info("[Ciligou] Stop requested, halting after %d/%d records", i, len(missingLinks))
			break
		}

		if row == nil || row.VodName == "" {
			continue
		}

		// 检查该视频的类型是否在启用列表中
		typeNames, err := db.GetGlobalVideoTypeNames(row.VodName)
		if err != nil {
			applog.Warn("[Ciligou] Failed to get type names for '%s': %v", row.VodName, err)
			continue
		}

		typeEnabled := false
		for _, tn := range typeNames {
			if db.IsTypeMagnetEnabled(tn) {
				typeEnabled = true
				break
			}
		}

		if !typeEnabled {
			applog.Info("[Ciligou] [%d/%d] Skipping '%s': type not in enabled list", i+1, len(missingLinks), row.VodName)
			continue
		}

		applog.Info("[Ciligou] [%d/%d] Fetching magnet for: %s", i+1, len(missingLinks), row.VodName)

		magnetLink, err := FetchMagnetForVideo(row.VodName)
		if err != nil {
			applog.Warn("[Ciligou] [%d/%d] FAILED to get magnet for '%s': %v", i+1, len(missingLinks), row.VodName, err)
			db.IncrementMagnetSearchFailures(row.VodName)
			continue
		}

		if magnetLink != "" {
			if err := db.SaveMagnetLink(row.Id, magnetLink); err != nil {
				applog.Error("[Ciligou] [%d/%d] Failed to save magnet link for '%s': %v", i+1, len(missingLinks), row.VodName, err)
				continue
			}
			totalUpdated++
			applog.Info("[Ciligou] [%d/%d] SUCCESS saved magnet link for '%s'", i+1, len(missingLinks), row.VodName)
		}
	}

	applog.Info("[Ciligou] Batch update completed: %d records updated", totalUpdated)
	return totalUpdated, nil
}