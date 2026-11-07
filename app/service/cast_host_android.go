//go:build android

package service

import (
	"cczjVideo/app/applog"
	"cczjVideo/app/operation"
	"context"
	"encoding/json"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func castRelayActive(ctx context.Context, active bool) {
	operation.Stage(ctx, "cast.native.request", applog.Fields{"active": active})
	if active {
		fields := operation.Fields(ctx, nil)
		payload, _ := json.Marshal(map[string]any{"type": "connectedDevice", "cast_session_id": fields["cast_session_id"]})
		application.Android.StartForegroundService(string(payload))
	} else {
		application.Android.StopForegroundService()
	}
}
