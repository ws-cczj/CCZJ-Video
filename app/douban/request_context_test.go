package douban

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"
)

func TestCancelledSearchAndDetailSkipAdmission(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := SearchSubjectIDContext(ctx, "query", SearchMeta{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := ParseDetailContext(ctx, "123"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestUserCancellationDoesNotTriggerDoubanAntiCrawlBackoff(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	old := client
	t.Cleanup(func() { client = old })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})
	client = &http.Client{Transport: interactiveTransport(func(request *http.Request) (*http.Response, error) {
		close(started)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	done := make(chan error, 1)
	go func() { _, err := fetchDoubanContext(ctx, "https://movie.douban.com/subject/123"); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("douban I/O ignored cancellation")
	}
	if remainingBlock() > 0 {
		t.Fatal("user cancellation was classified as anti-crawl failure")
	}
}
