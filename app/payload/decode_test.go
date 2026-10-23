package payload

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
)

func TestCompressedBudgetsAndCompleteJSON(t *testing.T) {
	for _, data := range []string{`{"value":1} {"value":2}`, `{"value":1} trailing`} {
		var result map[string]int
		if err := JSON(context.Background(), []byte(data), &result, false); !apperror.Is(err, apperror.Corrupt) {
			t.Fatal(err)
		}
	}
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	if _, err := w.Write([]byte(strings.Repeat(" ", 1025))); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Unpack(context.Background(), "without-suffix", bytes.NewReader(compressed.Bytes()), Limits{Input: 1024, Output: 1024}); !apperror.Is(err, apperror.Validation) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Unpack(ctx, "data.json", strings.NewReader(`{}`), Limits{Input: 10, Output: 10}); !apperror.Is(err, apperror.Cancelled) {
		t.Fatal(err)
	}
}
