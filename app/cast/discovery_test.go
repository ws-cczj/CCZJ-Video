package cast

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"
)

func TestDiscoverySelectsEveryUsableLANNotDefaultRoute(t *testing.T) {
	flags := net.FlagUp | net.FlagMulticast
	interfaces := []net.Interface{{Index: 1, Flags: flags, Name: "wifi"}, {Index: 2, Flags: flags, Name: "ethernet"}, {Index: 3, Flags: net.FlagMulticast, Name: "down"}, {Index: 4, Flags: flags | net.FlagLoopback, Name: "loopback"}, {Index: 5, Flags: net.FlagUp | net.FlagPointToPoint, Name: "vpn"}, {Index: 6, Flags: flags, Name: "unreadable"}}
	addresses := func(network net.Interface) ([]net.Addr, error) {
		if network.Index == 6 {
			return nil, errors.New("not available")
		}
		if network.Index != 1 && network.Index != 2 {
			t.Error("queried unusable interface", network.Name)
		}
		if network.Index == 1 {
			return []net.Addr{&net.IPNet{IP: net.ParseIP("192.168.1.8"), Mask: net.CIDRMask(24, 32)}, &net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)}}, nil
		}
		return []net.Addr{&net.IPNet{IP: net.ParseIP("10.0.0.8"), Mask: net.CIDRMask(24, 32)}, &net.IPNet{IP: net.ParseIP("192.168.1.8"), Mask: net.CIDRMask(24, 32)}, &net.IPNet{IP: net.ParseIP("8.8.8.8"), Mask: net.CIDRMask(24, 32)}}, nil
	}
	links := discoveryLinks(interfaces, addresses)
	if len(links) != 2 || links[0].ip.String() != "192.168.1.8" || links[1].ip.String() != "10.0.0.8" {
		t.Fatalf("lost a LAN or accepted non-LAN: %+v", links)
	}
	if got := discoveryLinks(interfaces[:1], func(net.Interface) ([]net.Addr, error) { return nil, errors.New("gone") }); len(got) != 0 {
		t.Fatal(got)
	}
}

func TestDiscoveryLANSocketSelectionAndCancellation(t *testing.T) {
	// opt-in: multicast sends use real interfaces and can trigger host firewall UI.
	if os.Getenv("CCZJ_TEST_LAN") != "1" {
		t.Skip("real LAN discovery requires CCZJ_TEST_LAN=1")
	}
	group := &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	sockets, err := discoverySockets(group)
	if err != nil {
		t.Fatal(err)
	}
	for _, socket := range sockets {
		if !socket.LocalAddr().(*net.UDPAddr).IP.IsPrivate() {
			t.Error("wildcard rather than LAN socket", socket.LocalAddr())
		}
		_ = socket.Close()
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := New(context.Background())
	done := make(chan error, 1)
	go func() { _, err := s.DiscoverContext(ctx); done <- err }()
	time.Sleep(30 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scan retained sockets on cancel")
	}
}
