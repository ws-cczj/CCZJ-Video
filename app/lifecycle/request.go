package lifecycle

import "context"

// WithShutdown links a request's cancellation/deadline to application shutdown.
// The returned release function detaches the shutdown hook on normal completion.
func WithShutdown(request, shutdown context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(request)
	stop := context.AfterFunc(shutdown, cancel)
	if shutdown.Err() != nil {
		cancel()
	}
	return ctx, func() { stop(); cancel() }
}
