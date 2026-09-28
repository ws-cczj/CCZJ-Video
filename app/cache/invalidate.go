// 统一缓存失效层。
//
// 这条链路上有三类派生缓存：Go 进程内的详情缓存（detail.Default）、豆瓣侧的本机镜像
// （热榜匹配、评论），以及前端自己的一堆（详情 localStorage、海报、TS 片段内存+IndexedDB）。
// 数据被改动（删片、改源、清库、一轮采集写库）之后，"谁该丢掉旧内容"这件事以前散落在各个
// 调用点，结果就是大多数地方压根没做。这里收拢成唯一入口：Go 侧直接清自己的，前端侧靠
// cache:invalidate 事件通知，两边的失效范围由同一个 payload 描述。
//
// 注意边界：只失效"我们自己的数据变了才会错"的缓存。豆瓣榜单正文和本机没有派生关系，
// 重新抓一次还要付限流代价，所以任何自动失效路径都不碰它（只有用户显式清除才会）。

package cache

import (
	"sync"

	"cczjVideo/app/applog"
	"cczjVideo/app/db"
	"cczjVideo/app/detail"
	"cczjVideo/app/douban"
)

// InvalidateEvent 是通知前端丢缓存的事件名，前端在 stores/cacheInvalidate.ts 订阅。
const InvalidateEvent = "cache:invalidate"

// 失效范围。前端按范围决定清到哪一层，Go 也按同一套范围决定清哪些缓存。
const (
	ScopeVideo  = "video"
	ScopeSource = "source"
	ScopeAll    = "all"
)

// InvalidateEventPayload 描述一次失效的作用域。
type InvalidateEventPayload struct {
	Scope     string   `json:"scope"`
	SourceKey string   `json:"source_key"`
	VodIDs    []string `json:"vod_ids"`
	Reason    string   `json:"reason"`
}

// EventPublisher 把事件送给 UI 层。cache 包自己不依赖 Wails（handler/collection 也才能
// 直接调它），启动时由 app/service 把 Event.Emit 接进来。
type EventPublisher func(name string, data any)

var (
	publishMu sync.Mutex
	publisher EventPublisher
)

// SetEventPublisher 接上失效事件的出口，重复调用以最后一次为准。
func SetEventPublisher(pub EventPublisher) {
	publishMu.Lock()
	publisher = pub
	publishMu.Unlock()
}

// DeleteCatalogVideo 软删除一条目录条目，并在删除成功后让它的全部缓存作废。
//
// 为什么不直接调 db.DeleteCatalogVideo：详情缓存的键是 source_key:global_id，而 global_id
// 只能在行还是 active 时查得到（目录查询过滤掉了已删除行）。先解析、再删除、后失效，
// 顺序错了就只能整源清空。
func DeleteCatalogVideo(sourceKey, vodID, reason string) error {
	var globalID int64
	if item, err := db.GetCatalogItem(sourceKey, vodID); err == nil && item != nil {
		globalID = item.GlobalID
	}
	if err := db.DeleteCatalogVideo(sourceKey, vodID); err != nil {
		return err
	}
	InvalidateVideo(sourceKey, vodID, globalID, reason)
	return nil
}

// RestoreCatalogVideo 把回收站里的一条放回视频库，并让它作废过的缓存重新可取。
// 恢复后必须失效：删除期间前端和详情缓存可能存着"这条不存在"或旧字段的结果。
func RestoreCatalogVideo(sourceKey, vodID, reason string) error {
	var globalID int64
	if item, err := db.GetRecycleItem(sourceKey, vodID); err == nil && item != nil {
		globalID = item.GlobalId
	}
	if err := db.RestoreCatalogVideo(sourceKey, vodID); err != nil {
		return err
	}
	InvalidateVideo(sourceKey, vodID, globalID, reason)
	return nil
}

// PurgeCatalogVideo 彻底删除一条回收站条目。同样先解析 global_id 再删，
// 删完就再也查不到了。
func PurgeCatalogVideo(sourceKey, vodID, reason string) (int, error) {
	var globalID int64
	if item, err := db.GetRecycleItem(sourceKey, vodID); err == nil && item != nil {
		globalID = item.GlobalId
	}
	deleted, err := db.PurgeCatalogVideo(sourceKey, vodID)
	if err != nil {
		return 0, err
	}
	InvalidateVideo(sourceKey, vodID, globalID, reason)
	return deleted, nil
}

