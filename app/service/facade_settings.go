package service

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/proxy"
	"strings"
)

// ======================== Settings ========================

func (a *App) GetSetting(key string) (string, error) {
	return a.settings.Get(key)
}

func (a *App) SetSetting(key, value string) error {
	return a.settings.Set(key, value)
}

// settingAllowPrivateNetwork 打开后，媒体与图片代理允许访问私网/回环地址
// （NAS、局域网里的源、本机起的缓存代理）。缺省关闭，理由见 proxy.ValidateTarget。
const settingAllowPrivateNetwork = "proxy_allow_private_network"

// SetAllowPrivateNetwork 落库并立刻生效。改完不用重启是刻意的：用户多半是在
// 「某个源放不出来」时来翻这个开关，必须当场试播才知道是不是它的问题。
func (a *App) SetAllowPrivateNetwork(allow bool) error {
	value := "0"
	if allow {
		value = "1"
	}
	if err := a.settings.Set(settingAllowPrivateNetwork, value); err != nil {
		return err
	}
	proxy.SetAllowPrivateTargets(allow)
	applog.Warn("[Proxy] 内网放行开关已改为 %v", allow)
	return nil
}

func (a *App) GetAllowPrivateNetwork() bool {
	raw, err := a.settings.Get(settingAllowPrivateNetwork)
	return err == nil && strings.TrimSpace(raw) == "1"
}
