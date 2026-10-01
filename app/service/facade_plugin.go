package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/plugin"
	"os"
	"strings"
)

// ======================== 扩展包 / Declarative Extension Packs ========================
//
// 校验、扫描、启用状态都在 app/plugin 里；这几个绑定只做转发。
// 每个包的拒绝原因由 app/plugin 自己按包落一条 Warn（见 Service.logScan）。

// ListPlugins 返回注册表；第一次调用会扫一次盘，之后返回缓存。
func (a *App) ListPlugins() ([]plugin.Info, error) {
	return a.plugins.List()
}

// RescanPlugins 强制重扫，设置页的「重新扫描」按钮用它，不需要重启。
func (a *App) RescanPlugins() ([]plugin.Info, error) {
	return a.plugins.Scan()
}

// SetPluginEnabled 记下一个包的启用/禁用状态（落 settings KV 的 plugin_states）。
func (a *App) SetPluginEnabled(id string, enabled bool) error {
	if err := a.plugins.SetEnabled(id, enabled); err != nil {
		applog.Warn("扩展包开关失败: %v", err)
		return err
	}
	applog.Info("扩展包开关: id=%s enabled=%t", id, enabled)
	return nil
}

// GetPluginDirectory 返回扩展包目录，顺便把它建出来——用户就是照着这条路径
// 把包拷进去的，目录不存在时界面也没法提示「放到这里」。
func (a *App) GetPluginDirectory() (string, error) {
	dir := a.plugins.Directory()
	if strings.TrimSpace(dir) == "" {
		return "", apperror.New(apperror.Internal, "扩展包目录不可用：应用数据目录解析失败")
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", apperror.Wrap(apperror.Storage, err, "创建扩展包目录失败")
	}
	return dir, nil
}

// ReadPluginFile 读包内文本文件（设置页展示 manifest / GLSL 源码用）。
func (a *App) ReadPluginFile(id string, path string) (string, error) {
	return a.plugins.ReadFile(id, path)
}

// ReadPluginAsset 读包内图片，返回 data:<mime>;base64,...，可直接当 img src。
func (a *App) ReadPluginAsset(id string, path string) (string, error) {
	return a.plugins.ReadAsset(id, path)
}

// OpenPluginDirectory 在系统文件管理器里打开扩展包目录。
// 复用 OpenFolder：它已经把「只能打开应用自己管的目录」这条白名单和跨平台
// 命令都处理好了，这里不再另起一份 exec.Command。
func (a *App) OpenPluginDirectory() error {
	dir, err := a.GetPluginDirectory()
	if err != nil {
		return err
	}
	if _, err := a.OpenFolder(dir); err != nil {
		return err
	}
	return nil
}

// InstallPlugin 安装一次原生拖放：WebView2 交给应用的只有被拖文件夹的绝对路径，
// 所以这里只把路径递下去——读目录、落盘、改名、校验、放进 plugins 都在 app/plugin
// 里做（那里的目录边界比这里能做的多）。
//
// 校验没过就什么都不会装，错误消息带原因码；成功时返回注册表里那条权威 Info，
// 界面据此刷新列表并重新注入脚本包。
func (a *App) InstallPlugin(path string) (*plugin.Info, error) {
	info, err := a.plugins.InstallFromPath(path)
	if err != nil {
		applog.WarnFields("扩展包拖放安装失败", applog.Fields{
			"dropped": path,
			"error":   err.Error(),
		})
		return nil, err
	}
	applog.InfoFields("扩展包安装完成", applog.Fields{
		"id":      info.ID,
		"kind":    info.Kind,
		"version": info.Version,
		"status":  info.Status,
		"files":   info.Files,
	})
	return info, nil
}

// UninstallPlugin 删掉某个扩展包在磁盘上的目录（界面上的「卸载」按钮）。
// 闸门都在 app/plugin.Uninstall 里：只认注册表里的包、只删 plugins 一层子目录，
// 应用自带的那几个直接拒绝。
func (a *App) UninstallPlugin(id string) error {
	if err := a.plugins.Uninstall(id); err != nil {
		applog.WarnFields("扩展包卸载失败", applog.Fields{"id": id, "error": err.Error()})
		return err
	}
	return nil
}
