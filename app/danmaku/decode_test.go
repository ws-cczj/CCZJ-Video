package danmaku

import (
	"context"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
func TestProvidedSegmentEvidence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		decode func([]byte) ([]Item, int, error)
		min    int
	}{
		{"public-dm-segment-1.bin", DecodeBilibili, 1753}, {"qq-segment.json", DecodeQQ, 1}, {"qq-segment-2.json", DecodeQQ, 1}, {"iq-kb-segment.br", DecodeIQiyi, 1}, {"iq-kb-segment-2.br", DecodeIQiyi, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, skipped, err := tc.decode(fixture(t, tc.name))
			if err != nil {
				t.Fatal(err)
			}
			if len(items) < tc.min {
				t.Fatalf("lost sample rows: %d", len(items))
			}
			for _, item := range items {
				if !validItem(item) {
					t.Fatal(item)
				}
			}
			if strings.Contains(tc.name, "segment-2") && items[0].TimeMS < 30000 {
				t.Fatal("segment timestamps were shifted or lost")
			}
			t.Logf("items=%d skipped=%d first=%+v", len(items), skipped, items[0])
		})
	}
}
func TestMalformedSegmentsAreNotEmptySuccess(t *testing.T) {
	for _, raw := range [][]byte{{0x0a, 0xff}, {0}, {0xff, 0xff, 0xff}, []byte(`<html>challenge</html>`)} {
		if _, _, err := DecodeBilibili(raw); err == nil {
			t.Fatalf("invalid protobuf accepted: %x", raw)
		}
	}
	if _, _, err := DecodeQQ([]byte(`{}`)); err == nil {
		t.Fatal("schema change was hidden")
	}
	if _, _, err := DecodeQQ([]byte(`{"barrage_list":[{"time_offset":"bad","content":"x"}]}`)); err == nil {
		t.Fatal("invalid rows were hidden")
	}
	if items, _, err := DecodeQQ([]byte(`{"barrage_list":[]}`)); err != nil || len(items) != 0 {
		t.Fatal(items, err)
	}
	if _, _, err := DecodeIQiyi([]byte("not brotli")); err == nil {
		t.Fatal("bad compression was hidden")
	}
}
func TestQQPrecisionBlackAndAbsoluteTimestamp(t *testing.T) {
	items, _, err := DecodeQQ([]byte(`{"barrage_list":[{"id":"9007199254740993123","time_offset":"61000","content":"<b>text</b>","content_style":"{\"color\":\"000000\",\"position\":2}"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != "9007199254740993123" || items[0].Color != 0 || items[0].TimeMS != 61000 || items[0].Mode != 5 || items[0].Text != "<b>text</b>" {
		t.Fatal(items)
	}
}

type fakeTransport func(*http.Request) (*http.Response, error)

func (f fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func fixtureClient(t *testing.T, resolve func(*http.Request) string) *Client {
	return NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(fixture(t, resolve(r)))))}, nil
	})})
}
func TestProviderDiscoveryUsesSuppliedCatalogEvidence(t *testing.T) {
	qq := &qqProvider{client: fixtureClient(t, func(r *http.Request) string {
		if strings.Contains(r.URL.Path, "MbSearch") {
			return "qq-title-search.json"
		}
		return "qq-episodes.json"
	})}
	search, err := qq.Search(context.Background(), "兰香如故")
	if err != nil || len(search) == 0 {
		t.Fatal(search, err)
	}
	if search[0].Year != "2026" || search[0].Kind != "电视剧" {
		t.Fatalf("QQ handoff metadata lost: year=%q kind=%q", search[0].Year, search[0].Kind)
	}
	episodes, err := qq.Episodes(context.Background(), search[0].Ref)
	if err != nil || len(episodes) == 0 {
		t.Fatal(episodes, err)
	}
	if episodes[0].EpisodeID != "c4102g9a01t" {
		t.Fatal(episodes[0])
	}
	iq := &iqiyiProvider{client: fixtureClient(t, func(r *http.Request) string {
		if strings.Contains(r.URL.Path, "homePageV3") {
			return "iq-search.json"
		}
		return "iq-episodes.json"
	})}
	search, err = iq.Search(context.Background(), "狂飙")
	if err != nil || len(search) == 0 {
		t.Fatal(search, err)
	}
	if search[0].Year != "2023" || search[0].Kind != "电视剧" {
		t.Fatalf("handoff metadata lost: year=%q kind=%q", search[0].Year, search[0].Kind)
	}
	episodes, err = iq.Episodes(context.Background(), search[0].Ref)
	if err != nil || len(episodes) != 39 {
		t.Fatalf("IQ catalog: %d %v", len(episodes), err)
	}
	if episodes[0].EpisodeID != "3493131456125200" {
		t.Fatal(episodes[0])
	}
}
func TestProvidedQQIndexAndBiliPartitionMetadata(t *testing.T) {
	p := &qqProvider{client: fixtureClient(t, func(*http.Request) string { return "qq-base.json" })}
	index, err := p.Index(context.Background(), Episode{EpisodeID: "c4102g9a01t"})
	if err != nil || len(index.Segments) < 2 {
		t.Fatal(index, err)
	}
	if index.Segments[1].StartMS != 30000 {
		t.Fatal(index.Segments[:2])
	}
	b := &bilibiliProvider{client: fixtureClient(t, func(r *http.Request) string {
		if strings.Contains(r.URL.Path, "dm/web/view") {
			return "public-dm-view.bin"
		}
		return "public-video-info.bin"
	})}
	pages, err := b.Episodes(context.Background(), "BV17x411w7KC")
	if err != nil || len(pages) == 0 || pages[0].EpisodeID != "279786" {
		t.Fatal(pages, err)
	}
	index, err = b.Index(context.Background(), pages[0])
	if err != nil || len(index.Segments) != 1 || index.Segments[0].EndMS != 360000 {
		t.Fatal(index, err)
	}
}
func TestIQSegmentURL(t *testing.T) {
	if got := iqSegmentURL("3493131456125200", "1"); got != "https://cmts.iqiyi.com/bullet/52/00/3493131456125200_60_1_a978a045.br" {
		t.Fatal(got)
	}
}

func TestBiliMissingResultIsEmptyOnlyWithExplicitZeroCount(t *testing.T) {
	for _, valid := range []bool{true, false} {
		client := NewClient(&http.Client{Transport: fakeTransport(func(r *http.Request) (*http.Response, error) {
			body := `{"code":0,"data":{"page":1,"numResults":0}}`
			if !valid {
				body = `{"code":0,"data":{"page":1}}`
			}
			if r.URL.Query().Get("search_type") == "video" {
				body = `{"code":0,"data":{"result":[{"bvid":"BV17x411w7KC","title":"sample"}]}}`
			}
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
		})})
		provider := &bilibiliProvider{client: client, mixin: strings.Repeat("a", 32), expires: time.Now().Add(time.Hour)}
		values, err := provider.Search(context.Background(), "sample")
		if valid && (err != nil || len(values) != 1) {
			t.Fatal(values, err)
		}
		if !valid && err == nil {
			t.Fatal("missing schema was treated as empty")
		}
	}
}
