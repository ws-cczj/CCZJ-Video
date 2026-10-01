package collect

import (
	"context"
	"strings"
)

// Collection APIs usually resolve the keyword with `vod_name LIKE '<wd>%'`, so a
// literal keyword only ever matches title prefixes: "大臣" misses "是，大臣" even
// though the source website finds it. Wrapping the keyword in LIKE wildcards
// turns that clause into a substring match; a server that already substring
// matches collapses `%%kw%%` back to `%kw%` and is unaffected.
const likeWildcard = "%"

// FetchSearchPage fetches one remote search page, retrying it as a substring
// match when the source's own keyword matching returns nothing. Sources that
// answer the literal keyword, and keywords carrying LIKE metacharacters, cost a
// single request.
func FetchSearchPage(strategy SourceStrategy, keyword string, page int) (*FetchResult, error) {
	remote, err := fetchSearchPage(strategy, keyword, page)
	if err != nil {
		return nil, err
	}
	if len(remote.List) > 0 || !substringRewritable(keyword) {
		return remote, nil
	}
	// The retry is best-effort: a source that treats the keyword verbatim answers
	// the wildcard form with nothing, and the literal result stays authoritative.
	wrapped, err := fetchSearchPage(strategy, likeWildcard+strings.TrimSpace(keyword)+likeWildcard, page)
	if err != nil || len(wrapped.List) == 0 {
		return remote, nil
	}
	return wrapped, nil
}

func fetchSearchPage(strategy SourceStrategy, keyword string, page int) (*FetchResult, error) {
	// FetchWithStrategy decodes through the strategy's own envelope, so a
	// declarative source searches out of data.list while a CMS source keeps
	// reading the flat top-level list.
	return FetchWithStrategy(context.Background(), strategy, strategy.BuildSearchUrl(keyword, page))
}

// substringRewritable reports whether a keyword can be wrapped without letting
// user text steer the match pattern itself.
func substringRewritable(keyword string) bool {
	trimmed := strings.TrimSpace(keyword)
	return trimmed != "" && !strings.ContainsAny(trimmed, `%_\`)
}
