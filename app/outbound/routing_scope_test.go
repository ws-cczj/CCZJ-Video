package outbound

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNonPoolInformationWithoutLocalProxyStaysDirect(t *testing.T) {
	var publicCalls atomic.Int32
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		publicCalls.Add(1)
		fmt.Fprint(w, "public")
	}))
	defer public.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "source")
	}))
	defer target.Close()
	p := fixturePool(t, "", public.URL)
	p.ready = make(chan struct{})
	base := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", target.Listener.Addr().String())
	}}
	client := &http.Client{Timeout: time.Second, Transport: WrapTransport("collect", base, identityTransport)}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://source.example/catalog")
	if got := responseText(t, response, err); got != "source" || publicCalls.Load() != 0 {
		t.Fatalf("local direct source used pool: body=%q public=%d", got, publicCalls.Load())
	}
}

func TestNonPoolInformationUsesOnlyLocalProxy(t *testing.T) {
	for _, category := range []string{"search", "collect", "reading", "novel", "comic", "image", "reading-image", "update", "unknown"} {
		for _, state := range []string{"healthy", "refreshing", "exhausted", "disabled"} {
			t.Run(category+"/"+state, func(t *testing.T) {
				var publicCalls, localCalls atomic.Int32
				public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					publicCalls.Add(1)
					fmt.Fprint(w, "public")
				}))
				defer public.Close()
				local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					localCalls.Add(1)
					if r.URL.Host != "source.example" || r.URL.RawQuery != "wd=video" || r.Header.Get("Referer") != "https://source.example/" {
						t.Error("local proxy lost source context")
					}
					fmt.Fprint(w, "local")
				}))
				defer local.Close()
				p := fixturePool(t, local.URL, public.URL)
				switch state {
				case "refreshing":
					p.nodes = nil
					p.ready = make(chan struct{})
				case "exhausted":
					p.nodes = nil
				case "disabled":
					p.config.Enabled = false
				}
				client := &http.Client{Timeout: 300 * time.Millisecond, Transport: WrapTransport(category, &http.Transport{}, identityTransport)}
				defer client.CloseIdleConnections()
				req, _ := http.NewRequestWithContext(context.Background(), "GET", "http://source.example/api?wd=video", nil)
				req.Header.Set("Referer", "https://source.example/")
				response, err := client.Do(req)
				if got := responseText(t, response, err); got != "local" || localCalls.Load() != 1 || publicCalls.Load() != 0 || p.Snapshot().PublicAttempts != 0 {
					t.Fatalf("%s used pool: body=%q local=%d public=%d", category, got, localCalls.Load(), publicCalls.Load())
				}
			})
		}
	}
}

func TestDanmakuGateway412CoolsOnlyItsSiteAndUsesNextNode(t *testing.T) {
	var blockedCalls, healthyCalls atomic.Int32
	newProxy := func(reject *atomic.Bool) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reject.Load() {
				blockedCalls.Add(1)
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
			healthyCalls.Add(1)
			fmt.Fprint(w, "comments")
		}))
	}
	var rejectA, rejectB atomic.Bool
	a, b := newProxy(&rejectA), newProxy(&rejectB)
	defer a.Close()
	defer b.Close()
	p := fixturePool(t, "", a.URL, b.URL)
	// The stable first route rejects this egress; the second serves comments.
	host := "api.bilibili.com"
	if p.candidates("bilibili.com")[0].URL == a.URL {
		rejectA.Store(true)
	} else {
		rejectB.Store(true)
	}
	client := &http.Client{Transport: WrapTransport("danmaku", &http.Transport{}, identityTransport)}
	defer client.CloseIdleConnections()
	response, err := client.Get("http://" + host + "/x/v2/dm/web/view?oid=1")
	if got := responseText(t, response, err); got != "comments" || blockedCalls.Load() != 1 || healthyCalls.Load() != 1 {
		t.Fatalf("412 did not advance node: result=%q rejected=%d healthy=%d", got, blockedCalls.Load(), healthyCalls.Load())
	}
	if rows := p.candidates("bilibili.com"); len(rows) != 1 || len(p.candidates("qq.com")) != 2 {
		t.Fatal("danmaku gateway refusal must not quarantine an unrelated provider")
	}
}

func TestDoubanImagesUsePoolButOtherImageOriginsStayLocal(t *testing.T) {
	public := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "public") }))
	defer public.Close()
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "local") }))
	defer local.Close()
	fixturePool(t, local.URL, public.URL)
	for _, category := range []string{"image", "reading-image"} {
		for _, item := range []struct{ host, want string }{
			{"img1.doubanio.com", "public"}, {"IMG9.DOUBANIO.COM.", "public"},
			{"img3.douban.com", "public"}, {"doubanio.com", "public"},
			{"doubanio.com.evil.example", "local"}, {"notdoubanio.com", "local"}, {"poster.example", "local"},
		} {
			t.Run(category+"/"+item.host, func(t *testing.T) {
				client := &http.Client{Transport: WrapTransport(category, &http.Transport{}, identityTransport)}
				defer client.CloseIdleConnections()
				req, _ := http.NewRequest("GET", "http://"+item.host+"/view/photo/l/public/p.jpg", nil)
				req.Header.Set("Referer", "https://movie.douban.com/")
				response, err := client.Do(req)
				if got := responseText(t, response, err); got != item.want {
					t.Fatalf("image origin %s routed to %s, want %s", item.host, got, item.want)
				}
			})
		}
	}
}
