package provider

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"cczjVideo/app/apperror"
)

func TestBrowseUsesRemoteCategoryWithoutKeywordAndKeepsUpstreamOffset(t *testing.T) {
	fetcher := NewFetcher()
	t.Cleanup(fetcher.Close)
	fetcher.client.Transport = fanqieTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("category_id") != "273" || r.URL.Query().Get("offset") != "20" || r.URL.Query().Get("q") != "" {
			t.Fatalf("wrong browse request: %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"code":0,"data":{"has_more":true,"data":[{"book_id":"9007199254740993123","book_name":"历史","genre":"0","creation_status":"0","word_number":"120000"},{"book_id":"2","book_name":"漫画","genre":"205"},{"book_id":"3","book_name":"音频","genre":"4"}]}}`))}, nil
	})
	doc, err := ParseDocument(`{"version":3,"kind":"novel","adapter":"fanqie-web"}`, "novel")
	if err != nil {
		t.Fatal(err)
	}
	out, err := NewPipeline(doc, fetcher, DefaultLimits()).Browse(context.Background(), "history", 20)
	if err != nil || len(out.Items) != 1 || out.Items[0].RemoteID != "9007199254740993123" || out.NextOffset != 23 || !out.HasNext || out.Items[0].Status != "完结" || out.Items[0].Words != "12.0万字" {
		t.Fatalf("browse result: %+v, %v", out, err)
	}
}

func TestBrowseRejectsUnexpectedShapesAndPreservesPublicationPagination(t *testing.T) {
	for _, body := range []string{`{}`, `{"code":0,"data":{}}`, `{"code":0,"data":{"data":null}}`, `{"code":1,"data":{"data":[]}}`} {
		t.Run(body, func(t *testing.T) {
			if _, err := decodeFanqieBrowse(body, "history", 0); err == nil {
				t.Fatal("invalid browse payload accepted")
			}
		})
	}
	out, err := decodeFanqieBrowse(`{"code":0,"data":{"publication_list":[{"book_id":"123","book_name":"出版作品","book_type":"文学"}],"total_count":25}}`, "publication", 0)
	if err != nil || len(out.Items) != 1 || out.Items[0].Genres != "文学" || !out.HasNext || out.NextOffset != 20 {
		t.Fatal(out, err)
	}
	_, err = (&Pipeline{Doc: &Document{Kind: "novel", Adapter: "fanqie-web"}}).Browse(context.Background(), "https://evil.example", 0)
	if !apperror.Is(err, apperror.Validation) {
		t.Fatal("caller URL accepted", err)
	}
}

func TestFanqieCreationStatusMatchesPublicPage(t *testing.T) {
	for value, want := range map[string]string{"0": "完结", "1": "连载", "": "", "unknown": ""} {
		if got := fanqieCreationStatus(value); got != want {
			t.Fatalf("status %q: %q, want %q", value, got, want)
		}
	}
}

func TestBrowseDecodesActualNumericAndBooleanContinuation(t *testing.T) {
	for _, flag := range []string{"1", "0", "true", "false"} {
		page, err := decodeFanqieBrowse(`{"code":0,"data":{"has_more":`+flag+`,"data":[{"book_id":"123","book_name":"书","genre":"0"}]}}`, "history", 0)
		if err != nil || page.HasNext != (flag == "1" || flag == "true") {
			t.Fatal(flag, page, err)
		}
	}
	if _, err := decodeFanqieBrowse(`{"code":0,"data":{"has_more":2,"data":[]}}`, "history", 0); err == nil {
		t.Fatal("invalid flag accepted")
	}
}
