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

// Row 是一块 Go 侧进程内缓存的读数。键（detail/chart/comments/match）是给界面查文案用的
// 稳定标识，不是中文标题——文案由前端 i18n 出，中英才可能对得上。
//
// 只统计「我们自己的」缓存：前端那几块（TS 片段、海报、图片代理）只有浏览器知道命中没有，
// 由界面自己报，所以这里的出网计数看不见它们。
type Row struct {
	Key       string `json:"key"`
	Entries   int    `json:"entries"`
	Bytes     int64  `json:"bytes"`
	Hits      int64  `json:"hits"`
	StaleHits int64  `json:"stale_hits"`
	Misses    int64  `json:"misses"`
	FetchOK   int64  `json:"fetch_ok"`
	FetchFail int64  `json:"fetch_fail"`
	Skipped   int64  `json:"skipped"`
}

// Stats 汇总当前存活的缓存条目与读法计数。热榜匹配只是「查过一次就记住」的映射，
// 没有新鲜度概念，所以它只有条目数——它存在的意义是让用户确认「清除缓存」真的清到了东西。
func Stats() []Row {
	detailStats := detail.Default.Stats()
	chart := douban.ChartCacheStats()
	comments := douban.CommentCacheStats()
	return []Row{
		{
			Key: "detail", Entries: detailStats.Entries, Bytes: detailStats.Bytes,
			Hits: detailStats.Hits, StaleHits: detailStats.StaleHits, Misses: detailStats.Misses,
			FetchOK: detailStats.FetchOK, FetchFail: detailStats.FetchFail,
		},
		{Key: "chart", Entries: chart.Entries, Hits: chart.Hits, StaleHits: chart.StaleHits,
			Misses: chart.Misses, FetchOK: chart.FetchOK, FetchFail: chart.FetchFail, Skipped: chart.Skipped},
		{Key: "comments", Entries: comments.Entries, Hits: comments.Hits, StaleHits: comments.StaleHits,
			Misses: comments.Misses, FetchOK: comments.FetchOK, FetchFail: comments.FetchFail, Skipped: comments.Skipped},
		{Key: "match", Entries: douban.ChartMatchCount()},
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
