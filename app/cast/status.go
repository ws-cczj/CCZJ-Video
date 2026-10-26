package cast

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/operation"
)

type Status struct {
	State           string  `json:"state"`
	TransportStatus string  `json:"transport_status"`
	Position        float64 `json:"position"`
	Duration        float64 `json:"duration"`
	HasPosition     bool    `json:"has_position"`
}

func clockSeconds(value string) (float64, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, false
	}
	hours, e1 := strconv.Atoi(parts[0])
	minutes, e2 := strconv.Atoi(parts[1])
	seconds, e3 := strconv.ParseFloat(parts[2], 64)
	if e1 != nil || e2 != nil || e3 != nil || hours < 0 || hours > 7*24 || minutes < 0 || minutes >= 60 || seconds < 0 || seconds >= 60 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return 0, false
	}
	return float64(hours*3600+minutes*60) + seconds, true
}

// StatusContext queries the renderer, rather than treating SOAP acceptance as
// evidence that the receiving device has decoded or started the media.
func (s *Service) StatusContext(request context.Context, id string) (Status, error) {
	ctx, release := lifecycle.WithShutdown(request, s.ctx)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	device, err := s.device(id)
	if err != nil {
		traceFailure(ctx, "cast.device.unavailable", err, applog.Fields{"cast_device_id": id})
		return Status{}, err
	}
	ctx = traceDevice(ctx, device)
	var response soapEnvelope
	if err = soap(ctx, device, "GetTransportInfo", "", &response); err != nil {
		return Status{}, err
	}
	result := Status{State: strings.TrimSpace(response.Body.Transport.State), TransportStatus: strings.TrimSpace(response.Body.Transport.Status)}
	if result.State == "" {
		err = fmt.Errorf("电视未返回播放状态")
		traceFailure(ctx, "cast.status.invalid", err, applog.Fields{"code": "CAST_INVALID_STATUS"})
		return Status{}, err
	}
	if err = soap(ctx, device, "GetPositionInfo", "", &response); err == nil {
		result.Position, result.HasPosition = clockSeconds(response.Body.Position.Time)
		result.Duration, _ = clockSeconds(response.Body.Position.Duration)
	}
	if ctx.Err() != nil {
		return Status{}, ctx.Err()
	}
	operation.Stage(ctx, "cast.status", applog.Fields{"transport_state": result.State, "transport_status": result.TransportStatus, "position_seconds": result.Position, "duration_seconds": result.Duration, "has_position": result.HasPosition})
	return result, nil
}
