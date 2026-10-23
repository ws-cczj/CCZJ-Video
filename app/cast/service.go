package cast

import (
	"cczjVideo/app/netpolicy"
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
	endpoint string
	service  string
	seen     time.Time
}
type Service struct {
	ctx     context.Context
	mu      sync.Mutex
	scanMu  sync.Mutex
	devices map[string]renderer
}

func New(ctx context.Context) *Service { return &Service{ctx: ctx, devices: make(map[string]renderer)} }

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
func soap(ctx context.Context, d renderer, action string, args string) error {
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
	data, e := readBounded(response.Body)
	if e != nil {
		return e
	}
	var envelope struct {
		Body struct {
			Fault *struct {
				Code        string `xml:"detail>UPnPError>errorCode"`
				Description string `xml:"detail>UPnPError>errorDescription"`
			} `xml:"Fault"`
		} `xml:"Body"`
	}
	if e = xml.Unmarshal(data, &envelope); e != nil {
		return fmt.Errorf("DLNA 返回无效响应: %w", e)
	}
	if fault := envelope.Body.Fault; fault != nil {
		return fmt.Errorf("DLNA %s: %s", fault.Code, fault.Description)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("DLNA HTTP %d", response.StatusCode)
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
	if e := validateMedia(media); e != nil {
		return e
	}
	d, e := s.device(id)
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(s.ctx, 8*time.Second)
	defer cancel()
	mime := "video/mp4"
	u, _ := url.Parse(media)
	if strings.HasSuffix(strings.ToLower(u.Path), ".m3u8") {
		mime = "application/vnd.apple.mpegurl"
	}
	metadata := `<DIDL-Lite xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/"><item id="0" parentID="-1" restricted="1"><dc:title>` + escaped(title) + `</dc:title><upnp:class>object.item.videoItem</upnp:class><res protocolInfo="http-get:*:` + mime + `:*">` + escaped(media) + `</res></item></DIDL-Lite>`
	if e = soap(ctx, d, "SetAVTransportURI", "<CurrentURI>"+escaped(media)+"</CurrentURI><CurrentURIMetaData>"+escaped(metadata)+"</CurrentURIMetaData>"); e != nil {
		return e
	}
	return soap(ctx, d, "Play", "<Speed>1</Speed>")
}
func (s *Service) Control(id, action string, seconds float64) error {
	d, e := s.device(id)
	if e != nil {
		return e
	}
	args := ""
	switch action {
	case "Play":
		args = "<Speed>1</Speed>"
	case "Pause", "Stop":
	case "Seek":
		if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds < 0 || seconds > 7*24*3600 {
			return errors.New("无效的投屏进度")
		}
		n := int(seconds)
		args = fmt.Sprintf("<Unit>REL_TIME</Unit><Target>%02d:%02d:%02d</Target>", n/3600, n/60%60, n%60)
	default:
		return errors.New("不支持的投屏操作")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 4*time.Second)
	defer cancel()
	return soap(ctx, d, action, args)
}