// ClearRecycleBin 清空回收站。按受影响的源整体失效，而不是逐条发事件——
// 回收站可能攒着几千条，逐条广播前端也处理不完。
func ClearRecycleBin(sourceKey, reason string) (int, error) {
	keys, err := db.RecycleSourceKeys(sourceKey)
	if err != nil {
		return 0, err
	}
	deleted, err := db.ClearRecycleBin(sourceKey)
	if err != nil {
		return 0, err
	}
	for _, key := range keys {
		InvalidateSource(key, reason)
	}
	return deleted, nil
}

// MergeGlobalVideoIdentities 把用户确认的几条身份并成一条，并让受影响源的缓存作废。
//
// 为什么整源失效而不是逐条：详情缓存的键是 source_key:global_id，合并既改写了存活行的字段，
// 又把兄弟行的归属搬走了，前端手里可能同时缓存着这两条身份的详细结果。先查源再合并，
// 是因为合并之后被并掉的身份就再也查不出它用过哪些源。
func MergeGlobalVideoIdentities(globalIDs []int64, reason string) (int64, int, error) {
	keys, err := db.IdentitySourceKeys(globalIDs)
	if err != nil {
		return 0, 0, err
	}
	keep, merged, err := db.MergeGlobalVideoIdentities(globalIDs)
	if err != nil {
		return 0, 0, err
	}
	for _, key := range keys {
		InvalidateSource(key, reason)
	}
	return keep, merged, nil
}

// InvalidateVideo 让单条视频的派生缓存作废。globalID 传 0 表示调用方拿不到身份，
// 此时只通知前端按 vod_id 清理（前端缓存的键就是 source_key:vod_id）。
func InvalidateVideo(sourceKey, vodID string, globalID int64, reason string) {
	if sourceKey == "" || vodID == "" {
		return
	}
	detail.Default.InvalidateVideo(sourceKey, globalID)
	applog.Debug("[Cache] 失效单条缓存 %s:%s (global=%d) 原因=%s", sourceKey, vodID, globalID, reason)
	emit(InvalidateEventPayload{Scope: ScopeVideo, SourceKey: sourceKey, VodIDs: []string{vodID}, Reason: reason})
}

// InvalidateSource 在源配置变化、清空源数据或一轮采集真的写入数据之后调用。
// 这里不清评论和榜单正文：它们只跟豆瓣侧内容有关，源里重新采集不会让它们变错。
func InvalidateSource(sourceKey, reason string) {
	if sourceKey == "" {
		return
	}
	detail.Default.InvalidateSource(sourceKey)
	douban.ClearChartMatchCache()
	applog.Debug("[Cache] 失效整源缓存 %s 原因=%s", sourceKey, reason)
	emit(InvalidateEventPayload{Scope: ScopeSource, SourceKey: sourceKey, Reason: reason})
}

// InvalidateAll 是用户显式"清除缓存"的入口：把所有本机派生缓存清干净，包括那些要靠
// 重新请求才能补回来的（评论、热榜匹配）。榜单正文仍然保留 —— 它唯一的取回代价是一次
// 限流下的豆瓣抓取，且与本库数据无关。
func InvalidateAll(reason string) {
	detail.Default.Clear()
	douban.ClearChartMatchCache()
	douban.ClearCommentsCache()
	applog.Info("[Cache] 已清除全部本机派生缓存 原因=%s", reason)
	emit(InvalidateEventPayload{Scope: ScopeAll, Reason: reason})
}

// CacheStats 是失效层能观测到的全部内存缓存，诊断台用它确认失效真的发生了。
type CacheStats struct {
	DetailEntries int   `json:"detail_entries"`
	DetailBytes   int64 `json:"detail_bytes"`
	ChartMatches  int   `json:"chart_matches"`
	CommentPages  int   `json:"comment_pages"`
}

// Stats 汇总当前存活缓存条目数。
func Stats() CacheStats {
	entries, bytes := detail.Default.Stats()
	return CacheStats{
		DetailEntries: entries,
		DetailBytes:   bytes,
		ChartMatches:  douban.ChartMatchCount(),
		CommentPages:  douban.CommentCacheCount(),
	}
}

func emit(payload InvalidateEventPayload) {
	publishMu.Lock()
	publish := publisher
	publishMu.Unlock()
	// 没接上事件通道（单测、启动早期）就只清 Go 侧：该失效的已经失效了，前端那一侧
	// 最坏是等到自己的 TTL 或用户手动清除。
	if publish == nil {
		return
	}
	publish(InvalidateEvent, payload)
}
