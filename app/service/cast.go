package service

import (
	"cczjVideo/app/cast"
	"cczjVideo/app/operation"
	"context"
)

func (a *App) DiscoverCastDevices(ctx context.Context) ([]cast.Device, error) {
	return runRequest(a, ctx, "cast.DiscoverCastDevices", func(ctx context.Context) ([]cast.Device, error) { return a.cast.DiscoverContext(ctx) })
}
func (a *App) DiscoverCastDevicesOnInterfaces(ctx context.Context, interfaces []cast.NetworkInterface) ([]cast.Device, error) {
	return runRequest(a, ctx, "cast.DiscoverCastDevicesOnInterfaces", func(ctx context.Context) ([]cast.Device, error) {
		return a.cast.DiscoverOnInterfacesContext(ctx, interfaces)
	})
}

// CastReceipt explicitly links frontend handoff evidence to the accepting RPC.
type CastReceipt struct {
	SessionID string `json:"cast_session_id"`
	RequestID string `json:"request_id"`
}

func (a *App) PlayCastVideo(ctx context.Context, id, url, title string) (CastReceipt, error) {
	return runRequest(a, ctx, "cast.PlayCastVideo", func(ctx context.Context) (CastReceipt, error) {
		err := a.cast.PlayContext(ctx, id, url, title)
		identity, _ := operation.IdentityFromContext(ctx)
		return CastReceipt{SessionID: a.cast.SessionID(id), RequestID: identity.RequestID}, err
	})
}
func (a *App) ControlCastVideo(ctx context.Context, id, action string, seconds float64) error {
	return runRequestError(a, ctx, "cast.ControlCastVideo", func(ctx context.Context) error { return a.cast.ControlContext(ctx, id, action, seconds) })
}

func (a *App) GetCastStatus(ctx context.Context, id string) (cast.Status, error) {
	return runRequest(a, ctx, "cast.GetCastStatus", func(ctx context.Context) (cast.Status, error) { return a.cast.StatusContext(ctx, id) })
}

func (a *App) ReleaseCastSession(ctx context.Context, id, session string) error {
	return runRequestError(a, ctx, "cast.ReleaseCastSession", func(ctx context.Context) error { return a.cast.ReleaseSession(ctx, id, session) })
}

// PlayCastVideoWithOptions preserves the default binding while allowing an
// explicit direct/relay comparison for receivers with different access paths.
func (a *App) PlayCastVideoWithOptions(ctx context.Context, id, url, title string, options cast.PlayOptions) (CastReceipt, error) {
	return runRequest(a, ctx, "cast.PlayCastVideoWithOptions", func(ctx context.Context) (CastReceipt, error) {
		err := a.cast.PlayWithOptionsContext(ctx, id, url, title, options)
		identity, _ := operation.IdentityFromContext(ctx)
		return CastReceipt{SessionID: a.cast.SessionID(id), RequestID: identity.RequestID}, err
	})
}
