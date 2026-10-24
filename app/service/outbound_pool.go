package service

import (
	"context"
	"encoding/json"

	"cczjVideo/app/apperror"
	"cczjVideo/app/outbound"
)

func (a *App) startOutboundPool() error {
	raw, err := a.settings.Get(outbound.SettingsKey)
	if err != nil {
		return err
	}
	config, err := outbound.DecodeConfig(raw)
	if err != nil {
		return apperror.Wrap(apperror.Validation, err, "公共代理池配置无效")
	}
	if raw == "" {
		encoded, err := json.Marshal(config)
		if err != nil {
			return err
		}
		if err = a.settings.Set(outbound.SettingsKey, string(encoded)); err != nil {
			return err
		}
	}
	pool, err := outbound.New(a.background.Context(), config, a.settings)
	if err != nil {
		return apperror.Wrap(apperror.Storage, err, "恢复公共代理池失败")
	}
	a.outboundPool = pool
	pool.SetControlBudget(a.networkBudget)
	outbound.Install(pool)
	if !a.background.Go("outbound.proxy-pool", func(ctx context.Context) { pool.Run(ctx) }) {
		pool.Close()
		return apperror.New(apperror.Cancelled, "代理池启动已取消")
	}
	return nil
}
