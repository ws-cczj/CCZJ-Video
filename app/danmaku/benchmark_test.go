package danmaku

import (
	"os"
	"testing"
)

func BenchmarkProvidedSegmentDecode(b *testing.B) {
	for _, spec := range []struct {
		name   string
		decode func([]byte) ([]Item, int, error)
	}{{"public-dm-segment-1.bin", DecodeBilibili}, {"qq-segment.json", DecodeQQ}, {"iq-kb-segment.br", DecodeIQiyi}} {
		b.Run(spec.name, func(b *testing.B) {
			raw, err := os.ReadFile("testdata/" + spec.name)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.SetBytes(int64(len(raw)))
			b.ResetTimer()
			for b.Loop() {
				if _, _, err := spec.decode(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
