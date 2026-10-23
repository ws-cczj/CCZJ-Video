package douban

import (
	"cczjVideo/app/apperror"
	"cczjVideo/app/workflight"
	"context"
	"sync"
	"time"
)

var commentRuntime = struct {
	sync.RWMutex
	reads  *workflight.Group[*DoubanCommentsResp]
	launch func(string, func(context.Context)) bool
}{reads: workflight.New[*DoubanCommentsResp](context.Background(), 64), launch: func(_ string, work func(context.Context)) bool { go work(context.Background()); return true }}

// ConfigureCommentRuntime connects stale refreshes and shared reads to the host.
// The returned closer must finish before SQLite is closed.
func ConfigureCommentRuntime(owner context.Context, launch func(string, func(context.Context)) bool) func(context.Context) error {
	reads := workflight.New[*DoubanCommentsResp](owner, 64)
	commentRuntime.Lock()
	commentRuntime.reads = reads
	commentRuntime.launch = launch
	commentRuntime.Unlock()
	return reads.Close
}

func launchCommentRefresh(work func(context.Context)) bool {
	commentRuntime.RLock()
	launch := commentRuntime.launch
	commentRuntime.RUnlock()
	return launch != nil && launch("douban.comment_refresh", work)
}

// Cold visits and stale refreshes share work; the last departing reader cancels I/O.
func requestCommentsPage(ctx context.Context, key, id string, page int, sort string) (*DoubanCommentsResp, error) {
	commentRuntime.RLock()
	reads := commentRuntime.reads
	commentRuntime.RUnlock()
	return reads.Do(ctx, key, func(ctx context.Context) (*DoubanCommentsResp, error) {
		commentsCache.RLock()
		entry, ok := commentsCache.entries[key]
		commentsCache.RUnlock()
		if ok && entry.data != nil && time.Since(entry.fetchedAt) < cacheTTL {
			return entry.data, nil
		}
		commentRefresh.Lock()
		until := commentRefresh.blocked[key]
		commentRefresh.Unlock()
		if time.Now().Before(until) {
			return nil, apperror.New(apperror.Unavailable, "豆瓣评论暂不可用，请稍后重试")
		}
		data, err := fetchCommentsPage(ctx, id, page, sort)
		if err == nil {
			err = ctx.Err()
		}
		if err == nil && data == nil {
			err = apperror.New(apperror.Unavailable, "豆瓣未返回评论页面")
		}
		if err == nil {
			commentsCounters.fetchOK.Add(1)
			storeCommentsPage(key, data)
		} else if apperror.CodeOf(err) != apperror.Cancelled {
			commentsCounters.fetchFail.Add(1)
			commentRefresh.Lock()
			commentRefresh.blocked[key] = time.Now().Add(commentRefreshBackoff)
			pruneCommentRefreshStateLocked()
			commentRefresh.Unlock()
		}
		return data, err
	})
}
