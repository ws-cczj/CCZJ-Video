package netstats

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type modeTransportFunc func(*http.Request) (*http.Response, error)

func (f modeTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReadingModeIsCapturedBeforeBodyCompletion(t *testing.T) {
	reset()
	defer reset()
	base := WrapTransport(CategoryReading, modeTransportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{Body: io.NopCloser(strings.NewReader("123")), Header: make(http.Header)}, nil
	}))
	request, _ := http.NewRequestWithContext(context.Background(), "GET", "https://example.test/", nil)
	novel, _ := ForReadingMode("novel", base).RoundTrip(request)
	comic, _ := ForReadingMode("comic", base).RoundTrip(request)
	// Complete in reverse order: reading totals must not follow a mutable current mode.
	io.Copy(io.Discard, comic.Body)
	comic.Body.Close()
	io.Copy(io.Discard, novel.Body)
	novel.Body.Close()
	rows := Snapshot()
	if len(rows) != 2 || rows[0].Category != "novel" || rows[1].Category != "comic" || rows[0].Bytes != 3 || rows[1].Bytes != 3 {
		t.Fatal(rows)
	}
}
