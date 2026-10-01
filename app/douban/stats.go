package douban

import "sync/atomic"

// SWR 是一块 stale-while-revalidate 缓存的读数。它记的是「这一趟进程里怎么被用的」，
// 重启后归零，这样诊断页上的命中率永远对得上用户刚做的那一圈操作。
type SWR struct {
	Entries   int   `json:"entries"`
	Hits      int64 `json:"hits"`
	StaleHits int64 `json:"stale_hits"`
	Misses    int64 `json:"misses"`
	FetchOK   int64 `json:"fetch_ok"`
	FetchFail int64 `json:"fetch_fail"`
	// Skipped 是「想刷新但没刷」的次数：后台已经有一趟在跑，或者还在失败退避里。
	// 它和 FetchFail 一起才说得清「为什么这份榜单三天没变」。
	Skipped int64 `json:"skipped"`
}

// swrCounters 用原子计数而不是锁：这些读数只在读路径上顺带 +1，不该和缓存本身抢同一把锁，
// 更不该被诊断页的读拖住抓取。
type swrCounters struct {
	hits      atomic.Int64
	staleHits atomic.Int64
	misses    atomic.Int64
	fetchOK   atomic.Int64
	fetchFail atomic.Int64
	skipped   atomic.Int64
}

var (
	chartCounters    swrCounters
	commentsCounters swrCounters
)

// ChartCacheStats 返回热榜缓存的条目数与读法计数。条目数包括来源不明、只用于兜底的降级数据。
func ChartCacheStats() SWR {
	chartCacheMu.RLock()
	entries := len(chartCacheData)
	chartCacheMu.RUnlock()
	return SWR{
		Entries:   entries,
		Hits:      chartCounters.hits.Load(),
		StaleHits: chartCounters.staleHits.Load(),
		Misses:    chartCounters.misses.Load(),
		FetchOK:   chartCounters.fetchOK.Load(),
		FetchFail: chartCounters.fetchFail.Load(),
		Skipped:   chartCounters.skipped.Load(),
	}
}

// CommentCacheStats 返回评论缓存的页数与读法计数。
func CommentCacheStats() SWR {
	commentsCache.RLock()
	entries := len(commentsCache.entries)
	commentsCache.RUnlock()
	return SWR{
		Entries:   entries,
		Hits:      commentsCounters.hits.Load(),
		StaleHits: commentsCounters.staleHits.Load(),
		Misses:    commentsCounters.misses.Load(),
		FetchOK:   commentsCounters.fetchOK.Load(),
		FetchFail: commentsCounters.fetchFail.Load(),
		Skipped:   commentsCounters.skipped.Load(),
	}
}
