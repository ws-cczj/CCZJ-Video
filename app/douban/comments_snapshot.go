package douban

import (
	"encoding/json"
	"sort"
	"sync"
	"time"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
)

const commentsSnapshotKey = "douban_comments_snapshot"
const maxCommentSnapshotPages = 80
const maxCommentSnapshotBytes = 8 << 20

type commentSnapshot struct {
	Key       string              `json:"key"`
	FetchedAt int64               `json:"fetched_at"`
	Data      *DoubanCommentsResp `json:"data"`
}

var commentSnapshotMu sync.Mutex

// Only used on a RAM miss. Keep the original fetch time across restarts;
// opening a cached page must never renew its lifetime.
func loadCommentSnapshots() {
	commentSnapshotMu.Lock()
	defer commentSnapshotMu.Unlock()
	raw, err := db.GetSetting(commentsSnapshotKey)
	if err != nil || raw == "" || len(raw) > maxCommentSnapshotBytes {
		return
	}
	var pages []commentSnapshot
	if json.Unmarshal([]byte(raw), &pages) != nil || len(pages) > maxCommentSnapshotPages {
		return
	}
	now := time.Now()
	commentsCache.Lock()
	defer commentsCache.Unlock()
	for _, p := range pages {
		at := time.Unix(p.FetchedAt, 0)
		if p.Key == "" || p.Data == nil || p.FetchedAt <= 0 || at.After(now) || now.Sub(at) >= commentStaleCeiling {
			continue
		}
		if old, ok := commentsCache.entries[p.Key]; !ok || old.fetchedAt.Before(at) {
			commentsCache.entries[p.Key] = commentCacheEntry{data: p.Data, fetchedAt: at}
		}
	}
	evictOldestCommentsLocked()
}

// Persist at most 80 recent pages, only after a successful network fetch.
// Serialize snapshot writes so simultaneous pages cannot overwrite a newer one.
func saveCommentSnapshots() {
	commentSnapshotMu.Lock()
	defer commentSnapshotMu.Unlock()
	commentsCache.RLock()
	pages := make([]commentSnapshot, 0, len(commentsCache.entries))
	for key, p := range commentsCache.entries {
		if p.data != nil && time.Since(p.fetchedAt) < commentStaleCeiling {
			pages = append(pages, commentSnapshot{key, p.fetchedAt.Unix(), p.data})
		}
	}
	commentsCache.RUnlock()
	sort.Slice(pages, func(i, j int) bool { return pages[i].FetchedAt > pages[j].FetchedAt })
	if len(pages) > maxCommentSnapshotPages {
		pages = pages[:maxCommentSnapshotPages]
	}
	// Encode one page at a time, bounding the batch before its final allocation.
	// Repeatedly serializing the entire batch while trimming could allocate
	// hundreds of megabytes for a few unusually large upstream pages.
	encoded := make([]json.RawMessage, 0, len(pages))
	size := 2
	for _, page := range pages {
		raw, err := json.Marshal(page)
		if err != nil || size+len(raw)+1 > maxCommentSnapshotBytes {
			continue
		}
		encoded = append(encoded, raw)
		size += len(raw) + 1
	}
	raw, err := json.Marshal(encoded)
	if err != nil {
		return
	}
	if err := db.SetSetting(commentsSnapshotKey, string(raw)); err != nil && db.DB() != nil {
		applog.Warn("[DoubanComments] 评论快照保存失败: %v", err)
	}

}

func clearCommentSnapshots() {
	commentSnapshotMu.Lock()
	defer commentSnapshotMu.Unlock()
	_ = db.SetSetting(commentsSnapshotKey, "")
}
