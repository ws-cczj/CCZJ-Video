package douban

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"cczjVideo/app/apperror"
)

func TestDetailFallbackRespectsActiveBlackout(t *testing.T) {
	for _, path := range []string{"detail", "abstract", "degraded-detail"} {
		t.Run(path, func(t *testing.T) {
			resetBlock(t)
			clearRateClock()
			noteDoubanSuccess()
			noteAntiCrawl()
			oldProbe := detailChallengedUntil.Load()
			t.Cleanup(func() { detailChallengedUntil.Store(oldProbe) })
			clearDetailChallenged()
			if path == "degraded-detail" {
				markDetailChallenged()
			}
			old := client
			t.Cleanup(func() { client = old })
			requests := 0
			client = &http.Client{Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
				requests++
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"subject":{"rate":"8.8"}}`)), Request: r}, nil
			})}
			var err error
			if path == "abstract" {
				_, err = fetchDetailByAbstractContext(context.Background(), "123")
			} else {
				_, err = ParseDetailContext(context.Background(), "123")
			}
			if requests != 0 || err == nil {
				t.Fatalf("blackout bypassed: requests=%d err=%v", requests, err)
			}
			if remainingBlock() == 0 {
				t.Fatal("fallback incorrectly cleared the active blackout")
			}
		})
	}
}

func TestAntiCrawlEscalatesAfterExpiredBlackout(t *testing.T) {
	resetBlock(t)
	blockMu.Lock()
	blockUntil, blockStrikes = time.Now().Add(-time.Second), 1
	blockMu.Unlock()
	if remainingBlock() != 0 {
		t.Fatal("expired blackout still blocks probe")
	}
	noteAntiCrawl()
	left, strikes := AntiCrawlState()
	if strikes != 2 || left < 14*time.Minute || left > 15*time.Minute {
		t.Fatalf("failed probe reset backoff: strikes=%d remaining=%s", strikes, left)
	}
}

func TestBatchSkipsDuringBlackout(t *testing.T) {
	resetBlock(t)
	noteDoubanSuccess()
	noteAntiCrawl()
	u := NewUpdater()
	// 没有数据库也应在查询待办前返回，不逐条尝试被静默的任务。
	if count, err := u.UpdateBatch(); count != 0 || err != nil || u.IsRunning() {
		t.Fatalf("blackout batch: count=%d err=%v running=%v", count, err, u.IsRunning())
	}
}

func TestAbstractStillWorksOutsideBlackout(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	old := client
	t.Cleanup(func() { client = old })
	requests := 0
	client = &http.Client{Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
		requests++
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"subject":{"rate":"8.8"}}`)), Request: r}, nil
	})}
	info, err := fetchDetailByAbstractContext(context.Background(), "123")
	if err != nil || requests != 1 || info.Rating != "8.8" {
		t.Fatalf("abstract unavailable outside blackout: info=%+v requests=%d err=%v", info, requests, err)
	}
}

func TestDirectRejectionStopsOtherDoubanEndpoints(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		for _, path := range []string{"crawler", "interactive"} {
			t.Run(http.StatusText(status)+"/"+path, func(t *testing.T) {
				resetBlock(t)
				clearRateClock()
				noteDoubanSuccess()
				old := client
				t.Cleanup(func() { client = old })
				calls := 0
				client = &http.Client{Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
					calls++
					return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("blocked")), Request: r}, nil
				})}
				var err error
				if path == "crawler" {
					_, err = fetchHTML("https://movie.douban.com/subject/123/")
				} else {
					_, err = fetchChartHTML(chartURL)
				}
				if err == nil || remainingBlock() == 0 {
					t.Fatalf("HTTP %d did not trigger shared blackout: %v", status, err)
				}
				clearRateClock()
				_, _ = fetchDetailByAbstractContext(context.Background(), "123")
				_, _ = fetchCommentsFromWeb(context.Background(), "123", 1, "time")
				_, _ = SearchSubjectIDContext(context.Background(), "测试第一季", SearchMeta{})
				if calls != 1 {
					t.Fatalf("other endpoints bypassed blackout: calls=%d", calls)
				}
			})
		}
	}
}

func TestMiddlewareCooldownDoesNotCountAsRemoteAntiCrawl(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	old := client
	t.Cleanup(func() { client = old })
	oldProbe := detailChallengedUntil.Load()
	t.Cleanup(func() { detailChallengedUntil.Store(oldProbe) })
	clearDetailChallenged()
	calls := 0
	client = &http.Client{Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		clearRateClock()
		return nil, &apperror.Error{Code: apperror.RateLimited, Message: "请求正在冷却", RetryAfterSeconds: 120}
	})}
	_, err := ParseDetailContext(context.Background(), "123")
	_, strikes := AntiCrawlState()
	if !apperror.Is(err, apperror.RateLimited) || apperror.Public(err).RetryAfterSeconds != 120 || calls != 1 || strikes != 0 {
		t.Fatalf("local cooldown treated as remote failure: calls=%d strikes=%d err=%v", calls, strikes, err)
	}
}
