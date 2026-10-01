package service

import (
	"encoding/json"
	"strconv"
)

// ======================== Section Index ========================
//
// 绑定面按域拆成了同包的 facade_*.go，每个文件顶部保留拆分前 facade.go 里的段落
// 横幅，所以对着横幅检索就等于对着文件检索；一个域的契约能在一屏里看完。
// 这里只留跨域共用的东西：下面三个 JSON 入参 helper 同时被 source / favorite /
// history 三个域的请求结构体用，挪进任何一个域文件都会让另外两个读起来很奇怪。
//
//   facade_video.go          Video
//   facade_source.go         Source
//   facade_source_import.go  数据源导入 / 导出（含 Brotli 压缩）
//   facade_source_admin.go   数据源详情 / 管理操作
//   facade_backup.go         全量备份：设置 / 收藏 / 历史 / 迁移归档恢复
//   facade_collect.go        Collect
//   facade_scheduler.go      采集调度器
//   facade_douban.go         Douban + 豆瓣评论
//   facade_globaltype.go     全局类型管理
//   facade_diagnostic.go     日志
//   facade_favorite.go       Favorites
//   facade_history.go        Watch History
//   facade_settings.go       Settings
//   facade_window.go         Window / Title Bar
//   facade_plugin.go         扩展包 / Declarative Extension Packs

// --- helpers ---
func normalizeId(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			return s
		}
		return ""
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return strconv.FormatInt(n, 10)
	}
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		return strconv.FormatInt(int64(f), 10)
	}
	return ""
}
func parseInt(raw json.RawMessage, def int) (int, error) {
	if len(raw) == 0 {
		return def, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return def, err
		}
		v, err := strconv.Atoi(s)
		if err != nil {
			return def, err
		}
		return v, nil
	}
	var v int
	if err := json.Unmarshal(raw, &v); err != nil {
		return def, err
	}
	return v, nil
}
func parseFloat(raw json.RawMessage, def float64) (float64, error) {
	if len(raw) == 0 {
		return def, nil
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return def, err
		}
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return def, err
		}
		return v, nil
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil {
		return def, err
	}
	return v, nil
}
