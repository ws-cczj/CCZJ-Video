package cast

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/lifecycle"
	"cczjVideo/app/netpolicy"
	"cczjVideo/app/operation"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Device exposes an opaque identifier. Control URLs never come from frontend input.
type Device struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type renderer struct {
	Device
	endpoint      string
	service       string
	seen          time.Time
	session       string
	media         string
	localAddress  string
	streamRelease func()
	command       chan struct{}
	playback      playbackEvidence
	statusKey     string
	statusAt      time.Time
}
type Service struct {
	ctx     context.Context
	mu      sync.Mutex
	scanMu  sync.Mutex
	devices map[string]renderer
	source  MediaSource
}

// MediaSource prepares a receiver-accessible resource and owns its transport.
// The release function must be idempotent; Failed reports media I/O failures.
type MediaSource interface {
	Prepare(context.Context, string, string, string) (string, string, func(), error)
	Failed(string) bool
	Owns(string, string) bool
}

func New(ctx context.Context) *Service { return &Service{ctx: ctx, devices: make(map[string]renderer)} }

// NewWithMedia connects receiver control to the application's media transport.
func NewWithMedia(ctx context.Context, source MediaSource) *Service {
	s := New(ctx)
	s.source = source
	return s
}

// LAN-only, IPv4-literal endpoints are pinned to the SSDP datagram's sender.
// This excludes redirects, DNS rebinding, public servers and loopback services.
func endpoint(raw string, peer net.IP) (*url.URL, error) {
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "http" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("无效的 DLNA 设备地址")
	}
	ip := net.ParseIP(u.Hostname())
	if ip == nil || !ip.Equal(peer) || !ip.IsPrivate() || ip.IsLoopback() || ip.To4() == nil {
		return nil, errors.New("DLNA 地址必须属于发现的局域网设备")
	}
	if p := u.Port(); p != "" {
		n, e := strconv.Atoi(p)
		if e != nil || n < 1 || n > 65535 {
			return nil, errors.New("无效的设备端口")
		}
	}
	return u, nil
}
func client() *http.Client {
	return &http.Client{Timeout: 3 * time.Second, Transport: netpolicy.WrapTransport("cast", &http.Transport{Proxy: nil, DisableKeepAlives: true, ResponseHeaderTimeout: 2 * time.Second}), CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("DLNA 不接受设备重定向") }}
}
func readBounded(r io.Reader) ([]byte, error) {
	b, e := io.ReadAll(io.LimitReader(r, (256<<10)+1))
	if len(b) > 256<<10 {
		return nil, errors.New("DLNA 响应过大")
	}
	return b, e
}

type description struct {
	Base   string `xml:"URLBase"`
	Device node   `xml:"device"`
}
type node struct {
	Name     string    `xml:"friendlyName"`
	Type     string    `xml:"deviceType"`
	Services []service `xml:"serviceList>service"`
	Children []node    `xml:"deviceList>device"`
}
type service struct {
	Type    string `xml:"serviceType"`
	Control string `xml:"controlURL"`
}

func transport(n node) (string, service, bool) {
	if strings.HasPrefix(n.Type, "urn:schemas-upnp-org:device:MediaRenderer:") {
		for _, s := range n.Services {
			if strings.HasPrefix(s.Type, "urn:schemas-upnp-org:service:AVTransport:") {
				v := strings.TrimPrefix(s.Type, "urn:schemas-upnp-org:service:AVTransport:")
				if version, e := strconv.Atoi(v); e == nil && version >= 1 && version <= 3 {
					return n.Name, s, true
				}
			}
		}
	}
	for _, child := range n.Children {
		if name, s, ok := transport(child); ok {
			return name, s, true
		}
	}
	return "", service{}, false
}
func resolve(ctx context.Context, raw string, peer net.IP) (renderer, error) {
	return resolveWithClient(ctx, raw, peer, client())
}
func resolveWithClient(ctx context.Context, raw string, peer net.IP, httpClient *http.Client) (renderer, error) {
	u, e := endpoint(raw, peer)
	if e != nil {
		return renderer{}, e
	}
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if e != nil {
		return renderer{}, e
	}
	res, e := httpClient.Do(req)
	if e != nil {
		return renderer{}, e
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return renderer{}, fmt.Errorf("DLNA 设备描述 HTTP %d", res.StatusCode)
	}
	body, e := readBounded(res.Body)
	if e != nil {
		return renderer{}, e
	}
	var doc description
	if e = xml.Unmarshal(body, &doc); e != nil {
		return renderer{}, e
	}
	name, svc, ok := transport(doc.Device)
	if !ok {
		return renderer{}, errors.New("设备没有视频投屏服务")
	}
	base := u
	if doc.Base != "" {
		base, e = endpoint(strings.TrimSpace(doc.Base), peer)
		if e != nil {
			return renderer{}, e
		}
	}
	relative, e := url.Parse(strings.TrimSpace(svc.Control))
	if e != nil || svc.Control == "" {
		return renderer{}, errors.New("设备缺少控制地址")
	}
	control, e := endpoint(base.ResolveReference(relative).String(), peer)
	if e != nil {
		return renderer{}, e
	}
	hash := sha256.Sum256([]byte(control.String()))
	if name == "" {
		name = peer.String()
	}
	if len(name) > 160 {
		name = name[:160]
	}
	return renderer{Device: Device{ID: hex.EncodeToString(hash[:12]), Name: name}, endpoint: control.String(), service: svc.Type, seen: time.Now()}, nil
}

