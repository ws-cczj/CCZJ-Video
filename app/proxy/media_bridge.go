package proxy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// MediaBridge streams media around mobile WebView asset bridges that buffer
// complete responses. It exposes explicit media/image handlers on loopback,
// behind a fresh unguessable path for each application lifetime.
type MediaBridge struct {
	mu       sync.Mutex
	ctx      context.Context
	handlers map[string]http.Handler
	server   *http.Server
	endpoint string
	closed   bool
	serveErr error
}

// NewMediaBridge creates a lazy bridge owned by an application lifecycle.
func NewMediaBridge(ctx context.Context, handler http.Handler) *MediaBridge {
	return &MediaBridge{ctx: ctx, handlers: map[string]http.Handler{"/__cczj/hls": handler}}
}

// Images need the same query-preserving, streaming Android transport as video.
// Only these explicit proxy routes are reachable through the capability URL.
func NewMediaBridgeWithImages(ctx context.Context, hls, images http.Handler) *MediaBridge {
	b := NewMediaBridge(ctx, hls)
	b.handlers["/__cczj/img"] = images
	return b
}

// Endpoint starts at most one listener and returns its private capability URL.
// Callers must not log or persist this URL.
func (b *MediaBridge) Endpoint() (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed || b.ctx.Err() != nil {
		return "", errors.New("media bridge is closed")
	}
	if b.serveErr != nil {
		return "", fmt.Errorf("media bridge stopped: %w", b.serveErr)
	}
	if b.server != nil {
		return b.endpoint, nil
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", fmt.Errorf("create media bridge capability: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(b.ctx, "tcp4", "127.0.0.1:0")
	if err != nil {
		return "", fmt.Errorf("listen for mobile media: %w", err)
	}
	prefix := "/" + hex.EncodeToString(secret[:])
	b.endpoint = "http://" + listener.Addr().String() + prefix
	server := &http.Server{
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    8192,
		BaseContext:       func(net.Listener) context.Context { return b.ctx },
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path, authorized := strings.CutPrefix(r.URL.Path, prefix)
			handler := b.handlers[path]
			if path == "/__cczj/ios-hls" {
				handler = b.handlers["/__cczj/hls"]
			}
			if !authorized || handler == nil {
				http.NotFound(w, r)
				return
			}
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			forward := r.Clone(r.Context())
			if path == "/__cczj/ios-hls" {
				forward = forward.WithContext(context.WithValue(forward.Context(), hlsProxyRouteKey{}, prefix+path))
			}
			forward.URL.Path = path
			forward.URL.RawPath = ""
			forward.RequestURI = forward.URL.RequestURI()
			handler.ServeHTTP(w, forward)
		}),
	}
	b.server = server
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			b.mu.Lock()
			b.serveErr = err
			b.mu.Unlock()
		}
	}()
	return b.endpoint, nil
}

// Close stops active streams and prevents a late request from restarting it.
func (b *MediaBridge) Close() error {
	b.mu.Lock()
	b.closed = true
	server := b.server
	b.mu.Unlock()
	if server == nil {
		return nil
	}
	return server.Close()
}
