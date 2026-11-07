package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/operation"
	"golang.org/x/net/netutil"
)

// CastRelay exposes only resources belonging to an active cast, using the same
// upstream client, cookie jar, headers and SSRF policy as local playback.
type CastRelay struct {
	ctx     context.Context
	cancel  context.CancelFunc
	hls     *HLSService
	active  func(context.Context, bool)
	mu      sync.Mutex
	streams map[*castStream]struct{}
	closed  bool
	pending int
}

type castResource struct{ url, referer, kind string }
type castStream struct {
	paused                          bool
	owner                           *CastRelay
	ctx                             context.Context
	cancel                          context.CancelFunc
	server                          *http.Server
	done                            chan struct{}
	base, root, peer, prefix        string
	health                          string
	watch                           *time.Timer
	dataSeen, getSeen, playlistSeen bool
	failureAt, recoveredAt          time.Time
	firstLogged, completeLogged     map[string]bool
	rejected                        map[string]int64
	continuous                      bool
	mu                              sync.Mutex
	resources                       map[string]castResource
	resourceBytes                   int

	inflight                        int
	requests, failures, transferred int64
	recoveries                      int64

	closed    bool
	idle      *time.Timer
	stopOwner func() bool
}

// NewCastRelay creates an application-owned, lazily bound LAN media transport.
// active belongs to the native host and keeps background streaming alive.
func NewCastRelay(ctx context.Context, hls *HLSService, active func(context.Context, bool)) *CastRelay {
	owned, cancel := context.WithCancel(ctx)
	return &CastRelay{ctx: owned, cancel: cancel, hls: hls, active: active, streams: make(map[*castStream]struct{})}
}

