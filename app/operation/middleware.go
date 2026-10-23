// Package operation composes request ownership, recovery and diagnostics without
// tying business modules to Wails or HTTP response writers.
package operation

import (
	"context"
	"fmt"
	"runtime/debug"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
)

type Meta struct{ Name, Mode, Pipeline string }
type Result struct {
	Meta
	RequestID string
	Duration  time.Duration
	Err       error
}
type Observer func(Result)

// Run keeps panic diagnostics private and gives errors a correlation ID. It
// neither logs arguments nor invents a success value after cancellation/panic.
func Run[T any](ctx context.Context, meta Meta, observe Observer, handler func(context.Context) (T, error)) (value T, err error) {
	ctx, identity := WithIdentity(ctx, meta.Pipeline)
	id := identity.RequestID
	began := time.Now()
	defer func() {
		if failure := recover(); failure != nil {
			err = apperror.Wrap(apperror.Internal, fmt.Errorf("panic: %v\n%s", failure, debug.Stack()), "操作失败，请查看日志")
		}
		err = apperror.WithOperation(err, meta.Name, id)
		if observe != nil {
			observe(Result{Meta: meta, RequestID: id, Duration: time.Since(began), Err: err})
		}
	}()
	if err = ctx.Err(); err != nil {
		return value, err
	}
	return handler(ctx)
}
func Log(result Result) {
	if result.Err == nil {
		return
	}
	code := apperror.Public(result.Err).Code
	fields := applog.Fields{"operation": result.Name, "pipeline": result.Pipeline, "request_id": result.RequestID, "mode": result.Mode, "duration_ms": result.Duration.Milliseconds(), "code": code, "error": apperror.Diagnostic(result.Err)}
	if code == apperror.Cancelled {
		return
	}
	if apperror.Describe(code).Level == "error" {
		applog.ErrorFields("operation failed", fields)
	} else {
		applog.WarnFields("operation failed", fields)
	}
	apperror.MarkLogged(result.Err)
}
