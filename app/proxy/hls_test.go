package proxy

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestHLSPlaylistRewritesAllURIForms(t *testing.T) {
	s := NewHLSService()
	base, err := url.Parse("https://media.example.test/path/master.m3u8?token=abc")
	if err != nil {
		t.Fatal(err)
	}
	playlist := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"keys/key.bin\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=1,URI=\"iframe.m3u8\"\nchild/level.m3u8\n"
	rewritten := s.rewritePlaylist(playlist, base, "")
	for _, upstream := range []string{
		"https://media.example.test/path/keys/key.bin",
		"https://media.example.test/path/init.mp4",
		"https://media.example.test/path/iframe.m3u8",
		"https://media.example.test/path/child/level.m3u8",
	} {
		want := s.URL(upstream)
		if !strings.Contains(rewritten, want) {
			t.Fatalf("missing rewritten URL for %s: %s", upstream, rewritten)
		}
	}
}

func TestHLSServeRewritesPlaylistAndStreamsMedia(t *testing.T) {
	s := NewHLSService()
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, ".m3u8") {
			return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}}, Body: io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n"))}, nil
		}
		return &http.Response{StatusCode: http.StatusPartialContent, Header: http.Header{"Content-Type": []string{"video/mp2t"}, "Content-Range": []string{"bytes 0-2/3"}}, Body: io.NopCloser(strings.NewReader("abc"))}, nil
	})

	playlistRequest := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+s.URL("https://1.1.1.1/index.m3u8"), nil)
	playlistRecorder := httptest.NewRecorder()
	s.ServeHTTP(playlistRecorder, playlistRequest)
	if playlistRecorder.Code != http.StatusOK || !strings.Contains(playlistRecorder.Body.String(), s.URL("https://1.1.1.1/segment.ts")) {
		t.Fatalf("playlist was not rewritten: status=%d body=%s", playlistRecorder.Code, playlistRecorder.Body.String())
	}

	mediaRequest := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+s.URL("https://1.1.1.1/segment.ts"), nil)
	mediaRequest.Header.Set("Range", "bytes=0-2")
	mediaRecorder := httptest.NewRecorder()
	s.ServeHTTP(mediaRecorder, mediaRequest)
	if mediaRecorder.Code != http.StatusPartialContent || mediaRecorder.Body.String() != "abc" || mediaRecorder.Header().Get("Content-Range") != "bytes 0-2/3" {
		t.Fatalf("media was not streamed correctly: status=%d headers=%v body=%s", mediaRecorder.Code, mediaRecorder.Header(), mediaRecorder.Body.String())
	}
}

func TestHLSProxyURLIsStableAndRelative(t *testing.T) {
	s := NewHLSService()
	u := "https://cdn.example.test/video/index.m3u8?x=1"
	if got := s.URL(u); got != s.URL(u) || !strings.HasPrefix(got, "/__cczj/hls?u=") {
		t.Fatalf("unexpected proxy URL: %s", got)
	}
}

// upstreamHost 挑一个公网地址：ValidateTarget 默认会拦私网，测试里绕不过去也不需要绕。
const upstreamHost = "https://1.1.1.1"

