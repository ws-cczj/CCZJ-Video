package provider

import (
	"net/http"
	"net/url"
	"testing"
)

func TestSourceScopesIsolateCookiesAndConfiguration(t *testing.T) {
	root := NewFetcher()
	defer root.Close()
	u, _ := url.Parse("https://same.example.test/chapter")
	a := root.Scope("source-a:config-one")
	b := root.Scope("source-b:config-one")
	a.client.Jar.SetCookies(u, []*http.Cookie{{Name: "auth", Value: "a", Path: "/"}})
	if got := b.client.Jar.Cookies(u); len(got) != 0 {
		t.Fatalf("source cookie leak: %v", got)
	}
	if got := root.Scope("source-a:config-two").client.Jar.Cookies(u); len(got) != 0 {
		t.Fatalf("configuration cookie leak: %v", got)
	}
	if got := root.Scope("source-a:config-one").client.Jar.Cookies(u); len(got) != 1 || got[0].Value != "a" {
		t.Fatalf("same source session lost: %v", got)
	}
	for i := 0; i < 200; i++ {
		root.Scope(string(rune(i)) + ":config")
	}
	if len(root.scopes) > 128 || len(root.order) > 128 {
		t.Fatal("session retention unbounded")
	}
}
