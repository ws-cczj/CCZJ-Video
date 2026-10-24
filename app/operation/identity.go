package operation

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"context"
)

type identityKey struct{}

// Identity is request metadata, not a source identity or a deduplication key.
type Identity struct{ RequestID, Pipeline string }

func WithIdentity(ctx context.Context, pipeline string) (context.Context, Identity) {
	identity := Identity{RequestID: apperror.NewRequestID(), Pipeline: pipeline}
	return context.WithValue(ctx, identityKey{}, identity), identity
}

func IdentityFromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(identityKey{}).(Identity)
	return identity, ok
}

// OwnedContext gives shared or accepted work its own identity and lifetime.
// Only the parent identity and explicitly selected fields cross this seam.
func OwnedContext(owner, request context.Context, pipeline string, fields applog.Fields) (context.Context, Identity) {
	ctx, identity := WithIdentity(owner, pipeline)
	selected := make(applog.Fields, len(fields)+1)
	for key, value := range fields {
		selected[key] = value
	}
	if request != nil {
		if parent, ok := IdentityFromContext(request); ok {
			selected["parent_request_id"] = parent.RequestID
		}
	}
	return WithFields(ctx, selected), identity
}
