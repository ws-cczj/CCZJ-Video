package douban

import (
	"context"
	"time"

	"cczjVideo/app/apperror"
)

// Interactive pages share the IP gate, cookies, bounded challenge retry and
// response validation. A comments visit must behave like a chart visit.
func fetchInteractiveDouban(url, referer, label string) (string, error) {
	return fetchInteractiveDoubanContext(context.Background(), url, referer, label)
}
func fetchInteractiveDoubanContext(ctx context.Context, url, referer, label string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if left := remainingBlock(); left > 0 {
		return "", apperror.Newf(apperror.Unavailable, "douban 反爬静默中，剩余 %s", left.Round(time.Minute))
	}
	if _, ok, err := awaitDoubanSlotContext(ctx, label, uiWaitBudget); err != nil {
		return "", err
	} else if !ok {
		return "", apperror.New(apperror.Unavailable, "douban 限速中，请稍后重试")
	}
	if left := remainingBlock(); left > 0 {
		return "", apperror.Newf(apperror.Unavailable, "douban 反爬静默中，剩余 %s", left.Round(time.Minute))
	}
	doc, challenge, err := fetchDoubanOnceContext(ctx, url, referer)
	if isSecChallengeURL(challenge) {
		if solveErr := solveDoubanChallengeContext(ctx, challenge, url); solveErr != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			noteAntiCrawl()
			return "", apperror.Wrap(apperror.Unavailable, solveErr, "豆瓣验证暂未通过")
		}
		doc, challenge, err = fetchDoubanOnceContext(ctx, url, challenge)
		if isSecChallengeURL(challenge) {
			noteAntiCrawl()
		}
	}
	if err != nil {
		return "", apperror.Wrap(apperror.Unavailable, err, "豆瓣页面暂不可用")
	}
	noteDoubanSuccess()
	return doc, nil
}
