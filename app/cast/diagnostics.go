package cast

import (
	"context"
	"errors"
	"net"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/operation"
)

func traceFailure(ctx context.Context, stage string, err error, fields applog.Fields) {
	if fields == nil {
		fields = applog.Fields{}
	}
	fields["stage"], fields["error"] = stage, applog.DiagnosticText(err.Error(), 1024)
	code := "CAST_REQUEST_FAILED"
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		code = "CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "TIMEOUT"
	case errors.As(err, &network) && network.Timeout():
		code = "TIMEOUT"
	}
	if _, exists := fields["code"]; !exists {
		fields["code"] = code
	}
	applog.WarnFields("cast operation failed", operation.Fields(ctx, fields))
}

func traceDevice(ctx context.Context, d renderer) context.Context {
	return operation.WithFields(ctx, applog.Fields{"cast_device_id": d.ID, "cast_session_id": d.session, "device_endpoint": applog.SafeURL(d.endpoint)})
}

func elapsed(start time.Time) int64 { return time.Since(start).Milliseconds() }
