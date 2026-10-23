package provider

import "testing"

func TestContinuationURLPreservesSignedQuery(t *testing.T) {
	op := &Operation{Params: map[string]string{"fixed": "value"}}
	const next = "https://example.com/search?pg=opaque&wd=server&sig=a%2Bb&cursor=two"
	got, err := BuildURL("https://example.com", op, Target{URL: next, PreserveQuery: true, Vars: map[string]string{VarKeyword: "client", VarPage: "2"}})
	if err != nil || got != next {
		t.Fatalf("continuation URL changed: %q, %v", got, err)
	}
	if _, err := BuildURL("https://example.com", op, Target{URL: "file:///secret", PreserveQuery: true}); err == nil {
		t.Fatal("continuation bypassed target validation")
	}
}
