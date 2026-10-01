package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/db"
)

// ======================== 全局类型管理 ========================

// GlobalTypeItem 全局类型项
type GlobalTypeItem struct {
	Id             int    `json:"id"`
	TypeName       string `json:"type_name"`
	CollectEnabled int    `json:"collect_enabled"`
	Sort           int    `json:"sort"`
	CreatedAt      string `json:"created_at"`
}

// GetGlobalTypes 获取所有全局类型
func (a *App) GetGlobalTypes() ([]*db.GlobalTypeRow, error) {
	return db.GetAllGlobalTypes()
}

// SetGlobalTypeCollectEnabledReq 设置全局类型采集开关请求
type SetGlobalTypeCollectEnabledReq struct {
	TypeName string `json:"type_name"`
	Enabled  bool   `json:"enabled"`
}

// SetGlobalTypeCollectEnabled 设置全局类型的采集开关
func (a *App) SetGlobalTypeCollectEnabled(req SetGlobalTypeCollectEnabledReq) (bool, error) {
	if req.TypeName == "" {
		return false, apperror.New(apperror.Validation, "type_name is empty")
	}
	if err := db.SetGlobalTypeCollectEnabled(req.TypeName, req.Enabled); err != nil {
		return false, err
	}
	return true, nil
}

// SyncGlobalTypes 从所有源同步类型到全局类型表
func (a *App) SyncGlobalTypes() (int, error) {
	return db.SyncGlobalTypesFromSources()
}
