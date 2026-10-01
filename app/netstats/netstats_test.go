package netstats

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func find(t *testing.T, cat Category) Stat {
	t.Helper()
	for _, st := range Snapshot() {
		if st.Category == string(cat) {
			return st
		}
	}
	t.Fatalf("快照里没有 %s", cat)
	return Stat{}
}

func TestRecordAggregatesRateAndSkipsIdleCategories(t *testing.T) {
	reset()
	t.Cleanup(reset)

	Record(CategoryDouban, 4000, 200, nil)
	Record(CategoryDouban, 8000, 300, nil)
	Record(CategoryPlayback, 1<<20, 250, nil)

	snap := Snapshot()
	if len(snap) != 2 {
		t.Fatalf("只有两个类别有流量，快照却返回 %d 行（没有出过网的类别不该占位）", len(snap))
	}
	// 顺序按 categories 声明走：播放流量排在豆瓣前面，界面才不会每次刷新都换行序。
	if snap[0].Category != string(CategoryPlayback) || snap[1].Category != string(CategoryDouban) {
		t.Fatalf("快照顺序 = %s, %s，期望 playback 在前", snap[0].Category, snap[1].Category)
	}

	d := find(t, CategoryDouban)
	if d.Requests != 2 || d.Bytes != 12000 || d.AvgMS != 250 {
		t.Fatalf("豆瓣累计 = %+v，期望 2 次 / 12000 字节 / 平均 250ms", d)
	}
	// 平均速率按「总字节 / 总耗时」算：(4000+8000)*1000/500 = 24000 B/s。
	if d.AvgBytesPerS != 24000 {
		t.Fatalf("豆瓣平均速率 = %d，期望 24000", d.AvgBytesPerS)
	}
	// 峰值是最快的那一次：8000B/300ms = 26666 B/s，比 4000B/200ms = 20000 更快。
	if d.PeakBytesPerS != 26666 {
		t.Fatalf("豆瓣峰值速率 = %d，期望 26666", d.PeakBytesPerS)
	}

	p := find(t, CategoryPlayback)
	if p.MaxMS != 250 || p.Bytes != 1<<20 {
		t.Fatalf("播放累计 = %+v", p)
	}
	if TotalBytes() != 12000+(1<<20) {
		t.Fatalf("TotalBytes = %d", TotalBytes())
	}
	if SinceUnix() == 0 {
		t.Fatal("记过账之后 SinceUnix 应该有值")
	}
}

func TestRecordKeepsPartialBytesOnFailure(t *testing.T) {
	reset()
	t.Cleanup(reset)

	Record(CategoryCollect, 1500, 900, errors.New("unexpected EOF"))

	c := find(t, CategoryCollect)
	if c.Fails != 1 || c.Bytes != 1500 {
		t.Fatalf("失败请求被当成零流量记账了：%+v", c)
	}
	if c.LastError != "unexpected EOF" {
		t.Fatalf("LastError = %q", c.LastError)
	}
}

func TestZeroDurationRecordDoesNotInventRate(t *testing.T) {
	reset()
	t.Cleanup(reset)

	Record(CategoryImage, 500, 0, nil)

	i := find(t, CategoryImage)
	// 本机时钟量不出耗时时会给出 ms=0：宁可速率是 0，也不能报出一个凭空的数。
	if i.AvgBytesPerS != 0 || i.PeakBytesPerS != 0 {
		t.Fatalf("ms=0 却算出了速率：%+v", i)
	}
	if i.Bytes != 500 || i.Requests != 1 {
		t.Fatalf("字节与次数仍然要记账：%+v", i)
	}
}

func TestWrapTransportRecordsReadBytes(t *testing.T) {
	reset()
	t.Cleanup(reset)

	base := &stubTransport{body: strings.Repeat("a", 300)}
	client := &http.Client{Transport: WrapTransport(CategoryImage, base)}
	download(t, client, "https://example.invalid/a")

	i := find(t, CategoryImage)
	if i.Requests != 1 || i.Fails != 0 || i.Bytes != 300 {
		t.Fatalf("一次完整读取应该记 1 次请求 / 300 字节：%+v", i)
	}
}

func TestWrapTransportRecordsPartialReadOnly(t *testing.T) {
	reset()
	t.Cleanup(reset)

	client := &http.Client{Transport: WrapTransport(CategoryPlayback, &stubTransport{body: strings.Repeat("b", 4096)})}
	resp, err := client.Get("https://example.invalid/seg.ts")
	if err != nil {
		t.Fatal(err)
	}
	// 分片转发只读了两个 1KB 就被上层放弃：流量按实际读到的算，不能拿 4096 冒充。
	buf := make([]byte, 1024)
	for range 2 {
		if _, err := resp.Body.Read(buf); err != nil {
			t.Fatal(err)
		}
	}
	resp.Body.Close()

	if p := find(t, CategoryPlayback); p.Bytes != 2048 || p.Requests != 1 {
		t.Fatalf("半途而废的连接记成了整段长度：%+v", p)
	}
}

func TestWrapTransportRecordsDialFailure(t *testing.T) {
	reset()
	t.Cleanup(reset)

	wantErr := errors.New("dial refused")
	client := &http.Client{Transport: WrapTransport(CategoryCollect, &stubTransport{err: wantErr})}
	if _, err := client.Get("https://example.invalid/list"); err == nil {
		t.Fatal("桩本就该让请求失败")
	}

	c := find(t, CategoryCollect)
	// 连不上没有字节，但次数与错误必须留痕：否则「全部挂在拨号上」在面板上和
	// 「这个功能没被用过」长得一模一样。
	if c.Requests != 1 || c.Fails != 1 || c.LastError != "dial refused" {
		t.Fatalf("失败请求没有被记账：%+v", c)
	}
}

// stubTransport 是可控的上游：要么给一段固定 body，要么直接失败。
type stubTransport struct {
	body string
	err  error
}

func (s *stubTransport) RoundTrip(*http.Request) (*http.Response, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

func download(t *testing.T, client *http.Client, url string) {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); err != nil {
		t.Fatal(err)
	}
}
