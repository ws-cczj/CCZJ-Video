package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"cczjVideo/app/operation"
)

func TestChapterColdBurstSharesOneContentRead(t *testing.T) {
	s := setupLibraryTest(t)
	metadata := make(chan applog.Fields, 1)
	if err := s.fetcher.WrapTransport(func(base http.RoundTripper) http.RoundTripper { return readingIdentityTransport{base, metadata} }); err != nil {
		t.Fatal(err)
	}
	base := fakeSource()
	defer base.Close()
	started, release := make(chan struct{}), make(chan struct{})
	var reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/c/1.html" {
			if reads.Add(1) == 1 {
				close(started)
			}
			select {
			case <-release:
			case <-r.Context().Done():
				return
			}
		}
		base.Config.Handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	addTestSource(t, server, "shared_text", novelDocument())
	found, err := s.Search(context.Background(), model.SourceKindNovel, "示例", 1, nil)
	if err != nil || found.Total == 0 {
		t.Fatalf("search = %+v %v", found, err)
	}
	detail, err := s.Detail(context.Background(), found.Results[0].Items[0].ID, true)
	if err != nil || len(detail.Chapters) == 0 {
		t.Fatalf("detail = %+v %v", detail, err)
	}
	chapterID := 0
	for _, chapter := range detail.Chapters {
		if !chapter.IsVolume {
			chapterID = chapter.ID
			break
		}
	}
	if chapterID == 0 {
		t.Fatal("fixture has no content chapter")
	}
	results := make(chan error, 8)
	request, parent := operation.WithIdentity(context.Background(), "rpc.LibraryChapterText")
	for range 8 {
		go func() { _, err := s.ChapterText(request, chapterID); results <- err }()
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("content fixture path did not start")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		_, joined := s.textReads.Stats()
		if joined == 7 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	for range 8 {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	if reads.Load() != 1 {
		t.Fatalf("content upstream reads = %d", reads.Load())
	}
	fields := <-metadata
	if fields["pipeline"] != "reading.chapter_text" || fields["parent_request_id"] != parent.RequestID || fields["request_id"] == parent.RequestID || fields["mode"] != "novel" || fields["source_key"] != "shared_text" || fields["chapter_id"] != chapterID {
		t.Fatalf("content HTTP lost operation context: %+v", fields)
	}
	if err := s.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

type readingIdentityTransport struct {
	base     http.RoundTripper
	metadata chan<- applog.Fields
}

func (t readingIdentityTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Path == "/c/1.html" {
		t.metadata <- operation.Fields(request.Context(), nil)
	}
	return t.base.RoundTrip(request)
}
