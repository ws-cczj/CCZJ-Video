package netpolicy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type responseCopy struct {
	status       int
	header       http.Header
	body         []byte
	proto        string
	major, minor int
	uncompressed bool
}

func (s *responseCopy) response(req *http.Request) *http.Response {
	return &http.Response{StatusCode: s.status, Status: strconv.Itoa(s.status) + " " + http.StatusText(s.status), Header: s.header.Clone(), Body: io.NopCloser(bytes.NewReader(s.body)), ContentLength: int64(len(s.body)), Proto: s.proto, ProtoMajor: s.major, ProtoMinor: s.minor, Uncompressed: s.uncompressed, Request: req}
}
func retryAfter(raw string, now time.Time) time.Duration {
	if seconds, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && seconds > 0 {
		return time.Duration(min(seconds, 86400)) * time.Second
	}
	if at, err := http.ParseTime(raw); err == nil && at.After(now) {
		return min(at.Sub(now), 24*time.Hour)
	}
	return 0
}

// Read-through capture never reads ahead. Streaming transfers disable capture;
// query capture stops at both a per-response and an application-wide byte budget.
type captureBody struct {
	io.ReadCloser
	mu                sync.Mutex
	data              []byte
	limit             int
	capture, overflow bool
	finished          bool
	complete          func([]byte, bool, error)
	reserve           func(int) bool
	release           func(int)
	closeOnce         sync.Once
	closeErr          error
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.mu.Lock()
	if !b.finished && b.capture && !b.overflow && n > 0 {
		if len(b.data)+n > b.limit || !b.reserve(n) {
			b.release(len(b.data))
			b.data = nil
			b.overflow = true
		} else {
			b.data = append(b.data, p[:n]...)
		}
	}
	if err != nil && !b.finished {
		b.finished = true
		data := b.data
		b.data = nil
		complete := b.capture && !b.overflow && errors.Is(err, io.EOF)
		b.mu.Unlock()
		var failure error
		if !errors.Is(err, io.EOF) {
			failure = err
		}
		b.complete(data, complete, failure)
		b.release(len(data))
	} else {
		b.mu.Unlock()
	}
	return n, err
}
func (b *captureBody) Close() error {
	b.closeOnce.Do(func() {
		b.closeErr = b.ReadCloser.Close()
		b.mu.Lock()
		if !b.finished {
			b.finished = true
			size := len(b.data)
			b.data = nil
			b.mu.Unlock()
			b.release(size)
			b.complete(nil, false, b.closeErr)
		} else {
			b.mu.Unlock()
		}
	})
	return b.closeErr
}
