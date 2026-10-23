package service

import (
	"bytes"
	"encoding/base64"
	"testing"

	"cczjVideo/app/apperror"
	"github.com/andybalholm/brotli"
)

func TestDetailDecompressionRejectsOversizedOutput(t *testing.T) {
	var compressed bytes.Buffer
	w := brotli.NewWriter(&compressed)
	_, err := w.Write(bytes.Repeat([]byte("x"), (16<<20)+1))
	if err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = (&App{}).DecompressDetailJSONBrotli(base64.StdEncoding.EncodeToString(compressed.Bytes()))
	if !apperror.Is(err, apperror.Validation) {
		t.Fatalf("oversized cache must fail explicitly: %v", err)
	}
}
