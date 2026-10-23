package cast

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestDiscoveryRequestCancellationClosesSocketWithLiveOwner(t *testing.T) {
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer receiver.Close()
	_ = receiver.SetReadDeadline(time.Now().Add(time.Second))
	owner := New(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := owner.discoverAtContext(ctx, receiver.LocalAddr().(*net.UDPAddr)); done <- err }()
	if _, _, err = receiver.ReadFromUDP(make([]byte, 2048)); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("request cancellation retained discovery socket")
	}
}
