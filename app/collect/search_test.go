package collect

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

type keywordRoundTripper struct {
	requested []string
	respond   func(wd string) string
}

func (k *keywordRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	wd := req.URL.Query().Get("wd")
	k.requested = append(k.requested, wd)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(k.respond(wd))),
	}, nil
}

func withKeywordSource(t *testing.T, respond func(wd string) string) *keywordRoundTripper {
	t.Helper()
	stub := &keywordRoundTripper{respond: respond}
	original := client
	client = &http.Client{Transport: stub}
	t.Cleanup(func() { client = original })
	return stub
}

func cmsBody(names ...string) string {
	var items []string
	for i, name := range names {
		items = append(items, fmt.Sprintf(`{"vod_id":"%d","vod_name":%q}`, i+1, name))
	}
	return fmt.Sprintf(`{"code":1,"total":%d,"list":[%s]}`, len(names), strings.Join(items, ","))
}

// A prefix-matching source is the reported bug: the website finds "是，大臣" for
// "大臣" while the collection endpoint answers the bare keyword with nothing.
func TestSearchEscalatesToSubstringWhenSourcePrefixMatches(t *testing.T) {
	stub := withKeywordSource(t, func(wd string) string {
		if strings.Contains(wd, "%") {
			return cmsBody("是，大臣第一季")
		}
		return cmsBody()
	})
	got, err := FetchSearchPage(CreateStrategy("https://example.test/api"), "大臣", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != 1 || got.List[0].VodName != "是，大臣第一季" {
		t.Fatalf("list=%v", got.List)
	}
	if len(stub.requested) != 2 || stub.requested[0] != "大臣" || stub.requested[1] != "%大臣%" {
		t.Fatalf("requested=%v", stub.requested)
	}
}

func TestSearchKeepsLiteralKeywordWhenSourceAnswersIt(t *testing.T) {
	stub := withKeywordSource(t, func(string) string { return cmsBody("大臣") })
	got, err := FetchSearchPage(CreateStrategy("https://example.test/api"), "大臣", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != 1 || got.List[0].VodName != "大臣" {
		t.Fatalf("list=%v", got.List)
	}
	if len(stub.requested) != 1 {
		t.Fatalf("expected one request, got %v", stub.requested)
	}
}

// Sources that treat the keyword verbatim answer the wildcard form with nothing,
// so the literal (empty) page must stay authoritative rather than error out.
func TestSearchFallsBackToLiteralPageWhenWildcardIsEmpty(t *testing.T) {
	stub := withKeywordSource(t, func(wd string) string {
		if strings.Contains(wd, "%") {
			return cmsBody()
		}
		return cmsBody("大臣")
	})
	got, err := FetchSearchPage(CreateStrategy("https://example.test/api"), "大臣", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.List) != 1 || got.List[0].VodName != "大臣" {
		t.Fatalf("list=%v", got.List)
	}
	if len(stub.requested) != 1 {
		t.Fatalf("requested=%v", stub.requested)
	}
}

// User text must stay the searched fragment: a typed LIKE wildcard would
// otherwise redefine the match pattern.
func TestSearchNeverRewritesKeywordCarryingLikeMetacharacters(t *testing.T) {
	for _, kw := range []string{"大臣%", "%大臣", "大臣_集", `大臣\集`} {
		stub := withKeywordSource(t, func(string) string { return cmsBody() })
		if _, err := FetchSearchPage(CreateStrategy("https://example.test/api"), kw, 1); err != nil {
			t.Fatal(err)
		}
		if len(stub.requested) > 1 {
			t.Fatalf("keyword %q escalated to %v", kw, stub.requested)
		}
	}
}
