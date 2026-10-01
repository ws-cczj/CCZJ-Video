// Package netstats 累计本进程真实发生的出网流量，供诊断面板读出「网络吞吐」而不是猜测。
//
// 只有一个机制：给各处已有的 http.Transport 套一层计数 RoundTripper，在响应体 Close 时
// 结账。它不查库、不发请求，读侧只是拿锁遍历七个计数器。
//
// 刻意不做统一的出网中间层：项目里 8 个 http.Client 各有各的超时、cookie jar 和地址校验
// 策略，为了计数把它们并成一个会改掉真实的网络行为。
package netstats

import (
	"io"
	"net/http"
	"sync"
	"time"
)

// 类别标签。诊断面板按这个顺序画，新增类别要同时补进 categories。
type Category string

const (
	CategoryPlayback  Category = "playback"  // 播放中继：m3u8 与 TS 分片
	CategoryLineProbe Category = "lineprobe" // 多线路测速取样
	CategoryCollect   Category = "collect"   // 采集源列表与详情分页
	CategoryDouban    Category = "douban"    // 豆瓣搜索/热榜/评论/验证
	CategoryImage     Category = "image"     // 海报代理
	CategoryUpdate    Category = "update"    // 更新检查与安装包下载
	CategoryDownload  Category = "download"  // 用户发起的离线下载
)

var categories = []Category{
	CategoryPlayback, CategoryLineProbe, CategoryCollect,
	CategoryDouban, CategoryImage, CategoryUpdate, CategoryDownload,
}

// Stat 是一类出网的累计结果。速率都由「字节/耗时」直接派生，没跑过的类别是 0 而不是猜测值。
type Stat struct {
	Category      string `json:"category"`
	Requests      int64  `json:"requests"`
	Fails         int64  `json:"fails"`
	Bytes         int64  `json:"bytes"`
	AvgMS         int64  `json:"avg_ms"`
	MaxMS         int64  `json:"max_ms"`
	AvgBytesPerS  int64  `json:"avg_bytes_per_sec"`
	PeakBytesPerS int64  `json:"peak_bytes_per_sec"`
	LastError     string `json:"last_error,omitempty"`
	LastAtUnix    int64  `json:"last_at_unix"`
}

type counters struct {
	requests int64
	fails    int64
	bytes    int64
	ms       int64
	maxMS    int64
	peakBps  int64
	lastErr  string
	lastAt   time.Time
}

var (
	mu    sync.Mutex
	book  = map[Category]*counters{}
	since time.Time
)

// Record 记一次出网。ms 是「发出请求到读完响应体」的总耗时，bytes 是实际读到的字节数，
// 所以命中本地缓存、根本没发请求的那些次不该走到这里——它们归各自的缓存计数器管。
func Record(cat Category, bytes, ms int64, err error) {
	mu.Lock()
	defer mu.Unlock()
	if since.IsZero() {
		since = time.Now()
	}
	c := book[cat]
	if c == nil {
		c = &counters{}
		book[cat] = c
	}
	c.requests++
	if ms > c.maxMS {
		c.maxMS = ms
	}
	c.ms += ms
	if err != nil {
		c.fails++
		c.lastErr = err.Error()
	}
	// 失败的请求常常一个字节都没读到，字节数仍然要记下来：半截断流也是真实流量。
	c.bytes += bytes
	if ms > 0 && bytes > 0 {
		if bps := bytes * 1000 / ms; bps > c.peakBps {
			c.peakBps = bps
		}
	}
	c.lastAt = time.Now()
}

// Snapshot 按固定顺序返回有流量的类别，一次都没有的类别不出现——空行只是噪音。
func Snapshot() []Stat {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Stat, 0, len(book))
	for _, cat := range categories {
		c := book[cat]
		if c == nil || c.requests == 0 {
			continue
		}
		st := Stat{
			Category:      string(cat),
			Requests:      c.requests,
			Fails:         c.fails,
			Bytes:         c.bytes,
			MaxMS:         c.maxMS,
			PeakBytesPerS: c.peakBps,
		}
		if c.requests > 0 {
			st.AvgMS = c.ms / c.requests
		}
		if c.ms > 0 {
			st.AvgBytesPerS = c.bytes * 1000 / c.ms
		}
		if !c.lastAt.IsZero() {
			st.LastAtUnix = c.lastAt.Unix()
		}
		st.LastError = c.lastErr
		out = append(out, st)
	}
	return out
}

// TotalBytes 是全部类别的出网字节合计，用于「这一趟总共下了多少」。
func TotalBytes() int64 {
	mu.Lock()
	defer mu.Unlock()
	var total int64
	for _, c := range book {
		total += c.bytes
	}
	return total
}

// SinceUnix 是第一条被记录的出网请求的时刻；还没有出过网时为 0。
func SinceUnix() int64 {
	mu.Lock()
	defer mu.Unlock()
	if since.IsZero() {
		return 0
	}
	return since.Unix()
}

// WrapTransport 给一类出网请求贴上类别标签：返回的 RoundTripper 把每次真实发生的
// 请求次数、读到的字节数与耗时记进本包的账本。它不改任何网络策略——拨号、超时、
// 重定向与地址校验仍然是原来那一份，所以加计数不会让任何一条请求变快或变慢。
func WrapTransport(cat Category, base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	return &counterTransport{cat: cat, base: base}
}

type counterTransport struct {
	cat  Category
	base http.RoundTripper
}

func (t *counterTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	started := time.Now()
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		// 连不上也是真实出网尝试：次数和失败数少了，面板就会把「全挂在拨号上」
		// 显示成一片空白，而那恰恰是最该看见的故障。
		Record(t.cat, 0, time.Since(started).Milliseconds(), err)
		return resp, err
	}
	if resp.Body != nil {
		resp.Body = &countedBody{ReadCloser: resp.Body, cat: t.cat, started: started}
	}
	return resp, nil
}

// countedBody 在 Close 时结账。字节只算调用方真的读走的部分，中途断掉就记已经流过来
// 的那一段——两者都是实测，不是按 Content-Length 推断。
type countedBody struct {
	io.ReadCloser
	cat     Category
	started time.Time
	n       int64
	err     error
	closed  bool
}

func (b *countedBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n += int64(n)
	if err != nil && err != io.EOF {
		b.err = err
	}
	return n, err
}

func (b *countedBody) Close() error {
	// 幂等：调用方普遍是 defer Close，而测速那类代码会在循环里提前 close 一次。
	if !b.closed {
		b.closed = true
		Record(b.cat, b.n, time.Since(b.started).Milliseconds(), b.err)
	}
	return b.ReadCloser.Close()
}

// reset 只给单测用：把进程内的账本清空，用例之间不互相污染。
func reset() {
	mu.Lock()
	defer mu.Unlock()
	book = map[Category]*counters{}
	since = time.Time{}
}
