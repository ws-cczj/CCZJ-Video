package service

import (
	"cczjVideo/app/db"
	"cczjVideo/app/library"
	"cczjVideo/app/model"
	"cczjVideo/app/provider"
	"context"
)

// NovelBrowseChannels lists implemented remote shelves for the novel library.
func (a *App) NovelBrowseChannels() []provider.BrowseChannel {
	return a.library.NovelBrowseChannels()
}

// NovelBrowsePage browses the source without requiring a search keyword.
func (a *App) NovelBrowsePage(ctx context.Context, channel, cursor string, refresh bool) (*library.NovelBrowsePage, error) {
	return runRequest(a, ctx, "reading.NovelBrowsePage", func(ctx context.Context) (*library.NovelBrowsePage, error) {
		return a.library.BrowseNovels(ctx, channel, cursor, refresh)
	})
}

// librarySearchResultEvent 是聚合搜索的逐源结果事件。
//
// 搜索不等最慢的源：每个源一有结果就发一次，界面可以渐进渲染，
// 而不是转圈到全部源都回来才一次性刷出来（reports/00:131）。
// 绑定方法的返回值仍然是完整汇总，所以不订阅事件的调用方也能用。
const librarySearchResultEvent = "library:search:result"

type ReadingSearchEvent struct {
	RequestID string                `json:"requestId"`
	Kind      string                `json:"kind"`
	Keyword   string                `json:"keyword"`
	Result    *library.SourceResult `json:"result"`
}

func (a *App) LibrarySearchStream(ctx context.Context, kind, keyword, cursor, requestID string) (*library.SearchOutcome, error) {
	return runRequest(a, ctx, "reading.LibrarySearchStream", func(ctx context.Context) (*library.SearchOutcome, error) {
		return a.library.SearchPageStream(ctx, model.SourceKind(kind), keyword, cursor, func(result *library.SourceResult) {
			if a.app != nil && ctx.Err() == nil {
				a.app.Event.Emit("library:search:page", ReadingSearchEvent{RequestID: requestID, Kind: kind, Keyword: keyword, Result: result})
			}
		})
	})
}

func (a *App) LibraryCatalogPage(ctx context.Context, query db.LibraryPageQuery) (*library.CatalogPage, error) {
	return runRequest(a, ctx, "reading.LibraryCatalogPage", func(ctx context.Context) (*library.CatalogPage, error) {
		return a.library.CatalogPage(ctx, query)
	})
}

func (a *App) LibraryResolveRefs(ctx context.Context, refs []db.LibraryRef) ([]*library.Item, error) {
	return runRequest(a, ctx, "reading.LibraryResolveRefs", func(ctx context.Context) ([]*library.Item, error) {
		return a.library.ResolveRefs(ctx, refs)
	})
}

// readingSourceHeaders 给图片代理反查一个源声明的请求头。
//
// 头不走 URL 参数：让 WebView 能在查询串里塞任意请求头，等于把"用本机的网络
// 身份发任意请求"开放给了页面脚本。按 source_key 反查用户自己配的内容，
// 与海报代理的信任模型一致。
func readingSourceHeaders(sourceKey string) map[string]string {
	return library.SourcesHeaders(sourceKey)
}

// ReadingImageURL 把一个上游图片地址换成同源代理地址。
//
// referer 必须是**完整的页面地址**，不要截成 origin：真实漫画 CDN 校验的
// 往往就是那个含路径的地址，而截断正是 /__cczj/img 这条路由存在的原因
// （ADR 0016、pending-decisions 第 16 条）。sourceKey 用来让代理反查这个源
// 声明的请求头。
func (a *App) ReadingImageURL(upstream, referer, sourceKey string) string {
	if a.readingImages == nil {
		return upstream
	}
	return a.readingImages.URL(upstream, referer, sourceKey)
}

// ======================== 源管理 ========================

// LibrarySources 列出某个模式下的源。kind 取 novel 或 comic。
func (a *App) LibrarySources(kind string) ([]*library.SourceInfo, error) {
	return a.library.ListSources(model.SourceKind(kind))
}

