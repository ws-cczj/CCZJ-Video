package collect

import (
	"cczjVideo/app/model"
	"net/url"
	"testing"
)

func query(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query()
}
func TestStrategyOperationParameterIsolation(t *testing.T) {
	s := CreateStrategyFromSource(&model.Source{ApiUrl: "https://example.test/api?keep=%E4%B8%AD%E6%96%87&ids=stale&wd=stale&h=99", StrategyConfig: `{"version":2,"strategy":"standard_cms","list":{"action":"catalog","page_param":"p","limit_param":"n","type_param":"category","hours_param":"hours","extra":{"isend":"0"}},"search":{"action":"search","page_param":"p","keyword_param":"q","extra":{"year":"0"}},"detail":{"action":"detail","id_param":"id","extra":{"sort_direct":"asc"}}}`})
	list := query(t, s.BuildListUrl(1, FetchOptions{Limit: 0, Hours: 0, TypeID: "0"}))
	if list.Get("keep") != "中文" || list.Get("ac") != "catalog" || list.Get("p") != "1" || list.Get("category") != "0" || list.Get("isend") != "0" {
		t.Fatalf("list query=%v", list)
	}
	for _, k := range []string{"ids", "wd", "h", "q", "id", "hours"} {
		if list.Has(k) {
			t.Fatalf("list leaked %s: %v", k, list)
		}
	}
	search := query(t, s.BuildSearchUrl("a & b", 2))
	if search.Get("ac") != "search" || search.Get("p") != "2" || search.Get("q") != "a & b" || search.Get("year") != "0" {
		t.Fatalf("search=%v", search)
	}
	for _, k := range []string{"ids", "h", "category", "id"} {
		if search.Has(k) {
			t.Fatalf("search leaked %s", k)
		}
	}
	detail := query(t, s.BuildDetailUrl("0"))
	if detail.Get("ac") != "detail" || detail.Get("id") != "0" || detail.Get("sort_direct") != "asc" {
		t.Fatalf("detail=%v", detail)
	}
	for _, k := range []string{"p", "q", "wd", "h", "category"} {
		if detail.Has(k) {
			t.Fatalf("detail leaked %s", k)
		}
	}
}

func TestStrategyEmptySearchAndDetailDoNotBuildRequest(t *testing.T) {
	s := CreateStrategy("https://example.test/api")
	if s.BuildSearchUrl(" \t", 1) != "" {
		t.Fatal("empty search built URL")
	}
	if s.BuildDetailUrl(" ") != "" {
		t.Fatal("empty detail built URL")
	}
}

func TestCMSVideolistDefaultsAndCustomJSON(t *testing.T) {
	cms := CreateStrategyFromSource(&model.Source{ApiUrl: "https://example.test/api", StrategyConfig: `{"version":2,"strategy":"cms_videolist"}`})
	if got := query(t, cms.BuildDetailUrl("upstream")).Get("ac"); got != "videolist" {
		t.Fatalf("cms action=%q", got)
	}
	custom := CreateStrategyFromSource(&model.Source{ApiUrl: "https://example.test/api", StrategyConfig: `{"version":2,"strategy":"custom","list":{"action":"listing","page_param":"page"},"detail":{"action":"show","id_param":"video"}}`})
	if got := query(t, custom.BuildListUrl(3, FetchOptions{})); got.Get("ac") != "listing" || got.Get("page") != "3" {
		t.Fatalf("custom list=%v", got)
	}
	if got := query(t, custom.BuildDetailUrl("x")); got.Get("ac") != "show" || got.Get("video") != "x" {
		t.Fatalf("custom detail=%v", got)
	}
}