func (s *Service) Discover() ([]Device, error) {
	return s.DiscoverContext(context.Background())
}

func (s *Service) DiscoverContext(ctx context.Context) ([]Device, error) {
	return s.discoverAtContext(ctx, &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900})
}

func escaped(value string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

type soapEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Fault *struct {
			Code        string `xml:"detail>UPnPError>errorCode"`
			Description string `xml:"detail>UPnPError>errorDescription"`
		} `xml:"Fault"`
		Transport struct {
			State  string `xml:"CurrentTransportState"`
			Status string `xml:"CurrentTransportStatus"`
		} `xml:"GetTransportInfoResponse"`
		Position struct {
			Time     string `xml:"RelTime"`
			Duration string `xml:"TrackDuration"`
			URI      string `xml:"TrackURI"`
		} `xml:"GetPositionInfoResponse"`
		Media struct {
			URI string `xml:"CurrentURI"`
		} `xml:"GetMediaInfoResponse"`
	} `xml:"Body"`
}

type soapFault struct{ code, description string }

func (e *soapFault) Error() string {
	return fmt.Sprintf("DLNA %s: %s", e.code, applog.DiagnosticText(e.description, 256))
}

func faultCode(err error, codes ...string) bool {
	var fault *soapFault
	if !errors.As(err, &fault) {
		return false
	}
	for _, code := range codes {
		if fault.code == code {
			return true
		}
	}
	return false
}

func soap(ctx context.Context, d renderer, action, args string, output ...*soapEnvelope) (failure error) {
	ctx = traceDevice(ctx, d)
	start := time.Now()
	fields := applog.Fields{"action": action}
	query := strings.HasPrefix(action, "Get")
	if !query {
		operation.Stage(ctx, "cast.soap.start", fields)
	}
	defer func() {
		fields["elapsed_ms"] = elapsed(start)
		if failure != nil {
			traceFailure(ctx, "cast.soap.failed", failure, fields)
		} else if !query {
			operation.Stage(ctx, "cast.soap.complete", fields)
		}
	}()
	body := `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` + action + ` xmlns:u="` + d.service + `"><InstanceID>0</InstanceID>` + args + `</u:` + action + `></s:Body></s:Envelope>`
	req, e := http.NewRequestWithContext(ctx, http.MethodPost, d.endpoint, strings.NewReader(body))
	if e != nil {
		return e
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPAction", `"`+d.service+`#`+action+`"`)
	response, e := client().Do(req)
	if e != nil {
		return e
	}
	defer response.Body.Close()
	fields["http_status"] = response.StatusCode
	data, e := readBounded(response.Body)
	fields["response_bytes"], fields["response_sha256"] = len(data), applog.Fingerprint(data)
	if e != nil {
		fields["code"] = "CAST_RESPONSE_TOO_LARGE"
		return e
	}
	var envelope soapEnvelope
	if e = xml.Unmarshal(data, &envelope); e != nil {
		fields["code"] = "CAST_INVALID_RESPONSE"
		return fmt.Errorf("DLNA 返回无效响应: %w", e)
	}
	if fault := envelope.Body.Fault; fault != nil {
		fields["code"] = "CAST_SOAP_FAULT"
		fields["upnp_code"] = fault.Code
		return &soapFault{code: fault.Code, description: fault.Description}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		fields["code"] = "CAST_HTTP_ERROR"
		return fmt.Errorf("DLNA HTTP %d", response.StatusCode)
	}
	if len(output) > 0 && output[0] != nil {
		*output[0] = envelope
	}
	return nil
}

func (s *Service) device(id string) (renderer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d, ok := s.devices[id]
	if !ok || time.Since(d.seen) > 2*time.Hour {
		return renderer{}, errors.New("设备已失效，请重新搜索")
	}
	return d, nil
}
func validateMedia(raw string) error {
	u, e := url.Parse(raw)
	if e != nil || len(raw) > 16384 || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
		return errors.New("投屏需要 HTTP/HTTPS 视频地址")
	}
	host := strings.ToLower(u.Hostname())
	ip := net.ParseIP(host)
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || host == "10.0.2.2" || host == "10.0.3.2" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified() || ip.IsMulticast() || ip.IsLinkLocalUnicast())) {
		return errors.New("电视无法访问本机代理地址，请使用原始视频地址")
	}
	return nil
}
func (s *Service) Play(id, media, title string) error {
	return s.PlayContext(context.Background(), id, media, title)
}

