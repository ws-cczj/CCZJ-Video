package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"cczjVideo/app/db"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/outbound"
	"cczjVideo/app/settings"
)

func TestOutboundPoolSettingValidationAndLifecycle(t *testing.T) {
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	a := &App{settings: settings.NewService(), background: lifecycle.NewGroup()}
	t.Cleanup(func() {
		a.background.Stop(time.Second)
		if a.outboundPool != nil {
			a.outboundPool.Close()
		}
		outbound.Install(nil)
		db.Close()
	})
	for _, raw := range []string{"null", `{"sources":["http://localhost/list"]}`, `{"maxCandidates":10000}`} {
		if err := a.SetSetting(outbound.SettingsKey, raw); err == nil {
			t.Fatalf("invalid pool config accepted: %s", raw)
		}
	}
	config := outbound.DefaultConfig()
	config.Enabled = false
	raw, _ := json.Marshal(config)
	if err := a.SetSetting(outbound.SettingsKey, string(raw)); err != nil {
		t.Fatal(err)
	}
	if err := a.startOutboundPool(); err != nil {
		t.Fatal(err)
	}
	if a.outboundPool == nil || a.outboundPool.Snapshot().Enabled {
		t.Fatal("persisted pool config was not applied")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	select {
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	default:
	}
	if !a.background.Stop(time.Second) {
		t.Fatal("pool owner did not stop")
	}
}