func TestHLSProxySendsRefererAndPropagatesItToOtherHosts(t *testing.T) {
	s := NewHLSService()
	seen := map[string]string{}
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		seen[req.URL.Host+req.URL.Path] = req.Header.Get("Referer")
		if strings.HasSuffix(req.URL.Path, ".m3u8") {
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
				Body:       io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\nhttps://2.2.2.2/other/away.ts\n")),
			}, nil
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": []string{"video/mp2t"}},
			Body:       io.NopCloser(strings.NewReader("abc")),
		}, nil
	})

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"http://wails.localhost"+s.URL(upstreamHost+"/p/index.m3u8?sig=secret"), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", rec.Code, rec.Body.String())
	}
	// 取列表这一跳：Referer 是列表自己的 origin，且不能把 ?sig= 这种签名带出去。
	if got := seen["1.1.1.1/p/index.m3u8"]; got != upstreamHost+"/" {
		t.Errorf("列表请求的 Referer = %q，期望 %q", got, upstreamHost+"/")
	}
	// 关键一步：跨到另一台主机的子资源，Referer 必须仍然是父列表的域——
	// CDN 校验的是「谁把我拉下来的」，拿子资源自己的域去送必然 403。
	body := rec.Body.String()
	if !strings.Contains(body, "ref=") {
		t.Fatalf("重写后的列表没有携带引用页上下文：%s", body)
	}
	away := "/__cczj/hls?u=" + base64.RawURLEncoding.EncodeToString([]byte("https://2.2.2.2/other/away.ts")) +
		"&ref=" + base64.RawURLEncoding.EncodeToString([]byte(upstreamHost+"/"))
	if !strings.Contains(body, strings.ReplaceAll(away, "&", "&")) {
		t.Fatalf("跨主机子资源没有带上父列表的 Referer：%s", body)
	}
	seg := httptest.NewRecorder()
	s.ServeHTTP(seg, httptest.NewRequest(http.MethodGet, "http://wails.localhost"+away, nil))
	if got := seen["2.2.2.2/other/away.ts"]; got != upstreamHost+"/" {
		t.Errorf("跨主机片段的 Referer = %q，期望父列表的 %q", got, upstreamHost+"/")
	}
}

func TestRefererOriginKeepsOnlySchemeAndHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://a.test/p/index.m3u8?sig=x", "https://a.test/"},
		{"http://a.test:8080/x", "http://a.test:8080/"},
		// 拒绝被用来冒充任意引用页：非 http(s)、缺主机、带 userinfo 都不放行。
		{"file:///etc/passwd", ""},
		// userinfo 不是「拒绝」而是被丢掉：输出只剩 scheme+host，
		// 所以带密码的引用页不会把密码送到上游。
		{"https://user:pw@a.test/x", "https://a.test/"},
		{"not-a-url", ""},
		{"", ""},
	} {
		if got := RefererOrigin(tc.in); got != tc.want {
			t.Errorf("RefererOrigin(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

func TestHLSProxyIgnoresOversizedRefParam(t *testing.T) {
	s := NewHLSService()
	var got string
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		got = req.Header.Get("Referer")
		return &http.Response{StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"video/mp2t"}},
			Body:   io.NopCloser(strings.NewReader("x"))}, nil
	})
	long := base64.RawURLEncoding.EncodeToString([]byte(strings.Repeat("a", maxRefBytes+10)))
	u := "/__cczj/hls?u=" + base64.RawURLEncoding.EncodeToString([]byte(upstreamHost+"/s.ts")) + "&ref=" + long
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://wails.localhost"+u, nil))
	// 超长时退回按上游推导，而不是把超长值原样送出。
	if got != upstreamHost+"/" {
		t.Errorf("超长 ref 生效了：Referer = %q", got)
	}
}

func TestHLSSegmentRequestCarriesUpstreamCookie(t *testing.T) {
	s := NewHLSService()
	var cookieAtSegment string
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.HasSuffix(req.URL.Path, ".m3u8") {
			return &http.Response{StatusCode: http.StatusOK,
				Header: http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"},
					"Set-Cookie": []string{"sess=abc; Path=/"}},
				Body: io.NopCloser(strings.NewReader("#EXTM3U\nsegment.ts\n"))}, nil
		}
		cookieAtSegment = req.Header.Get("Cookie")
		return &http.Response{StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"video/mp2t"}},
			Body:   io.NopCloser(strings.NewReader("abc"))}, nil
	})
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, httptest.NewRequest(http.MethodGet,
		"http://wails.localhost"+s.URL(upstreamHost+"/index.m3u8"), nil))
	child := "/__cczj/hls?u=" + base64.RawURLEncoding.EncodeToString([]byte(upstreamHost+"/segment.ts"))
	seg := httptest.NewRecorder()
	s.ServeHTTP(seg, httptest.NewRequest(http.MethodGet, "http://wails.localhost"+child, nil))
	if !strings.Contains(cookieAtSegment, "sess=abc") {
		t.Errorf("片段请求没有带上列表那一跳拿到的 cookie：%q", cookieAtSegment)
	}
	// 但上游的 Set-Cookie 绝不能被转回 WebView：那是把第三方 cookie 写进本机。
	if strings.Contains(rec.Header().Get("Set-Cookie"), "sess=abc") {
		t.Error("上游的 Set-Cookie 被转发给了 WebView")
	}
}

