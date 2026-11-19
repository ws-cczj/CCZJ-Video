package danmaku

import (
	"strings"

	"golang.org/x/net/html"
)

func htmlAttr(node *html.Node, key string) string {
	for _, attr := range node.Attr {
		if attr.Key == key {
			return strings.TrimSpace(attr.Val)
		}
	}
	return ""
}

func htmlHasClass(node *html.Node, class string) bool {
	for _, value := range strings.Fields(htmlAttr(node, "class")) {
		if value == class {
			return true
		}
	}
	return false
}

func htmlText(node *html.Node) string {
	var result strings.Builder
	for child := range node.Descendants() {
		if child.Type == html.TextNode {
			result.WriteString(child.Data)
		}
	}
	return strings.TrimSpace(result.String())
}
