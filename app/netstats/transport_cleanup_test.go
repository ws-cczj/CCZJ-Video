package netstats

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestClientCanCloseWrappedIdleConnections(t *testing.T) {
	closed := make(chan struct{}, 1)
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "ok") }))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateClosed {
			select {
			case closed <- struct{}{}:
			default:
			}
		}
	}
	server.Start()
	defer server.Close()
	base := &http.Transport{}
	defer base.CloseIdleConnections()
	client := &http.Client{Transport: WrapTransport(CategoryUpdate, base)}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	client.CloseIdleConnections()
	select {
	case <-closed:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("wrapped transport swallowed CloseIdleConnections; idle sockets and goroutines remain alive")
	}
}
