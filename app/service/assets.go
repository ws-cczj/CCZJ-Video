package service

import (
	"cczjVideo/app/operation"
	"net/http"
	"strings"
)

// AssetMiddleware serves native HTTP transports independently of Wails bindings.
// ResponseWriter and Request are native objects, not frontend RPC parameters.
func AssetMiddleware(a *App, mobile bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasPrefix(r.URL.Path, "/__cczj/") {
				// Fixed route labels never retain upstream URLs or request headers.
				pipeline := "http.unknown"
				switch r.URL.Path {
				case "/__cczj/img":
					pipeline = "http.image"
				case "/__cczj/hls":
					pipeline = "http.hls"
				case "/__cczj/android-media":
					pipeline = "http.androidMedia"
				}
				ctx, identity := operation.WithIdentity(r.Context(), pipeline)
				r = r.WithContext(ctx)
				w.Header().Set("X-CCZJ-Request-ID", identity.RequestID)
				w.Header().Set("X-CCZJ-Pipeline", pipeline)
			}
			if a.background != nil && strings.HasPrefix(r.URL.Path, "/__cczj/") {
				ctx, release := a.requestContext(r.Context())
				defer release()
				r = r.WithContext(ctx)
			}
			switch r.URL.Path {
			case "/__cczj/android-media":
				if mobile {
					a.serveMobileMediaEndpoint(w, r)
					return
				}
			case "/__cczj/hls":
				if a.hlsProxy == nil {
					http.Error(w, "HLS proxy unavailable", http.StatusServiceUnavailable)
					return
				}
				a.hlsProxy.ServeHTTP(w, r)
				return
			case "/__cczj/img":
				if a.readingImages == nil {
					http.Error(w, "image proxy unavailable", http.StatusServiceUnavailable)
					return
				}
				a.readingImages.ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.URL.Path, "/__cczj/") {
				http.NotFound(w, r)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