// Prepare verifies the upstream before publishing a capability URL. localAddress
// comes from the SSDP receiving socket, never from a frontend-supplied address.
func (b *CastRelay) Prepare(request context.Context, receiver, localAddress, raw string) (string, string, func(), error) {
	request, releaseRequest := lifecycle.WithShutdown(request, b.ctx)
	defer releaseRequest()
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil || len(b.streams)+b.pending >= 4 {
		b.mu.Unlock()
		return "", "", nil, apperror.New(apperror.Unavailable, "投屏连接暂不可用，请停止旧连接后重试")
	}
	b.pending++
	if len(b.streams)+b.pending == 1 && b.active != nil {
		b.active(request, true)
	}
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		b.pending--
		if len(b.streams)+b.pending == 0 && b.active != nil {
			b.active(request, false)
		}
		b.mu.Unlock()
	}()
	peerURL, err := url.Parse(receiver)
	if err != nil {
		return "", "", nil, err
	}
	peer := net.ParseIP(peerURL.Hostname())
	if peer == nil || peer.To4() == nil || !peer.IsPrivate() || peer.IsLoopback() {
		return "", "", nil, apperror.New(apperror.Validation, "投屏设备地址不可用")
	}
	local := net.ParseIP(localAddress)
	if local == nil || local.IsUnspecified() {
		connection, err := (&net.Dialer{}).DialContext(request, "udp4", net.JoinHostPort(peer.String(), "9"))
		if err != nil {
			return "", "", nil, apperror.Wrap(apperror.Unavailable, err, "无法连接电视所在的局域网")
		}
		local = connection.LocalAddr().(*net.UDPAddr).IP
		_ = connection.Close()
	}
	if local.To4() == nil || !local.IsPrivate() || local.IsLoopback() {
		return "", "", nil, apperror.New(apperror.Unavailable, "没有可用于投屏的局域网地址")
	}
	upstream, err := parsePublicHTTPURL(raw)
	if err != nil {
		return "", "", nil, apperror.Wrap(apperror.Validation, err, "视频地址不可用于投屏")
	}
	referer := RefererOrigin(raw)
	response, final, err := b.hls.fetch(request, upstream, "bytes=0-511", referer)
	if err != nil {
		operation.Stage(request, "cast.stream.probe_failed", applog.Fields{"code": "CAST_UPSTREAM_UNAVAILABLE"})
		return "", "", nil, apperror.Wrap(apperror.Unavailable, err, "无法读取投屏视频，请重试或更换线路")
	}
	defer response.Body.Close()
	operation.Stage(request, "cast.stream.probe", applog.Fields{"http_status": response.StatusCode})
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return "", "", nil, apperror.Newf(apperror.Unavailable, "视频源拒绝投屏请求（HTTP %d），请重试或更换线路", response.StatusCode)
	}
	prefix, err := io.ReadAll(io.LimitReader(response.Body, 512))
	if err != nil {
		return "", "", nil, apperror.Wrap(apperror.Unavailable, err, "读取投屏视频失败")
	}
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	playlist := bytes.HasPrefix(bytes.TrimSpace(prefix), []byte("#EXTM3U"))
	if playlist {
		contentType = "application/vnd.apple.mpegurl"
	}
	if len(prefix) == 0 || strings.Contains(contentType, "html") || strings.Contains(http.DetectContentType(prefix), "html") || strings.Contains(contentType, "json") || (isPlaylist(final, contentType) && !playlist) {
		return "", "", nil, apperror.New(apperror.Unavailable, "视频源未返回可播放内容，请更换线路")
	}
	rootKind := "unknown"
	if (len(prefix) >= 8 && (string(prefix[4:8]) == "ftyp" || string(prefix[4:8]) == "styp")) || (len(prefix) > 188 && prefix[0] == 0x47 && prefix[188] == 0x47) || bytes.HasPrefix(prefix, []byte("FLV")) || bytes.HasPrefix(prefix, []byte{0x1a, 0x45, 0xdf, 0xa3}) {
		rootKind = "media"
	}
	inferredType := mime.TypeByExtension(strings.ToLower(path.Ext(final.Path)))
	if strings.HasPrefix(contentType, "video/") || strings.HasPrefix(contentType, "audio/") || strings.HasPrefix(inferredType, "video/") || strings.HasPrefix(inferredType, "audio/") {
		rootKind = "media"
	}
	if playlist {
		rootKind = "playlist"
	}
	if contentType == "" || contentType == "application/octet-stream" {
		contentType = mime.TypeByExtension(strings.ToLower(path.Ext(final.Path)))
		if contentType == "" {
			contentType = "video/mp4"
		}
	}
	listener, err := (&net.ListenConfig{}).Listen(request, "tcp4", net.JoinHostPort(local.String(), "0"))
	if err != nil {
		return "", "", nil, apperror.Wrap(apperror.Unavailable, err, "无法创建局域网投屏连接")
	}
	var secret [32]byte
	if _, err = rand.Read(secret[:]); err != nil {
		_ = listener.Close()
		return "", "", nil, err
	}
	owned, _ := operation.OwnedContext(b.ctx, request, "cast.stream", selectedCastFields(request))
	ctx, cancel := context.WithTimeout(owned, 12*time.Hour)
	s := &castStream{owner: b, ctx: ctx, cancel: cancel, done: make(chan struct{}), peer: peer.String(), resources: make(map[string]castResource), health: "waiting_request", firstLogged: make(map[string]bool), completeLogged: make(map[string]bool), rejected: make(map[string]int64)}
	s.prefix = "/" + hex.EncodeToString(secret[:]) + "/"
	s.base = "http://" + listener.Addr().String() + s.prefix

	s.continuous = !playlist
	s.root, err = s.resourceURL(raw, referer, rootKind)
	if err != nil {
		cancel()
		_ = listener.Close()
		return "", "", nil, err
	}
	// Some receivers infer the demuxer from the URL even when MIME metadata is
	// present. Preserve a playlist extension for extensionless origin URLs too.
	if playlist && !strings.HasSuffix(s.root, ".m3u8") {
		id := strings.TrimPrefix(s.root, s.base)
		s.resources[id+".m3u8"] = s.resources[id]
		delete(s.resources, id)
		s.root += ".m3u8"
	}
	s.server = &http.Server{ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }, Handler: http.HandlerFunc(s.serveHTTP)}
	b.mu.Lock()
	if b.closed || b.ctx.Err() != nil || len(b.streams) >= 4 {
		b.mu.Unlock()
		cancel()
		_ = listener.Close()
		return "", "", nil, apperror.New(apperror.Unavailable, "投屏连接暂不可用，请停止旧连接后重试")
	}
	b.streams[s] = struct{}{}
	b.mu.Unlock()
	s.mu.Lock()
	s.watch = time.AfterFunc(45*time.Second, s.checkStartup)
	s.idle = time.AfterFunc(30*time.Minute, s.expire)
	s.stopOwner = context.AfterFunc(ctx, s.close)
	s.mu.Unlock()
	go func() {
		defer close(s.done)
		err := s.server.Serve(netutil.LimitListener(listener, 12))
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			operation.Stage(ctx, "cast.stream.server_failed", applog.Fields{"code": "CAST_RELAY_STOPPED"})
		}
		s.close()
	}()
	operation.Stage(ctx, "cast.stream.ready", applog.Fields{"transport": "lan_http", "playlist": playlist})
	return s.root, contentType, s.close, nil
}