// PlayContext ties both SOAP steps to the caller and application lifetime.
func (s *Service) PlayContext(request context.Context, id, media, title string) error {
	ctx, release := lifecycle.WithShutdown(request, s.ctx)
	defer release()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if e := validateMedia(media); e != nil {
		traceFailure(ctx, "cast.media.rejected", e, applog.Fields{"code": "CAST_INVALID_MEDIA"})
		return e
	}
	d, releaseCommand, e := s.ownCommand(ctx, id)
	if e != nil {
		traceFailure(ctx, "cast.device.unavailable", e, applog.Fields{"cast_device_id": id})
		return e
	}
	defer releaseCommand()
	previousRelease := d.streamRelease
	d.session = apperror.NewRequestID()
	d.playback = playbackEvidence{}
	d.statusKey = ""
	d.statusAt = time.Time{}
	ctx = traceDevice(ctx, d)
	operation.Stage(ctx, "cast.media.prepare", applog.Fields{"transport": map[bool]string{true: "relay", false: "direct"}[s.source != nil]})
	mime := "video/mp4"
	u, _ := url.Parse(media)
	if strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") {
		mime = "application/vnd.apple.mpegurl"
	}
	if s.source != nil {
		var err error
		media, mime, d.streamRelease, err = s.source.Prepare(ctx, d.endpoint, d.localAddress, media)
		if err != nil {
			traceFailure(ctx, "cast.media.prepare_failed", err, applog.Fields{"code": "CAST_STREAM_PREPARE_FAILED"})
			return err
		}
	}
	d.media = media
	s.mu.Lock()
	s.devices[d.ID] = d
	s.mu.Unlock()
	if previousRelease != nil {
		previousRelease()
	}
	started := false
	defer func() {
		if !started {
			s.releaseStream(d)
		}
	}()
	metadata := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="0" parentID="-1" restricted="1"><dc:title>` + escaped(title) + `</dc:title><upnp:class>object.item.videoItem</upnp:class><res protocolInfo="http-get:*:` + mime + `:*">` + escaped(media) + `</res></item></DIDL-Lite>`
	setURI := func(metadata string) error {
		return soap(ctx, d, "SetAVTransportURI", "<CurrentURI>"+escaped(media)+"</CurrentURI><CurrentURIMetaData>"+escaped(metadata)+"</CurrentURIMetaData>")
	}
	e = setURI(metadata)
	// Some renderers reject DIDL metadata even though they can inspect the URI.
	// Retry only explicit argument/MIME rejection, never an ambiguous timeout.
	if faultCode(e, "402", "714") {
		operation.Stage(ctx, "cast.media.metadata_fallback", applog.Fields{"attempt": 1})
		e = setURI("")
	}
	if e != nil {
		return e
	}
	for attempt := 0; ; attempt++ {
		operation.Stage(ctx, "cast.play.attempt", applog.Fields{"attempt": attempt})
		e = soap(ctx, d, "Play", "<Speed>1</Speed>")
		if !faultCode(e, "701") || attempt == 2 {
			if e == nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				s.accepted(ctx, d)
				started = true
			}
			return e
		}
		// SetURI may complete before the renderer leaves its transition state.
		timer := time.NewTimer(300 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (s *Service) Control(id, action string, seconds float64) error {
	return s.ControlContext(context.Background(), id, action, seconds)
}

// ControlContext stops an in-flight device request when its owner leaves.
func (s *Service) ControlContext(request context.Context, id, action string, seconds float64) error {
	ctx, release := lifecycle.WithShutdown(request, s.ctx)
	defer release()
	budget := 4 * time.Second
	if action == "Seek" {
		budget = 8 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	d, releaseCommand, e := s.ownCommand(ctx, id)
	if e != nil {
		traceFailure(ctx, "cast.device.unavailable", e, applog.Fields{"cast_device_id": id, "action": action})
		return e
	}
	defer releaseCommand()
	ctx = traceDevice(ctx, d)
	args := ""
	switch action {
	case "Play":
		args = "<Speed>1</Speed>"
	case "Pause", "Stop":
	case "Seek":
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 7*24*3600 {
			err := errors.New("无效的投屏进度")
			traceFailure(ctx, "cast.control.rejected", err, applog.Fields{"action": action, "code": "CAST_INVALID_POSITION"})
			return err
		}
		if err := s.requireSeekReady(ctx, d); err != nil {
			return err
		}
		n := int(seconds)
		args = fmt.Sprintf("<Unit>REL_TIME</Unit><Target>%02d:%02d:%02d</Target>", n/3600, n/60%60, n%60)
	default:
		err := errors.New("不支持的投屏操作")
		traceFailure(ctx, "cast.control.rejected", err, applog.Fields{"action": action, "code": "CAST_UNSUPPORTED_ACTION"})
		return err
	}
	fields := applog.Fields{"action": action}
	if action == "Seek" {
		fields["target_seconds"] = int(seconds)
	}
	operation.Stage(ctx, "cast.control.dispatch", fields)
	err := soap(ctx, d, action, args)
	if err == nil && (action == "Pause" || action == "Play") {
		if source, ok := s.source.(interface{ SetPaused(string, bool) }); ok {
			source.SetPaused(d.media, action == "Pause")
		}
	}
	if err == nil && (action == "Seek" || action == "Stop") {
		s.resetSeekSample(d, action == "Stop")
		if action == "Stop" {
			s.releaseStream(d)
		}
	}
	if action != "Seek" || !faultCode(err, "501", "701") {
		return err
	}
	// Some renderers accept time seeking only while playing. Recover only
	// after confirming that the paused resource still belongs to this session.
	status, statusErr := s.StatusContext(ctx, id)
	if statusErr != nil || status.State != "PAUSED_PLAYBACK" || !s.seekReady(d, status) {
		return err
	}
	operation.Stage(ctx, "cast.seek.paused_fallback", applog.Fields{"target_seconds": int(seconds)})
	err = s.seekPaused(ctx, d, args)
	if err == nil {
		s.resetSeekSample(d, false)
	}
	return err
}

func (s *Service) releaseStream(d renderer) {
	s.mu.Lock()
	current, ok := s.devices[d.ID]
	var release func()
	if ok && current.session == d.session {
		release = current.streamRelease
		current.streamRelease = nil
		s.devices[d.ID] = current
	}
	s.mu.Unlock()
	if release != nil {
		release()
	}
}

func (s *Service) seekPaused(ctx context.Context, d renderer, args string) (failure error) {
	// Restore the user's paused state even if the caller cancels after Play.
	// Application shutdown still cancels the bounded cleanup request.
	defer func() {
		restore, release := lifecycle.WithShutdown(context.WithoutCancel(ctx), s.ctx)
		defer release()
		restore, cancel := context.WithTimeout(restore, 2*time.Second)
		defer cancel()
		if err := soap(restore, d, "Pause", ""); err != nil {
			failure = errors.Join(failure, fmt.Errorf("恢复电视暂停状态失败: %w", err))
		} else if err := waitTransport(restore, d, "PAUSED_PLAYBACK"); err != nil {
			failure = errors.Join(failure, fmt.Errorf("未能确认电视恢复暂停: %w", err))
		}
	}()
	if err := soap(ctx, d, "Play", "<Speed>1</Speed>"); err != nil {
		return err
	}
	if err := waitTransport(ctx, d, "PLAYING"); err != nil {
		return err
	}
	if err := s.requireSeekReady(ctx, d); err != nil {
		return err
	}
	return soap(ctx, d, "Seek", args)
}

func waitTransport(ctx context.Context, d renderer, state string) error {
	for attempt := 0; attempt < 20; attempt++ {
		var response soapEnvelope
		if err := soap(ctx, d, "GetTransportInfo", "", &response); err != nil {
			return err
		}
		if strings.TrimSpace(response.Body.Transport.State) == state {
			return nil
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return errors.New("电视尚未确认播放状态")
}

// ReleaseSessions revokes local resources at the owning mode boundary. It does
// not claim that an unreachable receiver has accepted Stop.
func (s *Service) ReleaseSessions() {
	s.mu.Lock()
	var releases []func()
	for id, d := range s.devices {
		if d.streamRelease != nil {
			releases = append(releases, d.streamRelease)
		}
		d.streamRelease = nil
		d.playback = playbackEvidence{}
		d.session = ""
		d.media = ""
		s.devices[id] = d
	}
	s.mu.Unlock()
	for _, release := range releases {
		release()
	}
}

// ReleaseSession ignores stale owners so a late UI cleanup cannot revoke a resend.
func (s *Service) ReleaseSession(ctx context.Context, id, session string) error {
	d, release, err := s.ownCommand(ctx, id)
	if err != nil {
		return err
	}
	defer release()
	if session == "" || d.session != session {
		return nil
	}
	s.resetSeekSample(d, true)
	s.releaseStream(d)
	operation.Stage(traceDevice(ctx, d), "cast.session.disconnected", nil)
	return nil
}
