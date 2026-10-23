package provider

import (
	"cczjVideo/app/apperror"
	"context"
	"github.com/andybalholm/cascadia"
	"golang.org/x/net/html"
	"net/url"
	"strings"
)

// ComicTrending reads publisher popularity, not source availability. These
// references must be searched in reading sources, never treated as readable IDs.
func ComicTrending(ctx context.Context, fetcher *Fetcher, board string) ([]*Item, error) {
	path := ""
	switch board {
	case "japan":
		path = "/ranking/0"
	case "china":
		path = "/ranking/1"
	default:
		return nil, apperror.New(apperror.Validation, "漫画榜单无效")
	}
	origin := "https://manga.bilibili.com"
	doc := &Document{Kind: "comic", Encoding: "utf-8", Headers: map[string]string{"Referer": origin + "/"}}
	response, err := fetcher.Fetch(ctx, doc, &Operation{Path: path, Format: "html"}, origin, Target{})
	if err != nil {
		return nil, err
	}
	return extractComicTrending(response.Text, origin)
}

func extractComicTrending(raw, origin string) ([]*Item, error) {
	doc, err := html.Parse(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	items := make([]*Item, 0, 24)
	seen := map[string]bool{}
	first := func(node *html.Node, selector string) *html.Node {
		return cascadia.Query(node, cascadia.MustCompile(selector))
	}
	attr := func(node *html.Node, key string) string {
		if node != nil {
			for _, a := range node.Attr {
				if a.Key == key {
					return a.Val
				}
			}
		}
		return ""
	}
	text := func(node *html.Node) string {
		if node == nil {
			return ""
		}
		return strings.TrimSpace(textOf(node))
	}
	// Generated class suffixes may change. Restrict extraction to ranked cards,
	// excluding navigation, chapter links and unrelated recommendation widgets.
	for _, node := range cascadia.QueryAll(doc, cascadia.MustCompile(`div[class*="_rank-item-content_"]`)) {
		if len(items) >= 24 {
			break
		}
		link := first(node, `a[class*="_manga-title_"]`)
		title := text(link)
		href := attr(link, "href")
		address, err := url.Parse(href)
		if err != nil || !strings.HasPrefix(address.Path, "/detail/mc") || title == "" || seen[title] {
			continue
		}
		if address.IsAbs() && address.Scheme+"://"+address.Host != origin {
			continue
		}
		seen[title] = true
		cover := attr(first(node, "picture img"), "src")
		if image, err := url.Parse(cover); err != nil || image.Scheme != "https" || !strings.HasSuffix(image.Hostname(), ".hdslb.com") {
			cover = ""
		}
		items = append(items, &Item{Title: title, URL: origin + address.Path, Author: text(first(node, `[class*="_author-text_"]`)), Cover: cover, Genres: text(first(node, `[class*="_style-text_"]`))})
	}
	if len(items) < 3 {
		return nil, apperror.New(apperror.Corrupt, "漫画榜单结构已变化，暂时无法更新")
	}
	return items, nil
}
