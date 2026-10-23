package douban

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cczjVideo/app/db"
)

func TestCommentsCacheSurvivesProcessMemoryReset(t *testing.T) {
	isolateComments(t)
	if err := db.InitDB(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	seedComments("unused", commentPage("unused"), 0)
	var calls atomic.Int32
	fetchCommentsPage = func(context.Context, string, int, string) (*DoubanCommentsResp, error) {
		calls.Add(1)
		return commentPage("saved"), nil
	}
	if _, err := FetchComments("2400099", 1, "new_score"); err != nil {
		t.Fatal(err)
	}
	// Model a new process: discard RAM, retain the database.
	commentsCache.Lock()
	commentsCache.entries = map[string]commentCacheEntry{}
	commentsCache.Unlock()
	resp, err := FetchComments("2400099", 1, "new_score")
	if err != nil || resp.Comments[0].ID != "saved" {
		t.Fatalf("restart result: %v, %v", resp, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("restart fetched again: calls=%d", calls.Load())
	}
	if cacheTTL < 30*time.Minute || chartCacheTTL < 30*time.Minute {
		t.Fatal("successful caches must last at least 30 minutes")
	}
	t.Run("original timestamp and bounded snapshot", func(t *testing.T) {
		at := time.Now().Add(-31 * time.Minute).Truncate(time.Second)
		commentsCache.Lock()
		commentsCache.entries = map[string]commentCacheEntry{}
		for i := 0; i < maxCommentSnapshotPages+10; i++ {
			commentsCache.entries[fmt.Sprintf("%d_1_time", i+1)] = commentCacheEntry{data: commentPage("saved"), fetchedAt: at.Add(-time.Duration(i) * time.Second)}
		}
		commentsCache.Unlock()
		saveCommentSnapshots()
		raw, err := db.GetSetting(commentsSnapshotKey)
		if err != nil {
			t.Fatal(err)
		}
		var pages []commentSnapshot
		if json.Unmarshal([]byte(raw), &pages) != nil || len(pages) != maxCommentSnapshotPages {
			t.Fatalf("invalid snapshot size: %d", len(pages))
		}
		commentsCache.Lock()
		commentsCache.entries = map[string]commentCacheEntry{}
		commentsCache.Unlock()
		loadCommentSnapshots()
		commentsCache.RLock()
		entry := commentsCache.entries["1_1_time"]
		commentsCache.RUnlock()
		if !entry.fetchedAt.Equal(at) {
			t.Fatalf("cache read renewed fetch time: %s vs %s", entry.fetchedAt, at)
		}
		ClearCommentsCache()
		if raw, _ := db.GetSetting(commentsSnapshotKey); raw != "" {
			t.Fatal("clear left persistent cache behind")
		}
	})

}

func TestCommentsConcurrentColdRequestsFetchOnce(t *testing.T) {
	isolateComments(t)
	seedComments("unused", commentPage("unused"), 0)
	var calls atomic.Int32
	fetchCommentsPage = func(context.Context, string, int, string) (*DoubanCommentsResp, error) {
		calls.Add(1)
		time.Sleep(30 * time.Millisecond)
		return commentPage("shared"), nil
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := FetchComments("2400098", 1, "new_score"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("same cold page fetched %d times", calls.Load())
	}
}
