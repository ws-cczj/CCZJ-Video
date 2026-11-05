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
	MediaState      string  `json:"media_state"`
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
	ctx, cancel := context.WithTimeout(ctx, 7*time.Second)
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
	result := Status{State: strings.TrimSpace(response.Body.Transport.State), TransportStatus: strings.TrimSpace(response.Body.Transport.Status), MediaState: "unknown"}
	if result.State == "" {
		err = fmt.Errorf("电视未返回播放状态")
		traceFailure(ctx, "cast.status.invalid", err, applog.Fields{"code": "CAST_INVALID_STATUS"})
		return Status{}, err
	}
	// Position is optional: an unsupported or slow position endpoint must not
	// hide a valid transport state or keep the sender playing alongside the TV.
	positionCtx, cancelPosition := context.WithTimeout(ctx, 1500*time.Millisecond)
	trackURI := ""
	if err = soap(positionCtx, device, "GetPositionInfo", "", &response); err == nil {
		trackURI = strings.TrimSpace(response.Body.Position.URI)
		result.Position, result.HasPosition = clockSeconds(response.Body.Position.Time)
		result.Duration, _ = clockSeconds(response.Body.Position.Duration)
		// Some receivers return a zero clock and zero duration after losing
		// their media surface. This is not a usable progress checkpoint.
		if result.Position == 0 && result.Duration == 0 {
			result.HasPosition = false
		}
	}
	cancelPosition()
	if device.media != "" && trackURI == device.media {
		result.MediaState = "matched"
	} else if device.media != "" {
		// TrackURI can refer to a child of a playlist. GetMediaInfo identifies
		// the transport resource supplied by SetAVTransportURI instead.
		mediaCtx, cancelMedia := context.WithTimeout(ctx, 1500*time.Millisecond)
		var media soapEnvelope
		if soap(mediaCtx, device, "GetMediaInfo", "", &media) == nil {
			uri := strings.TrimSpace(media.Body.Media.URI)
			if uri == device.media && trackURI == "" {
				result.MediaState = "matched"
			} else if uri != "" && uri != device.media {
				result.MediaState = "other"
			}
		}
		cancelMedia()
	}
	// A previous resource's clock must never become this video's checkpoint.
	if result.MediaState != "matched" {
		result.HasPosition = false
	}
	if ctx.Err() != nil {
		return Status{}, ctx.Err()
	}
	operation.Stage(ctx, "cast.status", applog.Fields{"transport_state": result.State, "transport_status": result.TransportStatus, "position_seconds": result.Position, "duration_seconds": result.Duration, "has_position": result.HasPosition, "media_state": result.MediaState})
	return result, nil
}