// LibraryAddSource 加一个源。document 是 provider.Document 的 JSON 原文，
// 会先校验再落库：一份带 js / webView 之类不支持字段的文档必须在这一刻
// 报出具名错误，而不是存进去之后表现成"搜不出东西"。
func (a *App) LibraryAddSource(kind, name, apiURL, document string) (*library.SourceInfo, error) {
	return a.library.AddSource(model.SourceKind(kind), name, apiURL, document)
}

func (a *App) LibraryUpdateSource(sourceKey, name, apiURL, document string) error {
	return a.library.UpdateSource(sourceKey, name, apiURL, document)
}

func (a *App) LibraryDeleteSource(sourceKey string) error {
	return a.library.DeleteSource(sourceKey)
}

func (a *App) LibrarySetSourceEnabled(sourceKey string, enabled bool) error {
	return a.library.SetSourceEnabled(sourceKey, enabled)
}

// LibraryTestSource 逐级试一个源：配置 → 搜索 → 详情 → 目录 → 正文/图片。
//
// 逐级是因为"能搜到"与"能读"是两件事：目录规则写错时搜索是正常的。
// 每一步失败都继续下一步，一次测试把所有坏掉的步骤都报出来。
func (a *App) LibraryTestSource(ctx context.Context, sourceKey string) (*library.TestOutcome, error) {
	return runRequest(a, ctx, "reading.LibraryTestSource", func(ctx context.Context) (*library.TestOutcome, error) {
		return a.library.TestSource(ctx, sourceKey)
	})
}

// ======================== 内容 ========================

// LibrarySearch 在所有启用的源上搜一遍。每个源各自成功或失败，
// 单个源挂了不会让整屏搜索变空；空列表只代表一次成功的"没有结果"。
func (a *App) LibrarySearch(ctx context.Context, kind, keyword string, page int) (*library.SearchOutcome, error) {
	return runRequest(a, ctx, "reading.LibrarySearch", func(ctx context.Context) (*library.SearchOutcome, error) {
		return a.library.Search(ctx, model.SourceKind(kind), keyword, page,
			func(result *library.SourceResult) {
				if a.app == nil || ctx.Err() != nil {
					return
				}
				a.app.Event.Emit(librarySearchResultEvent, result)
			})
	})
}

// LibrarySearchPage preserves each source's continuation. An empty cursor starts
// a new query; subsequent calls use only the opaque cursor returned by this app.
func (a *App) LibrarySearchPage(ctx context.Context, kind, keyword, cursor string) (*library.SearchOutcome, error) {
	return runRequest(a, ctx, "reading.LibrarySearchPage", func(ctx context.Context) (*library.SearchOutcome, error) {
		return a.library.SearchPage(ctx, model.SourceKind(kind), keyword, cursor)
	})
}

// LibraryItems 列出一个模式下所有已入库的作品（跨源）。书城与分类页用它。
// includeRetired 为真时连来源已下架的一起返回：书架指着它们，
// 界面上要能显示成"来源已下架"，而不是让用户的书架凭空少一行。
func (a *App) LibraryItems(kind string, includeRetired bool) ([]*library.Item, error) {
	return a.library.Items(model.SourceKind(kind), includeRetired)
}

// LibraryResolveRef 按自然键 {kind, sourceKey, remoteId} 把一个条目取回来。
//
// 书架存的是自然键而不是本地 id：id 是自增主键，换库或重装就变了，
// 而书架要跨这些活下来。解析不到时返回 NOT_FOUND，前端据此把这一条
// 标成"来源不可用"，而不是把它从书架上悄悄删掉。
func (a *App) LibraryResolveRef(kind, sourceKey, remoteID string) (*library.Item, error) {
	return a.library.ResolveRef(model.SourceKind(kind), sourceKey, remoteID)
}

// LibraryItem 取一部作品与它的目录。refresh 为真时才去源站刷新目录。
func (a *App) LibraryItem(ctx context.Context, itemID int, refresh bool) (*library.ItemDetail, error) {
	return runRequest(a, ctx, "reading.LibraryItem", func(ctx context.Context) (*library.ItemDetail, error) {
		return a.library.Detail(ctx, itemID, refresh)
	})
}

