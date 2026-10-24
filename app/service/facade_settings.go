package service

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/danmaku"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/operation"
	"cczjVideo/app/outbound"
	"cczjVideo/app/proxy"
	"context"
	"encoding/json"
	"strings"
)

// ======================== Settings ========================

func (a *App) GetSetting(key string) (string, error) {
	return a.settings.Get(key)
}

func (a *App) SetSetting(key, value string) error {
	fields := applog.Fields{"setting_key": key, "value_sha256": applog.Fingerprint([]byte(value))}
	if key == "default_source_key" || key == applicationModeKey {
		fields["value"] = value
	}
	ctx := operation.WithFields(context.Background(), fields)
	work := func(context.Context) (bool, error) {
		return true, a.setSetting(key, value)
	}
	if key == applicationModeKey {
		// Switching stops the previous mode owner; registering this request in
		// that group would make shutdown wait for the switch itself.
		_, err := operation.Run(ctx, operation.Meta{Name: "settings.SetSetting", Mode: a.currentMode(), Pipeline: "rpc.SetSetting"}, operation.Log, work)
		return err
	}
	_, err := runRequest(a, ctx, "settings.SetSetting", work)
	return err
}

func (a *App) setSetting(key, value string) error {
	if key == applicationModeKey {
		return a.switchApplicationMode(value)
	}
	a.settingsWriteMu.Lock()
	defer a.settingsWriteMu.Unlock()
	if key == outbound.SettingsKey {
		config, err := outbound.DecodeConfig(value)
		if err != nil {
			return apperror.Wrap(apperror.Validation, err, "公共代理池配置无效")
		}
		raw, err := json.Marshal(config)
		if err != nil {
			return err
		}
		return a.settings.Set(key, string(raw)) // Configuration is applied on restart.
	}
	if key == netpolicy.SettingsKey {
		settings, err := netpolicy.DecodeSettings(value)
		if err != nil {
			return apperror.Wrap(apperror.Validation, err, "网络请求策略无效")
		}
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		if err = a.settings.Set(key, string(raw)); err != nil {
			return err
		}
		if a.requestPolicy != nil {
			a.requestPolicy.SetSettings(settings)
		}
		return nil
	}
	if key == danmaku.SourceSettingsKey {
		settings, err := danmaku.DecodeSourceSettings(value)
		if err != nil {
			return apperror.Wrap(apperror.Validation, err, "弹幕源设置无效")
		}
		raw, err := json.Marshal(settings)
		if err != nil {
			return err
		}
		value = string(raw)
	}
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
