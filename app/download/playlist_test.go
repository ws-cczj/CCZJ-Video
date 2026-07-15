package download

import (
	"reflect"
	"testing"
)

func TestIsHLSURL(t *testing.T) {
	for _, raw := range []string{"https://example.test/a.m3u8", "https://example.test/a.M3U8?token=1"} {
		if !IsHLSURL(raw) {
			t.Fatalf("expected HLS URL: %s", raw)
		}
	}
	if IsHLSURL("https://example.test/movie.mp4") {
		t.Fatal("unexpected HLS detection")
	}
}

func TestParseM3U8SegmentsResolvesRelativeURLs(t *testing.T) {
	got := ParseM3U8Segments("https://cdn.example.test/path/list.m3u8", "#EXTM3U\npart-1.ts\nhttps://media.example.test/part-2.ts\n")
	want := []string{"https://cdn.example.test/path/part-1.ts", "https://media.example.test/part-2.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("segments = %#v, want %#v", got, want)
	}
}