func TestHLSPlaylistNeverForwardsRangeAndNeverTruncates(t *testing.T) {
	s := NewHLSService()
	var ranges []string
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		ranges = append(ranges, req.Header.Get("Range"))
		if len(ranges) == 1 {
			// 第一次就回了 206：模拟上游无视请求、或列表地址不以 .m3u8 结尾。
			return &http.Response{StatusCode: http.StatusPartialContent,
				Header: http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"},
					"Content-Range": []string{"bytes 0-6/100"}},
				Body: io.NopCloser(strings.NewReader("#EXTM3U\n"))}, nil
		}
		return &http.Response{StatusCode: http.StatusOK,
			Header: http.Header{"Content-Type": []string{"application/vnd.apple.mpegurl"}},
			Body:   io.NopCloser(strings.NewReader("#EXTM3U\nfull.ts\n"))}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+s.URL(upstreamHost+"/playlist"), nil)
	req.Header.Set("Range", "bytes=0-6")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if len(ranges) != 2 || ranges[1] != "" {
		t.Fatalf("重取列表时没有去掉 Range：%v", ranges)
	}
	if rec.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200（重写后的列表一定是完整体）", rec.Code)
	}
	// 列表被重写成代理地址，所以这里解码头里的 base64 再比对：
	// 出现 /full.ts 才说明发出去的是第二次的完整列表，而不是第一次那 7 个字节。
	encoded := base64.RawURLEncoding.EncodeToString([]byte(upstreamHost + "/full.ts"))
	if !strings.Contains(rec.Body.String(), encoded) {
		t.Fatalf("发出的是被截断的列表：%s", rec.Body.String())
	}
	if got := rec.Header().Get("Content-Range"); got != "" {
		t.Errorf("响应带着 Content-Range=%q，但状态是 200", got)
	}
	if got := rec.Header().Get("Content-Length"); got != fmt.Sprint(len(rec.Body.String())) {
		t.Errorf("Content-Length=%q 与实际长度 %d 不符", got, len(rec.Body.String()))
	}
}

func TestHLSRangeStillForwardedForSegments(t *testing.T) {
	s := NewHLSService()
	var gotRange string
	s.client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		gotRange = req.Header.Get("Range")
		return &http.Response{StatusCode: http.StatusPartialContent,
			Header: http.Header{"Content-Type": []string{"video/mp2t"},
				"Content-Range": []string{"bytes 10-12/20"}},
			Body: io.NopCloser(strings.NewReader("abc"))}, nil
	})
	req := httptest.NewRequest(http.MethodGet, "http://wails.localhost"+s.URL(upstreamHost+"/segment.ts"), nil)
	req.Header.Set("Range", "bytes=10-12")
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	// 反过来也要成立：只有列表才吞掉 Range，字节区间是 BYTERANGE 片段的生命线。
	if gotRange != "bytes=10-12" {
		t.Errorf("片段的 Range 没转发： %q", gotRange)
	}
	if rec.Code != http.StatusPartialContent || rec.Header().Get("Content-Range") != "bytes 10-12/20" {
		t.Errorf("206 没有原样回传：status=%d headers=%v", rec.Code, rec.Header())
	}
}
