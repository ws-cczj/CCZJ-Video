package douban

import (
	"cczjVideo/app/apperror"
	"context"
	"net/http"
	"testing"
	"time"
)

func TestCommentsCancellationReachesHTTPWithoutFailureBackoff(t *testing.T) {
	isolateComments(t)
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	old := client
	t.Cleanup(func() { client = old })
	started, stopped := make(chan struct{}), make(chan struct{})
	client = &http.Client{Transport: interactiveTransport(func(req *http.Request) (*http.Response, error) {
		close(started)
		<-req.Context().Done()
		close(stopped)
		return nil, req.Context().Err()
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := FetchCommentsContext(ctx, "99240001", 1, "time"); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("HTTP not started")
	}
	cancel()
	select {
	case err := <-done:
		if !apperror.Is(err, apperror.Cancelled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("read did not cancel")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("HTTP did not cancel")
	}
	commentRuntime.RLock()
	reads := commentRuntime.reads
	commentRuntime.RUnlock()
	deadline := time.Now().Add(time.Second)
	for {
		active, _ := reads.Stats()
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("cancelled worker did not settle")
		}
		time.Sleep(time.Millisecond)
	}
	commentRefresh.Lock()
	until := commentRefresh.blocked["99240001_1_time"]
	commentRefresh.Unlock()
	if !until.IsZero() {
		t.Fatal("cancel triggered a 30-minute failure cooldown")
	}
}

func TestRateWaitCancellationReturnsPromptly(t *testing.T) {
	clearRateClock()
	rateMu.Lock()
	lastRequestTime = time.Now().Add(time.Minute)
	rateMu.Unlock()
	t.Cleanup(clearRateClock)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel)
	started := time.Now()
	_, _, err := awaitDoubanSlotContext(ctx, "test", 2*time.Minute)
	if !apperror.Is(err, apperror.Cancelled) || time.Since(started) > time.Second {
		t.Fatal(err, time.Since(started))
	}
}
