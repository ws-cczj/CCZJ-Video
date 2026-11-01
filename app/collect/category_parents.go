package collect

import (
	"cczjVideo/app/apperror"
	"context"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var cmsTypeLink = regexp.MustCompile(`^vod-type-id-([0-9]+)\.html$`)

// CompleteCategoryParents supplements omitted CMS hierarchy from the provider's
// public crawler navigation. IDs and names must both match its API type list.
func CompleteCategoryParents(ctx context.Context, apiURL string, classes []Category) ([]Category, error) {
	u, err := url.Parse(apiURL)
	if err != nil || strings.ToLower(u.Hostname()) != "api.yyzy-tv.vip" {
		return classes, nil
	}
	known := map[string]int{}
	root := ""
	for i, row := range classes {
		known[row.ID.String()] = i
		if strings.Contains(row.Name, "短剧") || strings.Contains(row.Name, "短劇") {
			root = row.ID.String()
		}
	}
	if root == "" {
		return classes, nil
	}
	for _, row := range classes {
		if row.Parent.String() == root {
			return classes, nil
		}
	}
	if !cmsTypeLink.MatchString("vod-type-id-" + root + ".html") {
		return nil, apperror.New(apperror.Corrupt, "来源分类编号无效")
	}
	target := "https://get.1080zyk17.com/?m=vod-type-id-" + root + ".html"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header[k] = v
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, apperror.Wrap(apperror.Unavailable, err, "读取来源分类层级失败")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &HTTPError{StatusCode: resp.StatusCode, URL: target, RetryAfter: retryAfter(resp.Header.Get("Retry-After"))}
	}
	reader, err := decompress(resp.Body, resp.Header.Get("Content-Encoding"))
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	body, err := io.ReadAll(io.LimitReader(reader, (1<<20)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > 1<<20 {
		return nil, apperror.New(apperror.Corrupt, "来源分类页面超出范围")
	}
	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	result := append([]Category(nil), classes...)
	var text func(*html.Node) string
	text = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return n.Data
		}
		var out strings.Builder
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			out.WriteString(text(c))
		}
		return out.String()
	}
	var links func(*html.Node) []string
	links = func(n *html.Node) []string {
		var ids []string
		if n.Type == html.ElementNode && n.Data == "a" {
			for _, a := range n.Attr {
				if a.Key != "href" {
					continue
				}
				href, e := url.Parse(a.Val)
				if e != nil {
					continue
				}
				m := cmsTypeLink.FindStringSubmatch(href.Query().Get("m"))
				if len(m) != 2 {
					continue
				}
				if i, ok := known[m[1]]; ok && strings.TrimSpace(text(n)) == strings.TrimSpace(classes[i].Name) {
					ids = append(ids, m[1])
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			ids = append(ids, links(c)...)
		}
		return ids
	}
	count := 0
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "li" {
			ids := links(n)
			if len(ids) > 1 && ids[0] == root {
				for _, id := range ids[1:] {
					if id != root {
						result[known[id]].Parent = result[known[root]].ID
						count++
					}
				}
				return
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			visit(c)
		}
	}
	visit(doc)
	if count == 0 {
		return nil, apperror.New(apperror.Corrupt, "来源分类层级未能确认")
	}
	return result, nil
}
