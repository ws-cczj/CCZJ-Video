package download

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"reflect"
	"strings"
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

func TestParsePlaylistKeepsKeyAndByteRange(t *testing.T) {
	const base = "https://cdn.example.test/path/list.m3u8"
	content := strings.Join([]string{
		"#EXTM3U",
		"#EXT-X-MEDIA-SEQUENCE:7",
		`#EXT-X-KEY:METHOD=AES-128,URI="key.php?r=52",IV=0x00000000000000000000000000000002`,
		"#EXTINF:10.0,",
		"seg-1.ts",
		"#EXT-X-BYTERANGE:100@200",
		"#EXTINF:5.0,",
		"big.mp4",
		"#EXT-X-BYTERANGE:50",
		"#EXTINF:5.0,",
		"big.mp4",
	}, "\n")

	playlist := ParsePlaylist(base, content)
	if len(playlist.Unsupported) > 0 {
		t.Fatalf("unexpected unsupported tags: %v", playlist.Unsupported)
	}
	if len(playlist.Segments) != 3 {
		t.Fatalf("segments = %d, want 3", len(playlist.Segments))
	}

	first := playlist.Segments[0]
	if first.URL != base[:strings.LastIndex(base, "/")]+"/seg-1.ts" {
		t.Fatalf("first URL = %q", first.URL)
	}
	if first.Seq != 7 {
		t.Fatalf("first seq = %d, want 7 (EXT-X-MEDIA-SEQUENCE)", first.Seq)
	}
	if first.Key == nil || first.Key.Method != "AES-128" {
		t.Fatalf("first key = %#v, want AES-128", first.Key)
	}
	if want := "https://cdn.example.test/path/key.php?r=52"; first.Key.URI != want {
		t.Fatalf("key URI = %q, want %q", first.Key.URI, want)
	}
	if want := "00000000000000000000000000000002"; hex.EncodeToString(first.Key.IV) != want {
		t.Fatalf("key IV = %x, want %s", first.Key.IV, want)
	}

	// 第二段的区间是显式的，第三段省略起点，要接在第二段之后。
	second, third := playlist.Segments[1], playlist.Segments[2]
	if second.Range == nil || second.Range.Start != 200 || second.Range.Length != 100 {
		t.Fatalf("second range = %#v, want 100@200", second.Range)
	}
	if third.Range == nil || third.Range.Start != 300 || third.Range.Length != 50 {
		t.Fatalf("third range = %#v, want 50@300 (continuation)", third.Range)
	}
	if got := second.Range.Header(); got != "bytes=200-299" {
		t.Fatalf("range header = %q", got)
	}
	if third.Key == nil || third.Key.URI != first.Key.URI {
		t.Fatal("EXT-X-KEY must stay in effect for later segments")
	}
}

func TestParsePlaylistReportsUnsupportedKey(t *testing.T) {
	content := "#EXTM3U\n#EXT-X-KEY:METHOD=SAMPLE-AES,URI=\"k\"\n#EXTINF:4,\na.ts\n"
	playlist := ParsePlaylist("https://cdn.example.test/list.m3u8", content)
	if len(playlist.Unsupported) == 0 {
		t.Fatal("SAMPLE-AES must be reported: a plain concatenation of it is unplayable")
	}
	if !strings.Contains(playlist.Unsupported[0], "SAMPLE-AES") {
		t.Fatalf("unsupported = %v", playlist.Unsupported)
	}
}

func TestParsePlaylistByteRangeWithoutAnchorIsRejected(t *testing.T) {
	content := "#EXTM3U\n#EXT-X-BYTERANGE:100\n#EXTINF:4,\na.mp4\n"
	playlist := ParsePlaylist("https://cdn.example.test/list.m3u8", content)
	if playlist.Segments[0].Range != nil {
		t.Fatalf("range = %#v, want none", playlist.Segments[0].Range)
	}
	if len(playlist.Unsupported) == 0 {
		t.Fatal("a byte range with no start and no predecessor must be reported")
	}
}

