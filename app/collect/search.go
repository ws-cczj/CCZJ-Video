package collect

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/applog"
	"cczjVideo/app/model"
	"cczjVideo/app/operation"
	"context"
	"errors"
	"net/url"
	"strings"
)

// Some legacy collection APIs resolve the keyword with `vod_name LIKE '<wd>%'`, so a
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
	return FetchSearchPageContext(context.Background(), strategy, keyword, page)
}

func FetchSearchPageContext(ctx context.Context, strategy SourceStrategy, keyword string, page int) (*FetchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	remote, err := fetchSearchPage(ctx, strategy, keyword, page)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil {
		if !IsSearchUnsupported(err) {
			return nil, err
		}
		operation.Stage(ctx, "suggestion_fallback", applog.Fields{"reason": apperror.Diagnostic(err)})
		return fetchCMSSuggestions(ctx, strategy, keyword, page, err)
	}
	if len(remote.List) > 0 || !flatCMSStrategy(strategy) || !substringRewritable(keyword) {
		return remote, nil
	}
	// The retry is best-effort: a source that treats the keyword verbatim answers
	// the wildcard form with nothing, and the literal result stays authoritative.
	operation.Stage(ctx, "substring_retry", nil)
	wrapped, err := fetchSearchPage(ctx, strategy, likeWildcard+strings.TrimSpace(keyword)+likeWildcard, page)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err != nil || len(wrapped.List) == 0 {
		operation.Stage(ctx, "substring_retry_unused", applog.Fields{"error": err})
		return remote, nil
	}
	return wrapped, nil
}

// IsSearchUnsupported recognises the protocol refusals shared by ordinary and
// category searches. Transport failures remain retryable transport failures.
func IsSearchUnsupported(err error) bool {
	var apiErr *APIError
	return errors.Is(err, errSearchUnsupported) || (errors.As(err, &apiErr) && apiErr.Code == 1002)
}

func fetchSearchPage(ctx context.Context, strategy SourceStrategy, keyword string, page int) (*FetchResult, error) {
	// FetchWithStrategy decodes through the strategy's own envelope, so a
	// declarative source searches out of data.list while a CMS source keeps
	// reading the flat top-level list.
	return FetchWithStrategy(ctx, strategy, strategy.BuildSearchUrl(keyword, page))
}

// substringRewritable reports whether a keyword can be wrapped without letting
// user text steer the match pattern itself.
func substringRewritable(keyword string) bool {
	trimmed := strings.TrimSpace(keyword)
	return trimmed != "" && !strings.ContainsAny(trimmed, `%_\`)
}

func flatCMSStrategy(strategy SourceStrategy) bool {
	name := strategy.GetStrategyName()
	return (name == "standard_cms" || name == "cms_videolist") && shapeOfStrategy(strategy) == nil
}

// Some MAC CMS sites disable the collection API's wd parameter while their own
// search UI still uses ajax/suggest. Only an explicit refusal enables this path;
// transport failures, auth failures and arbitrary HTML must retain their errors.
func fetchCMSSuggestions(ctx context.Context, strategy SourceStrategy, keyword string, page int, refused error) (*FetchResult, error) {
	if !flatCMSStrategy(strategy) || page > 1 {
		return nil, refused
	}
	base, err := url.Parse(strategy.BuildSearchUrl(keyword, 1))
	if err != nil {
		return nil, refused
	}
	at := strings.Index(base.Path, "/api.php/")
	if at < 0 {
		return nil, refused
	}
	base.Path = base.Path[:at] + "/index.php/ajax/suggest"
	base.RawPath = ""
	base.RawQuery = url.Values{"mid": {"1"}, "wd": {strings.TrimSpace(keyword)}, "limit": {"20"}}.Encode()
	base.Fragment = ""
	result, err := doFetchContext(ctx, base.String(), nil)
	if err != nil {
		return nil, err
	}
	// Suggestion rows omit categories, so they cannot pass the user's category
	// bans. Resolve their original IDs in one bounded CMS detail batch before the
	// handler applies its normal category policy. Regular API search stays one read.
	ids := make([]string, 0, len(result.List))
	for _, video := range result.List {
		if video == nil || strings.TrimSpace(video.VodId.String()) == "" || strings.TrimSpace(video.VodName) == "" {
			return nil, apperror.New(apperror.Corrupt, "网页搜索返回了无效的作品身份")
		}
		if len(ids) == 20 {
			break
		}
		ids = append(ids, video.VodId.String())
	}
	if len(ids) > 0 {
		details, err := FetchWithStrategy(ctx, strategy, strategy.BuildDetailUrl(strings.Join(ids, ",")))
		if err != nil {
			return nil, err
		}
		byID := make(map[string]*model.Video, len(details.List))
		for _, video := range details.List {
			if video != nil {
				byID[video.VodId.String()] = video
			}
		}
		result.List = nil
		for _, id := range ids {
			if video := byID[id]; video != nil {
				if image, parseErr := url.Parse(video.VodPic); parseErr == nil && video.VodPic != "" {
					video.VodPic = base.ResolveReference(image).String()
				}
				result.List = append(result.List, video)
			} else {
				return nil, apperror.New(apperror.Corrupt, "网页搜索候选的批量详情返回不完整")
			}
		}
	}
	// Suggestions expose a bounded first page; several CMS variants report zero
	// totals despite returning hits. Do not advertise nonexistent later pages.
	result.Page = 1
	result.SearchLimited = true
	result.Pagecount = 1
	result.Total = FlexInt(len(result.List))
	operation.Stage(ctx, "suggestions_resolved", applog.Fields{"result_count": len(result.List), "candidate_count": len(ids), "limited": true})
	return result, nil
}
