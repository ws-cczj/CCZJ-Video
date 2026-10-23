package douban

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type interactiveTransport func(*http.Request) (*http.Response, error)

func (f interactiveTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestCommentsAndChartShareChallengeRetry(t *testing.T) {
	for _, kind := range []string{"comments", "chart"} {
		t.Run(kind, func(t *testing.T) {
			resetBlock(t)
			clearRateClock()
			noteDoubanSuccess()
			old := client
			t.Cleanup(func() { client = old })
			gets, submissions := 0, 0
			client = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
				status, body, loc := 200, "", ""
				if r.URL.Host == "movie.douban.com" {
					gets++
					if gets == 1 {
						status = 302
						loc = "https://sec.douban.com/c?r=comments"
					} else {
						body = `<div id="comments"><div class="comment-item" data-cid="123"><div><span class="short">cached comment</span></div></div></div>`
					}
				} else if r.URL.Host == "sec.douban.com" && r.Method == http.MethodGet {
					body = liveChallengePage
				} else if r.URL.Host == "sec.douban.com" && r.Method == http.MethodPost {
					submissions++
					status = 302
					loc = "https://movie.douban.com/subject/123/comments"
				} else {
					t.Fatalf("unexpected request: %s", r.URL)
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{loc}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
			})}
			if kind == "comments" {
				resp, err := fetchCommentsFromWeb(context.Background(), "123", 1, "new_score")
				if err != nil || len(resp.Comments) != 1 {
					t.Fatalf("comment retry failed: %+v %v", resp, err)
				}
			} else if _, err := fetchChartHTML("https://movie.douban.com/chart"); err != nil {
				t.Fatal(err)
			}
			if gets != 2 || submissions != 1 {
				t.Fatalf("GET=%d POST=%d; want one challenge retry", gets, submissions)
			}
			if remainingBlock() > 0 {
				t.Fatal("successful challenge incorrectly triggered blackout")
			}
		})
	}
}

func TestCommentsDoNotCacheLoginPageAsEmpty(t *testing.T) {
	resetBlock(t)
	clearRateClock()
	noteDoubanSuccess()
	old := client
	t.Cleanup(func() { client = old })
	client = &http.Client{Transport: interactiveTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`<html><form>Log in</form></html>`)), Header: http.Header{}, Request: r}, nil
	})}
	if _, err := fetchCommentsFromWeb(context.Background(), "123", 1, "time"); err == nil {
		t.Fatal("login HTML was treated as an empty successful page")
	}
}

func TestFailedColdCommentsBackOff(t *testing.T) {
	isolateComments(t)
	seedComments("unused", commentPage("unused"), 0)
	calls := 0
	fetchCommentsPage = func(context.Context, string, int, string) (*DoubanCommentsResp, error) {
		calls++
		return nil, io.ErrUnexpectedEOF
	}
	for range 2 {
		if _, err := FetchComments("2400097", 1, "time"); err == nil {
			t.Fatal("failure became successful cache")
		}
	}
	if calls != 1 {
		t.Fatalf("failed page fetched %d times during backoff", calls)
	}
	if commentRefreshBackoff < 30*time.Minute {
		t.Fatal("backoff shorter than half hour")
	}
}
