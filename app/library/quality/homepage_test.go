package quality

import (
	"strings"
	"testing"
)

func TestHomepageMatchingKeepsCandidateScopeAndRejectsExecutableLinks(t *testing.T) {
	x, err := Compile(testCorpus())
	if err != nil {
		t.Fatal(err)
	}
	html := `<a href="/book/0" title="作品0">cover</a><a href="/book/1"><b>作品1</b></a><a href="javascript:bad()">作品2</a><a href="#here">作品2</a><a href="/genre">历史</a>`
	e, err := MatchHomepage(strings.NewReader(html), "https://mirror.example/", x)
	if err != nil || len(e.Matches) != 2 {
		t.Fatal(e, err)
	}
	for _, m := range e.Matches {
		if m.Identity != "provisional" || m.Kind != "novel" {
			t.Fatal("homepage guess became a content verification", m)
		}
	}
	e, err = MatchHomepage(strings.NewReader(strings.Repeat(`<a href="/book/0">作品0</a>`, 4000)), "https://mirror.example/", x)
	if err != nil || e.Anchors != 2048 || len(e.Matches) != 1 {
		t.Fatal(e, err)
	}
}
