package operation

import (
	"cczjVideo/app/applog"
	"context"
)

type fieldsKey struct{}

// WithFields carries explicitly selected diagnostic inputs through domain I/O.
// Callers must not pass arbitrary request bodies, credentials or configuration.
func WithFields(ctx context.Context, fields applog.Fields) context.Context {
	return context.WithValue(ctx, fieldsKey{}, Fields(ctx, fields))
}

func Fields(ctx context.Context, fields applog.Fields) applog.Fields {
	out := applog.Fields{}
	if ctx != nil {
		if inherited, ok := ctx.Value(fieldsKey{}).(applog.Fields); ok {
			for key, value := range inherited {
				out[key] = value
			}
		}
		if identity, ok := IdentityFromContext(ctx); ok {
			out["request_id"], out["pipeline"] = identity.RequestID, identity.Pipeline
		}
	}
	for key, value := range fields {
		out[key] = value
	}
	return out
}

func Stage(ctx context.Context, stage string, fields applog.Fields) {
	fields = Fields(ctx, fields)
	fields["stage"] = stage
	applog.InfoFieldsAt(1, "operation stage", fields)
}
