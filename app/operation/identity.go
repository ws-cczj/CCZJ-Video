package operation

import (
	"cczjVideo/app/apperror"
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
