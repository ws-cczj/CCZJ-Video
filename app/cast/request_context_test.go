package cast

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

func TestPlaybackAndControlsCancelActualSOAPWithoutStoppingApplication(t *testing.T) {
	for _, action := range []string{"play", "pause"} {
		t.Run(action, func(t *testing.T) {
			started, closed, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, err := io.Copy(io.Discard, r.Body); err != nil {
					return
				}
				_ = r.Body.Close()
				count.Add(1)
				close(started)
				select {
				case <-r.Context().Done():
					close(closed)
				case <-release:
				}
			}))
			defer server.Close()
			defer close(release)
			s := New(context.Background())
			s.devices["tv"] = renderer{endpoint: server.URL, service: "urn:schemas-upnp-org:service:AVTransport:1", seen: time.Now()}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if action == "play" {
					done <- s.PlayContext(ctx, "tv", "https://cdn.example/test.mp4", "Test")
				} else {
					done <- s.ControlContext(ctx, "tv", "Pause", 0)
				}
			}()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("SOAP did not start")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			case <-time.After(time.Second):
				t.Fatal("request cancellation did not stop SOAP")
			}
			select {
			case <-closed:
			case <-time.After(time.Second):
				t.Fatal("upstream connection remains open")
			}
			if count.Load() != 1 {
				t.Fatal("cancelled URI setup continued to the Play step")
			}
			if s.ctx.Err() != nil {
				t.Fatal("request cancellation stopped application owner")
			}
		})
	}
}
