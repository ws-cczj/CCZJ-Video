package quality

import (
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	"cczjVideo/app/provider"
	"golang.org/x/net/html"
)

type HomepageMatch struct {
	Kind     string `json:"kind"`
	WorkID   string `json:"workId"`
	Title    string `json:"title"`
	URL      string `json:"url"`
	Identity string `json:"identity"`
}
type HomepageEvidence struct {
	Anchors int             `json:"anchors"`
	Lookups int             `json:"lookups"`
	Matches []HomepageMatch `json:"matches"`
}

// MatchHomepage indexes bounded real anchor titles without executing JavaScript
// or guessing chapter selectors. A title hit is an adaptation candidate, never
// proof that an extension can read a work or has a complete/ad-free catalog.
func MatchHomepage(r io.Reader, base string, x *Index) (HomepageEvidence, error) {
	e := HomepageEvidence{Matches: []HomepageMatch{}}
	root, err := html.Parse(io.LimitReader(r, 256<<10))
	if err != nil {
		return e, err
	}
	u, err := url.Parse(base)
	if err != nil {
		return e, err
	}
	seen := map[string]bool{}
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if e.Anchors >= 2048 {
			return
		}
		if n.Type == html.ElementNode && n.Data == "a" {
			e.Anchors++
			href, title := "", ""
			for _, a := range n.Attr {
				if a.Key == "href" {
					href = a.Val
				}
				if a.Key == "title" {
					title = a.Val
				}
			}
			if href != "" && !strings.HasPrefix(href, "#") {
				link, parseErr := u.Parse(href)
				if parseErr == nil && (link.Scheme == "https" || link.Scheme == "http") && link.Hostname() != "" && link.User == nil {
					var b strings.Builder
					var text func(*html.Node, int)
					text = func(node *html.Node, depth int) {
						if depth > 32 || b.Len() > 600 {
							return
						}
						if node.Type == html.TextNode {
							b.WriteString(node.Data)
						}
						for child := node.FirstChild; child != nil; child = child.NextSibling {
							text(child, depth+1)
						}
					}
					text(n, 0)
					for _, name := range []string{title, strings.TrimSpace(b.String())} {
						if size := utf8.RuneCountInString(name); size < 2 || size > 120 {
							continue
						}
						for _, kind := range []string{"novel", "comic"} {
							e.Lookups++
							for _, hit := range x.Lookup(kind, &provider.Item{Title: name, URL: link.String()}) {
								key := kind + "\x00" + hit.Work.ID
								if seen[key] || len(e.Matches) >= 64 {
									continue
								}
								seen[key] = true
								e.Matches = append(e.Matches, HomepageMatch{kind, hit.Work.ID, name, link.String(), hit.Identity.Status})
							}
						}
					}
				}
			}
			return // descendants were consumed once; invalid nested anchors are not recursively expanded
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return e, nil
}
