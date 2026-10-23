package cast

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEndpointPinning(t *testing.T) {
	peer := net.ParseIP("192.168.1.30")
	for _, raw := range []string{"http://192.168.1.30:1400/desc.xml", "http://192.168.1.30/desc.xml"} {
		if _, e := endpoint(raw, peer); e != nil {
			t.Fatal(e)
		}
	}
	for _, raw := range []string{"http://127.0.0.1/", "http://192.168.1.31/", "http://example.com/", "http://8.8.8.8/", "file:///etc/passwd", "http://u:p@192.168.1.30/", "http://192.168.1.30:99999/", "https://192.168.1.30/"} {
		if _, e := endpoint(raw, peer); e == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}
func TestMediaValidation(t *testing.T) {
	for _, u := range []string{"https://cdn.example/video.m3u8?x=1&y=2", "http://192.168.1.8/video.mp4"} {
		if e := validateMedia(u); e != nil {
			t.Fatal(e)
		}
	}
	for _, u := range []string{"https://wails.localhost/video", "http://localhost/x", "http://127.0.0.1/x", "http://10.0.2.2/x", "file:///tmp/x", "javascript:alert(1)", "http://169.254.169.254/x"} {
		if e := validateMedia(u); e == nil {
			t.Fatalf("accepted %s", u)
		}
	}
}
func TestPlayAndControlSOAP(t *testing.T) {
	var mu sync.Mutex
	var actions []string
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		defer r.Body.Close()
		if e := xml.Unmarshal(data, new(any)); e != nil {
			t.Error(e)
		}
		mu.Lock()
		actions = append(actions, r.Header.Get("SOAPAction"))
		bodies = append(bodies, string(data))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/xml")
		fmt.Fprint(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><Response/></s:Body></s:Envelope>`)
	}))
	defer server.Close()
	s := New(context.Background())
	s.devices["tv"] = renderer{Device: Device{ID: "tv", Name: "TV"}, endpoint: server.URL, service: "urn:schemas-upnp-org:service:AVTransport:1", seen: time.Now()}
	if e := s.Play("tv", "https://cdn.example/movie.m3u8?a=1&b=2", "A < B & C"); e != nil {
		t.Fatal(e)
	}
	for _, action := range []string{"Pause", "Play", "Seek", "Stop"} {
		if e := s.Control("tv", action, 3723); e != nil {
			t.Fatal(e)
		}
	}
	if len(actions) != 6 || !strings.HasSuffix(actions[0], `#SetAVTransportURI"`) || !strings.HasSuffix(actions[1], `#Play"`) {
		t.Fatal(actions)
	}
	if !strings.Contains(bodies[0], "a=1&amp;b=2") || !strings.Contains(bodies[0], "A &amp;lt; B &amp;amp; C") || !strings.Contains(bodies[4], "01:02:03") {
		t.Fatal(bodies)
	}
	if e := s.Control("tv", "Delete", 0); e == nil {
		t.Fatal("arbitrary action accepted")
	}
	if e := s.Control("tv", "Seek", math.NaN()); e == nil {
		t.Fatal("NaN accepted")
	}
}
func TestSOAPFaultAndOversize(t *testing.T) {
	for _, body := range []string{`<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError><errorCode>701</errorCode><errorDescription>Transition not available</errorDescription></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, strings.Repeat("x", (256<<10)+1)} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		e := soap(context.Background(), renderer{endpoint: server.URL, service: "urn:schemas-upnp-org:service:AVTransport:1"}, "Pause", "")
		server.Close()
		if e == nil {
			t.Fatal("bad response accepted")
		}
	}
}
func TestCancelledDiscoveryReturnsPromptly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	started := time.Now()
	_, _ = New(ctx).Discover()
	if time.Since(started) > time.Second {
		t.Fatal("cancelled scan retained UDP socket")
	}
}
func TestResolveDescriptionPinsAllURLs(t *testing.T) {
	cases := []struct {
		name, base, control string
		status              int
		want                bool
	}{
		{"relative", "", "/AVTransport", 200, true},
		{"URLBase", "http://192.168.1.30:8000/device/", "control", 200, true},
		{"off-device-base", "http://192.168.1.31/", "control", 200, false},
		{"off-device-control", "", "http://127.0.0.1/control", 200, false},
		{"public-control", "", "http://8.8.8.8/control", 200, false},
		{"empty-control", "", "", 200, false},
		{"http-failure", "", "/control", 500, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				fmt.Fprintf(w, `<root><URLBase>%s</URLBase><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>客厅电视</friendlyName><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>%s</controlURL></service></serviceList></device></root>`, tc.base, tc.control)
			}))
			defer server.Close()
			local := strings.TrimPrefix(server.URL, "http://")
			c := client()
			c.Transport = &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				return (&net.Dialer{}).DialContext(ctx, network, local)
			}}
			device, e := resolveWithClient(context.Background(), "http://192.168.1.30/desc.xml", net.ParseIP("192.168.1.30"), c)
			if (e == nil) != tc.want {
				t.Fatalf("got %v, want success=%v", e, tc.want)
			}
			if tc.want && (device.Name != "客厅电视" || device.ID == "" || !strings.HasPrefix(device.endpoint, "http://192.168.1.30")) {
				t.Fatal(device)
			}
		})
	}
}
func TestSOAPCancellationAndDeviceExpiry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := soap(ctx, renderer{endpoint: "http://192.168.1.30/control", service: "urn:schemas-upnp-org:service:AVTransport:1"}, "Pause", ""); e == nil {
		t.Fatal("cancelled request accepted")
	}
	s := New(context.Background())
	s.devices["old"] = renderer{seen: time.Now().Add(-3 * time.Hour)}
	if e := s.Play("old", "https://cdn.example/a.mp4", "Test"); e == nil {
		t.Fatal("expired device accepted")
	}
}
func TestSSDPDiscoveryRoundTripAndConcurrentScan(t *testing.T) {
	testSSDPDiscovery(t, false)
}

func TestSSDPBroadDiscoveryAfterPacketLoss(t *testing.T) {
	testSSDPDiscovery(t, true)
}

func testSSDPDiscovery(t *testing.T, broadOnly bool) {
	// Real LAN listeners can trigger Windows Firewall for each temporary test binary.
	// Keep this integration test opt-in instead of opening sockets in ordinary runs.
	if os.Getenv("CCZJ_TEST_LAN") != "1" {
		t.Skip("real LAN discovery requires CCZJ_TEST_LAN=1")
	}
	// Use an actual private interface, keeping the production sender-IP checks on.
	var ip net.IP
	addresses, e := net.InterfaceAddrs()
	if e != nil {
		t.Fatal(e)
	}
	for _, addr := range addresses {
		n, _, _ := net.ParseCIDR(addr.String())
		if n.To4() != nil && n.IsPrivate() && !n.IsLoopback() {
			ip = n
			break
		}
	}
	if ip == nil {
		t.Skip("no private IPv4 interface available")
	}
	listener, e := net.Listen("tcp4", net.JoinHostPort(ip.String(), "0"))
	if e != nil {
		t.Fatal(e)
	}
	httpServer := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `<root><device><deviceType>urn:schemas-upnp-org:device:MediaRenderer:1</deviceType><friendlyName>Discovery Test TV</friendlyName><serviceList><service><serviceType>urn:schemas-upnp-org:service:AVTransport:1</serviceType><controlURL>/transport</controlURL></service></serviceList></device></root>`)
	}))
	httpServer.Listener = listener
	httpServer.Start()
	defer httpServer.Close()
	socket, e := net.ListenUDP("udp4", &net.UDPAddr{IP: ip})
	if e != nil {
		t.Fatal(e)
	}
	defer socket.Close()
	socket.SetReadDeadline(time.Now().Add(5 * time.Second))
	received := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 2048)
		var peer *net.UDPAddr
		searches := map[string]int{}
		for {
			n, from, err := socket.ReadFromUDP(buf)
			if err != nil {
				return
			}
			if peer == nil {
				close(received)
			}
			peer = from
			query := string(buf[:n])
			if !strings.HasPrefix(query, "M-SEARCH * HTTP/1.1") {
				continue
			}
			if !broadOnly {
				break
			}
			// This renderer answers only the root-device query, and its first
			// reply is lost. Both are valid conditions on a real Wi-Fi LAN.
			if strings.Contains(query, "ST: upnp:rootdevice\r\n") {
				searches["root"]++
				if searches["root"] >= 2 {
					break
				}
			}
		}
		packet := "HTTP/1.1 200 OK\r\nLOCATION: " + httpServer.URL + "/description.xml\r\nST: urn:schemas-upnp-org:device:MediaRenderer:1\r\n\r\n"
		socket.WriteToUDP([]byte(packet), peer)
		// Duplicate and hostile responses must neither duplicate devices nor fetch outside the sender.
		socket.WriteToUDP([]byte(packet), peer)
		socket.WriteToUDP([]byte("HTTP/1.1 200 OK\r\nLOCATION: http://127.0.0.1/private\r\n\r\n"), peer)
	}()
	s := New(context.Background())
	s.devices["active"] = renderer{Device: Device{ID: "active"}, seen: time.Now().Add(-5 * time.Minute)}
	result := make(chan []Device, 1)
	errc := make(chan error, 1)
	go func() { devices, e := s.discoverAt(socket.LocalAddr().(*net.UDPAddr)); result <- devices; errc <- e }()
	select {
	case <-received:
	case <-time.After(2 * time.Second):
		t.Fatal("no discovery query")
	}
	if _, e = s.Discover(); e == nil {
		t.Fatal("overlapping scan accepted")
	}
	devices := <-result
	if e = <-errc; e != nil {
		t.Fatal(e)
	}
	<-done
	if len(devices) != 1 || devices[0].Name != "Discovery Test TV" {
		t.Fatal(devices)
	}
	if _, e = s.device("active"); e != nil {
		t.Fatal("rescan discarded an active cast session", e)
	}
}