func selectedCastFields(ctx context.Context) applog.Fields {
	all := operation.Fields(ctx, nil)
	fields := applog.Fields{}
	for _, key := range []string{"cast_device_id", "cast_session_id", "mode"} {
		if value, ok := all[key]; ok {
			fields[key] = value
		}
	}
	return fields
}

func (s *castStream) resourceURL(raw, referer, kind string) (string, error) {
	if len(raw) > 8192 || len(referer) > maxRefBytes {
		return "", errors.New("cast resource URL too large")
	}
	sum := sha256.Sum256([]byte(raw + "\x00" + referer + "\x00" + kind))
	id := hex.EncodeToString(sum[:])
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if len(ext) > 8 {
		ext = ""
	}
	for _, char := range ext {
		if char != '.' && (char < 'a' || char > 'z') && (char < '0' || char > '9') {
			ext = ""
			break
		}
	}
	id += ext
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return "", errors.New("cast stream closed")
	}
	if _, ok := s.resources[id]; !ok {
		if len(s.resources) >= 32768 || s.resourceBytes+len(raw)+len(referer) > 8<<20 {
			return "", errors.New("cast resource budget exceeded")
		}
		s.resources[id] = castResource{raw, referer, kind}
		s.resourceBytes += len(raw) + len(referer)
	}
	return s.base + id, nil
}

