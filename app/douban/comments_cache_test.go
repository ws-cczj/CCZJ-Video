package douban

import (
	"strconv"
	"testing"
	"time"
)

func TestCommentsCacheEvictsOldestOverCap(t *testing.T) {
	defer ClearCommentsCache()

	base := time.Now().Add(-time.Hour)
	commentsCache.Lock()
	commentsCache.entries = make(map[string]commentCacheEntry)
	for i := 0; i <= maxCommentCacheEntries; i++ {
		commentsCache.entries["c_"+strconv.Itoa(i)] = commentCacheEntry{
			data:      &DoubanCommentsResp{Page: i},
			fetchedAt: base.Add(time.Duration(i) * time.Second),
		}
	}
	evictOldestCommentsLocked()
	_, oldestKept := commentsCache.entries["c_0"]
	_, newestKept := commentsCache.entries["c_"+strconv.Itoa(maxCommentCacheEntries)]
	size := len(commentsCache.entries)
	commentsCache.Unlock()

	if size != maxCommentCacheEntries {
		t.Fatalf("缓存淘汰后仍有 %d 条，期望 %d 条", size, maxCommentCacheEntries)
	}
	if oldestKept {
		t.Error("最早抓取的条目应被淘汰")
	}
	if !newestKept {
		t.Error("最新抓取的条目不应被淘汰")
	}
}