func TestWriteSequenceInsertsInitSegmentOnce(t *testing.T) {
	content := "#EXTM3U\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\na.m4s\n#EXTINF:4,\nb.m4s\n"
	playlist := ParsePlaylist("https://cdn.example.test/path/list.m3u8", content)
	sequence, issues := WriteSequence(playlist.Segments)
	if len(issues) > 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(sequence) != 3 {
		t.Fatalf("sequence = %d entries, want 3 (init + 2 segments)", len(sequence))
	}
	if !sequence[0].Init || sequence[0].URL != "https://cdn.example.test/path/init.mp4" {
		t.Fatalf("first entry = %#v, want the init segment", sequence[0])
	}
	if sequence[1].Init || sequence[2].Init {
		t.Fatal("init segment must be inserted once, not per media segment")
	}
}

func TestWriteSequenceRejectsEncryptedMapWithoutIV(t *testing.T) {
	content := "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\"k\"\n#EXT-X-MAP:URI=\"init.mp4\"\n#EXTINF:4,\na.m4s\n"
	playlist := ParsePlaylist("https://cdn.example.test/list.m3u8", content)
	sequence, issues := WriteSequence(playlist.Segments)
	if len(issues) == 0 {
		t.Fatal("an encrypted map without an explicit IV has no defined IV, so it must be refused")
	}
	for _, seg := range sequence {
		if seg.Init {
			t.Fatal("no init segment may be queued when its IV is unknown")
		}
	}
}

func TestParsePlaylistKeepsKeysThroughAdFiltering(t *testing.T) {
	const base = "https://cdn.example.test/list.m3u8"
	content := strings.Join([]string{
		"#EXTM3U",
		`#EXT-X-KEY:METHOD=AES-128,URI="k"`,
		"#EXTINF:30,",
		"real-1.ts",
		"#EXTINF:30,",
		"real-2.ts",
		"#EXT-X-DISCONTINUITY",
		"#EXTINF:4,",
		"ad-1.ts",
		"#EXTINF:4,",
		"ad-2.ts",
		"#EXT-X-DISCONTINUITY",
		"#EXTINF:30,",
		"real-3.ts",
	}, "\n")
	playlist := ParsePlaylist(base, content)
	if len(playlist.Segments) != 3 {
		t.Fatalf("segments = %d, want the two content groups", len(playlist.Segments))
	}
	for _, seg := range playlist.Segments {
		if strings.Contains(seg.URL, "ad-") {
			t.Fatalf("ad group survived filtering: %s", seg.URL)
		}
		if seg.Key == nil {
			t.Fatalf("segment %s lost its key while filtering ads", seg.URL)
		}
	}
}

func TestSegmentIVDefaultsToSequenceNumber(t *testing.T) {
	iv := SegmentIV(&Key{Method: "AES-128"}, 258)
	want := "00000000000000000000000000000102"
	if got := hex.EncodeToString(iv); got != want {
		t.Fatalf("iv = %s, want %s", got, want)
	}
	explicit := strings.Repeat("ab", 16)
	ivBytes, _ := hex.DecodeString(explicit)
	if got := hex.EncodeToString(SegmentIV(&Key{IV: ivBytes}, 258)); got != explicit {
		t.Fatalf("explicit IV must win, got %s", got)
	}
}

func TestDecryptAES128RoundTrip(t *testing.T) {
	key := []byte("0123456789abcdef")
	iv := SegmentIV(nil, 3)
	plaintext := []byte("a ts segment that is not a whole number of blocks")

	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	padded := pkcs7Pad(plaintext)
	encrypted := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(encrypted, padded)

	got, err := DecryptAES128(key, iv, encrypted)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Fatalf("decrypted = %q, want %q", got, plaintext)
	}

	if _, err := DecryptAES128(key, iv, encrypted[:len(encrypted)-1]); err == nil {
		t.Fatal("a partial block must be rejected, not silently truncated")
	}
	if _, err := DecryptAES128([]byte("short"), iv, encrypted); err == nil {
		t.Fatal("a wrong-size key must be rejected")
	}
}

func pkcs7Pad(data []byte) []byte {
	pad := aes.BlockSize - len(data)%aes.BlockSize
	out := make([]byte, 0, len(data)+pad)
	out = append(out, data...)
	for i := 0; i < pad; i++ {
		out = append(out, byte(pad))
	}
	return out
}