// LibraryNovelContext returns only the reader's chapter window.
func (a *App) LibraryNovelContext(ctx context.Context, location library.NovelReadingLocation) (*library.NovelReadingContext, error) {
	return runRequest(a, ctx, "reading.LibraryNovelContext", func(ctx context.Context) (*library.NovelReadingContext, error) {
		return a.library.NovelContext(ctx, location)
	})
}

func (a *App) LibraryRefreshNovelContext(ctx context.Context, location library.NovelReadingLocation) (*library.NovelReadingContext, error) {
	return runRequest(a, ctx, "reading.LibraryRefreshNovelContext", func(ctx context.Context) (*library.NovelReadingContext, error) {
		return a.library.RefreshNovelContext(ctx, location)
	})
}

// LibraryComicContext returns only the current comic episode and its neighbors.
func (a *App) LibraryComicContext(ctx context.Context, location library.ReadingLocation) (*library.ReadingContext, error) {
	return runRequest(a, ctx, "reading.LibraryComicContext", func(ctx context.Context) (*library.ReadingContext, error) {
		return a.library.ComicContext(ctx, location)
	})
}
func (a *App) LibraryRefreshComicContext(ctx context.Context, location library.ReadingLocation) (*library.ReadingContext, error) {
	return runRequest(a, ctx, "reading.LibraryRefreshComicContext", func(ctx context.Context) (*library.ReadingContext, error) {
		return a.library.RefreshComicContext(ctx, location)
	})
}

// LibraryRefreshToc explicitly requests the complete updated directory.
func (a *App) LibraryRefreshToc(ctx context.Context, itemID int) (*library.ItemDetail, error) {
	return runRequest(a, ctx, "reading.LibraryRefreshToc", func(ctx context.Context) (*library.ItemDetail, error) {
		return a.library.RefreshToc(ctx, itemID)
	})
}

// LibraryChapterText 取一章正文。Warnings 里是"内容拿到了但有件事该说"：
// 可能被截断、编码像是错了、疑似字体加密。三条都不阻断显示。
func (a *App) LibraryChapterText(ctx context.Context, chapterID int) (*library.ChapterText, error) {
	return runRequest(a, ctx, "reading.LibraryChapterText", func(ctx context.Context) (*library.ChapterText, error) {
		return a.library.ChapterText(ctx, chapterID)
	})
}

// LibraryChapterPages 取一话的图片列表。返回的 URL 已经是同源代理地址，
// 前端直接当 <img src> 用，不要自己拼。
func (a *App) LibraryChapterPages(ctx context.Context, chapterID int) (*library.ChapterPages, error) {
	return runRequest(a, ctx, "reading.LibraryChapterPages", func(ctx context.Context) (*library.ChapterPages, error) {
		return a.library.ChapterPages(ctx, chapterID)
	})
}

func (a *App) LibraryRefreshChapterPages(ctx context.Context, chapterID int) (*library.ChapterPages, error) {
	return runRequest(a, ctx, "reading.LibraryRefreshChapterPages", func(ctx context.Context) (*library.ChapterPages, error) {
		return a.library.RefreshChapterPages(ctx, chapterID)
	})
}

func (a *App) LibraryRefreshReadingToc(ctx context.Context, itemID, chapterID int) (*library.ItemDetail, error) {
	return runRequest(a, ctx, "reading.LibraryRefreshReadingToc", func(ctx context.Context) (*library.ItemDetail, error) {
		return a.library.RefreshReadingToc(ctx, itemID, chapterID)
	})
}

func (a *App) ComicTrending(ctx context.Context, board string, refresh bool) (*library.ComicTrendPage, error) {
	return runRequest(a, ctx, "reading.ComicTrending", func(ctx context.Context) (*library.ComicTrendPage, error) {
		return a.library.TrendingComics(ctx, board, refresh)
	})
}