func (s *castStream) serveHTTP(w http.ResponseWriter, r *http.Request) {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	id, ok := strings.CutPrefix(r.URL.Path, s.prefix)
	s.mu.Lock()
	resource, exists := s.resources[id]
	if err != nil || host != s.peer || !ok || !exists || s.closed || r.URL.RawQuery != "" {
		reason := "resource_revoked"
		if err != nil || host != s.peer {
			reason = "receiver_mismatch"
		}
		s.rejected[reason]++
		count := s.rejected[reason]
		s.mu.Unlock()
		if count == 1 {
			operation.Stage(s.ctx, "cast.stream.rejected", applog.Fields{"reason": reason})
		}
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		s.mu.Unlock()
		w.Header().Set("Allow", "GET, HEAD")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if s.inflight >= 8 {
		s.rejected["concurrency_limit"]++
		s.mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	s.inflight++
	s.requests++
	if r.Method == http.MethodGet {
		s.getSeen = true
	}
	s.idle.Reset(30 * time.Minute)
	first := s.requests == 1
	s.mu.Unlock()
	if first {
		operation.Stage(s.ctx, "cast.stream.receiver_request", applog.Fields{"method": r.Method, "resource_kind": resource.kind, "has_range": r.Header.Get("Range") != ""})
	}
	defer func() {
		s.mu.Lock()
		s.inflight--
		if !s.closed {
			s.idle.Reset(30 * time.Minute)
		}
		s.mu.Unlock()
	}()
	forward := r.Clone(context.WithValue(r.Context(), hlsResourceURLKey{}, s.resourceURL))
	if !s.continuous && s.base+id == s.root {
		forward.Header.Del("Range")
	}
	forward = forward.WithContext(context.WithValue(forward.Context(), hlsSniffPlaylistKey{}, true))
	forward = forward.WithContext(context.WithValue(forward.Context(), hlsStreamKey{}, s.continuous && s.base+id == s.root))
	forward.URL = &url.URL{Path: "/__cczj/hls", RawQuery: strings.TrimPrefix(s.owner.hls.urlFor(resource.url, resource.referer), "/__cczj/hls?")}
	forward.RequestURI = forward.URL.RequestURI()
	// Request headers describe the LAN transfer, not the CDN connection.
	w.Header().Set("transferMode.dlna.org", "Streaming")
	result := &castResponse{ResponseWriter: w, status: http.StatusOK, features: r.Header.Get("getcontentFeatures.dlna.org") == "1"}
	result.onBytes = func(n int) { s.mu.Lock(); s.transferred += int64(n); s.mu.Unlock() }
	began := time.Now()
	kind := func() string {
		if strings.Contains(strings.ToLower(result.Header().Get("Content-Type")), "mpegurl") {
			return "playlist"
		}
		content := strings.ToLower(result.Header().Get("Content-Type"))
		if strings.Contains(content, "html") || strings.Contains(content, "json") || strings.HasPrefix(content, "text/") {
			return "unknown"
		}
		return resource.kind
	}
	result.firstData = func(firstBytes int) {
		category := kind()
		s.mu.Lock()
		first := !s.firstLogged[category]
		s.firstLogged[category] = true
		if category == "playlist" {
			s.playlistSeen = true
		}
		recovered := category == "media" && (s.health == "startup_timeout" || s.failureAt.After(s.recoveredAt))
		if !s.dataSeen && s.health != "startup_timeout" {
			s.health = category + "_received"
		}
		if category == "media" {
			s.dataSeen = true
			s.health = "media_received"
			s.recoveredAt = time.Now()
		}
		if recovered {
			s.recoveries++
			recovered = s.recoveries <= 8
		}
		s.mu.Unlock()
		if recovered {
			operation.Stage(s.ctx, "cast.stream.recovered", applog.Fields{"resource_kind": category})
		}
		if first {
			operation.Stage(s.ctx, "cast.stream.first_bytes", applog.Fields{"bytes": firstBytes, "resource_kind": category, "http_status": result.status, "elapsed_ms": time.Since(began).Milliseconds()})
		}
	}
	upstreamStatus := 0
	upstreamFailed := false
	forward = forward.WithContext(context.WithValue(forward.Context(), hlsFetchObserveKey{}, func(status int, err error) { upstreamStatus = status; upstreamFailed = err != nil }))
	s.owner.hls.ServeHTTP(result, forward)
	category := kind()
	cancelled := r.Context().Err() != nil || result.failed
	failed := !cancelled && (result.status >= 400 || upstreamFailed)
	s.mu.Lock()

	recovered := false
	if failed {
		s.failures++
		s.failureAt = time.Now()
	} else if !cancelled && r.Method == http.MethodGet && result.bytes > 0 && category == "media" {
		recovered = !s.failureAt.IsZero() && s.failureAt.After(s.recoveredAt)
		s.recoveredAt = time.Now()
		s.health = "media_received"
	}
	outcome := "success"
	if failed {
		outcome = "failed"
	} else if cancelled {
		outcome = "cancelled"
	}
	completionKey := r.Method + "." + category + "." + outcome
	firstComplete := !s.completeLogged[completionKey]
	s.completeLogged[completionKey] = true
	logResponse := (!failed && firstComplete) || (failed && s.failures <= 8)
	if recovered {
		s.recoveries++
		recovered = s.recoveries <= 8
	}
	s.mu.Unlock()
	if recovered {
		operation.Stage(s.ctx, "cast.stream.recovered", applog.Fields{"resource_kind": category})
	}
	if logResponse {
		code := "OK"
		if cancelled {
			code = "CLIENT_CANCELLED"
		} else if upstreamFailed {
			code = "UPSTREAM_IO"
		} else if failed {
			code = "HTTP_ERROR"
		}
		operation.Stage(s.ctx, "cast.stream.response", applog.Fields{"method": r.Method, "has_range": r.Header.Get("Range") != "", "http_status": result.status, "upstream_status": upstreamStatus, "bytes": result.bytes, "resource_kind": category, "failed": failed, "code": code, "elapsed_ms": time.Since(began).Milliseconds()})
	}
}

type castResponse struct {
	http.ResponseWriter
	status    int
	bytes     int64
	wrote     bool
	features  bool
	failed    bool
	firstData func(int)
	onBytes   func(int)
}

func (w *castResponse) WriteHeader(status int) {
	if w.wrote {
		return
	}
	w.wrote = true
	w.status = status
	if w.features {
		op := "00"
		if w.Header().Get("Accept-Ranges") == "bytes" || strings.HasPrefix(w.Header().Get("Content-Range"), "bytes ") {
			op = "01"
		}
		w.Header().Set("contentFeatures.dlna.org", "DLNA.ORG_OP="+op+";DLNA.ORG_CI=0")
	}
	w.ResponseWriter.WriteHeader(status)
}
func (w *castResponse) Write(data []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(data)
	if n > 0 && w.bytes == 0 && w.status < 400 && w.firstData != nil {
		w.firstData(n)
	}
	w.bytes += int64(n)
	if w.onBytes != nil {
		w.onBytes(n)
	}
	if err != nil {
		w.failed = true
	}
	return n, err
}
func (w *castResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Owns verifies a child TrackURI against resources issued by this exact
// session. A matching host alone cannot identify a video.
func (b *CastRelay) Owns(media, resource string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.streams {
		if s.root != media {
			continue
		}
		id, ok := strings.CutPrefix(resource, s.base)
		if !ok {
			return false
		}
		s.mu.Lock()
		_, exists := s.resources[id]
		closed := s.closed
		s.mu.Unlock()
		return exists && !closed
	}
	return false
}

// Failed reports transport evidence without exposing capability URLs or source
// responses through the application binding.
func (b *CastRelay) Failed(media string) bool { return b.Health(media) == "failed" }

// Health preserves receiver transport status independently of local I/O evidence.
func (b *CastRelay) Health(media string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.streams {
		if s.root != media {
			continue
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.health == "startup_timeout" {
			return "failed"
		}
		if !s.failureAt.IsZero() && s.failureAt.After(s.recoveredAt) && time.Since(s.failureAt) < 30*time.Second {
			if s.dataSeen {
				return "degraded"
			}
			return "failed"
		}
		if s.dataSeen {
			return "media_received"
		}
		return s.health
	}
	return "revoked"
}

// Startup deadlines belong to Go and continue while the WebView is hidden.
// Once media arrives, idle/pause is not inferred from absence of LAN requests.
func (s *castStream) checkStartup() {
	s.mu.Lock()
	if s.closed || s.dataSeen || s.paused {
		s.mu.Unlock()
		return
	}
	phase := "no_request"
	if s.requests > 0 {
		phase = "probe_only"
	}
	if s.getSeen {
		phase = "no_media"
	}
	if s.playlistSeen {
		phase = "playlist_only"
	}
	if s.health == "key_received" || s.health == "init_received" || s.health == "unknown_received" {
		phase = s.health
	}
	s.health = "startup_timeout"
	s.mu.Unlock()
	operation.Stage(s.ctx, "cast.stream.receiver_timeout", applog.Fields{"code": "CAST_RECEIVER_NOT_FETCHING", "phase": phase})
	s.mu.Lock()
	if !s.closed && !s.paused {
		s.watch = time.AfterFunc(75*time.Second, func() {
			s.mu.Lock()
			closeStream := !s.dataSeen && !s.paused
			s.mu.Unlock()
			if closeStream {
				s.close()
			}
		})
	}
	s.mu.Unlock()
}
func (s *castStream) expire() {
	s.mu.Lock()
	if s.inflight > 0 && !s.closed {
		s.idle.Reset(30 * time.Minute)
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	s.close()
}
func (s *castStream) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.resources = nil

	s.resourceBytes = 0
	if s.watch != nil {
		s.watch.Stop()
	}
	if s.idle != nil {
		s.idle.Stop()
	}
	if s.stopOwner != nil {
		s.stopOwner()
	}
	fields := applog.Fields{"requests": s.requests, "failed_requests": s.failures, "recovered_requests": s.recoveries, "bytes": s.transferred, "receiver_mismatch": s.rejected["receiver_mismatch"], "resource_revoked": s.rejected["resource_revoked"], "concurrency_limit": s.rejected["concurrency_limit"], "relay_health": s.health}
	s.mu.Unlock()
	s.cancel()
	_ = s.server.Close()
	s.owner.mu.Lock()
	delete(s.owner.streams, s)
	if len(s.owner.streams)+s.owner.pending == 0 && s.owner.active != nil {
		s.owner.active(s.ctx, false)
	}
	s.owner.mu.Unlock()
	operation.Stage(s.ctx, "cast.stream.closed", fields)
}

// Close revokes every capability and waits for the listeners to stop.
func (b *CastRelay) Close(ctx context.Context) error {
	b.cancel()
	b.mu.Lock()
	b.closed = true
	streams := make([]*castStream, 0, len(b.streams))
	for s := range b.streams {
		streams = append(streams, s)
	}
	b.mu.Unlock()
	for _, s := range streams {
		s.close()
	}
	for _, s := range streams {
		select {
		case <-s.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// SetPaused suspends startup deadlines for an explicitly paused transport.
// The bounded idle and absolute session lifetimes still release forgotten sessions.
func (b *CastRelay) SetPaused(media string, paused bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.streams {
		if s.root != media {
			continue
		}
		s.mu.Lock()
		if s.paused != paused && !s.closed {
			s.paused = paused
			if s.watch != nil {
				s.watch.Stop()
			}
			if !paused && !s.dataSeen {
				s.watch = time.AfterFunc(45*time.Second, s.checkStartup)
			}
		}
		s.mu.Unlock()
	}
}
