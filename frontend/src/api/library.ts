import { readingRequest } from './requestChain'
import { response } from './response'
import { required } from './decode'
import { validateLibrary,item,source } from './libraryDecode'
import { CancellablePromise } from '@wailsio/runtime'
import { onBackendEvent } from './events'
import { search as decodeSearch } from './libraryDecode'
import { searchRelevance } from '../reading/searchRelevance'
/** Reading transport adapters preserve Wails cancellation while validating DTOs. */
import {
  LibraryAddSource,
  LibraryCatalogPage,
  LibraryChapterPages,
  LibraryRefreshChapterPages,
  LibraryRefreshReadingToc,
  LibraryNovelContext,
  LibraryComicContext,
  LibraryRefreshComicContext,
  LibraryRefreshNovelContext,
  LibrarySearchStream,
  LibraryChapterText,
  LibraryDeleteSource,
  LibraryItem,
  LibraryItems,
  LibraryResolveRef,
  LibraryResolveRefs,
  LibraryRefreshToc,
  LibrarySearch,
  LibrarySearchPage,
  LibrarySetSourceEnabled,
  LibrarySources,
  LibraryTestSource,
  LibraryUpdateSource,
  normalizeApiError,
} from './app'
import type {
  Chapter as ChapterDTO,
  ChapterPages as ChapterPagesDTO,
  ChapterText as ChapterTextDTO,
  Item as ItemDTO,
  ItemDetail as ItemDetailDTO,
  ReadingContext as ReadingContextDTO,
  ReadingLocation as ReadingLocationDTO,
  SearchOutcome as SearchOutcomeDTO,
  SourceInfo as SourceInfoDTO,
  SourceResult as SourceResultDTO,
  TestOutcome as TestOutcomeDTO,
  TestStage as TestStageDTO,
} from '../../bindings/cczjVideo/app/library/models'

export type {
  ChapterDTO,
  ChapterPagesDTO,
  ChapterTextDTO,
  ItemDTO,
  ItemDetailDTO,
  ReadingContextDTO,
  ReadingLocationDTO,
  SearchOutcomeDTO,
  SourceInfoDTO,
  SourceResultDTO,
  TestOutcomeDTO,
  TestStageDTO,
}

export type NovelReadingContextDTO = ReadingContextDTO
export type NovelReadingLocationDTO = ReadingLocationDTO

/** 阅读模式只有这两种。视频源不走这里（后端会直接拒）。 */
export type LibraryKind = 'novel' | 'comic'

/** Decode non-null responses once through the cancellable transport path. */
function unwrap<T>(promise: CancellablePromise<T | null>, what: string): CancellablePromise<T> {
  return response(()=>promise,value=>validateLibrary(what,required(value)))
}

function unwrapList<T>(promise: CancellablePromise<(T | null)[] | null>, what: string): CancellablePromise<T[]> {
 return response(()=>promise,value=>{
  const values=required(value)
  if(!Array.isArray(values))throw new TypeError('Invalid library collection')
  const rows=values.filter((entry):entry is T=>entry!==null&&entry!==undefined)
  for(const row of rows){if(what==='LibraryItems')item(row);if(what==='LibrarySources')source(row)}
  return rows
 })
}

/**
 * 一个条目的自然键。
 *
 * 书架、追漫与阅读进度都按它存，**不按标题**：同名内容跨源可能是不同译本、
 * 不同版本或不同扫描组，按书名合并会把两份进度混成一份（reports/00:107）。
 * 本地 `id` 是数据库自增主键，重装或换库会变，所以不能拿它当持久键。
 */
export interface LibraryRef {
  kind: LibraryKind
  sourceKey: string
  remoteId: string
}

/** 把 DTO 里的三个字段收成一个自然键。 */
export function refOf(item: ItemDTO): LibraryRef {
  return { kind: item.kind as LibraryKind, sourceKey: item.sourceKey, remoteId: item.remoteId }
}

/** 自然键的稳定字符串形式，用作存储表的键与 Map 的 key。 */
export function refKey(ref: LibraryRef): string {
  return `${ref.kind}\u0000${ref.sourceKey}\u0000${ref.remoteId}`
}

export function parseRefKey(key: string): LibraryRef | null {
  const parts = key.split('\u0000')
  if (parts.length !== 3 || parts[0] !== 'novel' && parts[0] !== 'comic' || !parts[1] || !parts[2]) {
    return null
  }
  return { kind: parts[0] as LibraryKind, sourceKey: parts[1], remoteId: parts[2] }
}

/**
 * 搜索一页。
 *
 * 每个源各自成功或失败：单个源挂了不会让整屏变空，`results[i].errorCode`
 * 非空表示那一个源失败了，而 `items` 为空且没有 errorCode 才代表
 * "这个源确实没有这本书"（reports/00:135）。
 */
let searchSequence=0
export function searchLibrary(kind: LibraryKind, keyword: string, page = 1, cursor = '', onProgress?: (outcome: SearchOutcomeDTO) => void): CancellablePromise<SearchOutcomeDTO> {
  if (onProgress && !(page > 1 && !cursor)) {
    return readingRequest(async request => {
      let accumulated: SourceResultDTO[] = []
      let searched = 0
      const started = Date.now()
      function merge(outcome: SearchOutcomeDTO): SearchOutcomeDTO {
        const bySource = new Map<string,SourceResultDTO>()
        for (const row of [...accumulated,...(outcome.results??[])]) {
          if (!row) continue
          const previous=bySource.get(row.sourceKey)
          const items=[...new Map([...(previous?.items??[]),...(row.items??[])].filter((item):item is ItemDTO=>!!item).map(item=>[item.id,item])).values()]
          bySource.set(row.sourceKey,{...row,items})
        }
        const results = [...bySource.values()]
        const failures = new Set(results.filter(row => row.errorCode).map(row => row.sourceKey))
        return { ...outcome, page, results, total: results.reduce((sum,row)=>sum+(row.items?.length??0),0), failedCount: failures.size, searchedSources:searched+(outcome.searchedSources??0) }
      }
      for (let round = 0; ; round++) {
        const result = await request(searchLibraryStreamPage(kind, keyword, page, cursor, partial => onProgress(merge(partial))))
        const merged = merge(result)
        onProgress(merged)
        // A few bounded rounds cover failed first choices without requiring
        // repeated clicks. The opaque cursor retains every unsearched source.
        // An author can share a book's title as a pen name. Keep author hits
        // visible, but do not let them hide the book on remaining sources.
        const exact = merged.results.some(row => row?.items?.some(item => item && searchRelevance(item,merged.keyword || keyword)===4))
        if (!result.nextCursor || !result.pendingSources || exact || round >= 2 || Date.now()-started>=32000) return merged
        accumulated = merged.results.filter((row): row is SourceResultDTO => !!row)
        searched = merged.searchedSources??0
        cursor = result.nextCursor
      }
    })
  }
  return searchLibraryStreamPage(kind,keyword,page,cursor,onProgress)
}

function searchLibraryStreamPage(kind: LibraryKind, keyword: string, page: number, cursor: string, onProgress?: (outcome: SearchOutcomeDTO) => void): CancellablePromise<SearchOutcomeDTO> {
  if(onProgress && !(page>1&&!cursor)) {
    const requestId=`${Date.now()}-${++searchSequence}`
    const results=new Map<string,SourceResultDTO>()
    const stop=onBackendEvent<{requestId:string;kind:string;keyword:string;result:SourceResultDTO}>('library:search:page',event=>{
      if(event.requestId!==requestId||event.kind!==kind||event.keyword!==keyword)return
      const result=event.result
      const rows=[...results.values()].filter(row=>row.sourceKey!==result.sourceKey).concat(result)
      const partial:SearchOutcomeDTO={keyword,page,results:rows,total:rows.reduce((sum,row)=>sum+(row.items?.length??0),0),failedCount:rows.filter(row=>row.errorCode).length,nextCursor:'',searchedSources:rows.length,pendingSources:0}
      decodeSearch(partial)
      results.set(result.sourceKey,result)
      onProgress(partial)
    })
    const pending=unwrap(LibrarySearchStream(kind,keyword,cursor,requestId),'LibrarySearchStream')
    void pending.then(stop,stop)
    return pending
  }
  // Preserve the numbered-page entry for older callers. Current search views
  // follow the opaque continuation and never reconstruct upstream page URLs.
  return page > 1 && !cursor
    ? unwrap(LibrarySearch(kind, keyword, page), 'LibrarySearch')
    : unwrap(LibrarySearchPage(kind, keyword, cursor), 'LibrarySearchPage')
}

/**
 * 列出一个模式下所有已入库的作品（跨源）。书城与分类页用它。
 *
 * 与"书架"是两件事：书架是用户主动保存的那一份，这里是这个模式下
 * 所有搜到过、已落库的作品。includeRetired 为真时连来源已下架的一起返回，
 * 界面上要能显示成"来源已下架"，而不是让用户的书架凭空少一行。
 */
export function listLibraryItems(kind: LibraryKind, includeRetired = false): CancellablePromise<ItemDTO[]> {
  return unwrapList(LibraryItems(kind, includeRetired), 'LibraryItems')
}

/**
 * 按自然键把一个条目取回来。
 *
 * 书架存的是 `{kind, sourceKey, remoteId}` 而不是本地 id：id 是自增主键，
 * 换库或重装就变了，而书架要跨这些活下来。解析不到时抛 NOT_FOUND，
 * 调用方据此把这一条标成"来源不可用"——**不要**把它从书架上悄悄删掉，
 * 凭空消失是最坏的处置，用户会以为数据丢了。
 */
export function resolveLibraryRef(ref: LibraryRef): CancellablePromise<ItemDTO> {
  return unwrap(LibraryResolveRef(ref.kind, ref.sourceKey, ref.remoteId), 'LibraryResolveRef')
}

/** Resolve metadata in bounded batches; missing identities retain a null slot. */
export function resolveLibraryRefs(refs: readonly LibraryRef[]) {
  return readingRequest(async request => {
    const items: Array<ItemDTO | null> = []
    for (let start = 0; start < refs.length; start += 128) {
      const batch = await request(LibraryResolveRefs(refs.slice(start, start + 128)))
      if (!Array.isArray(batch) || batch.length !== Math.min(128, refs.length - start)) throw new Error('Invalid shelf resolution response')
      for(const value of batch)if(value!==null)item(value)
      items.push(...batch)
    }
    return items
  })
}

/**
 * 取一部作品与它的目录。
 *
 * `refresh` 为真才去源站刷新目录。默认不刷：打开一本书就发一轮目录请求，
 * 在几十个源的书架上等于每次进应用都打一遍源站。
 *
 * 源被删了或配置坏了时**仍然返回条目本身**（`item.available` 为假），
 * 这样书架能显示成"来源已下架"，而不是让用户的书架凭空少一行。
 */
export function fetchLibraryItem(itemId: number, refresh = false): CancellablePromise<ItemDetailDTO> {
  return unwrap(LibraryItem(itemId, refresh), 'LibraryItem')
}

/** 只刷目录。追更用它：元信息很少变，章节列表才是会变的那一半。 */
export function refreshLibraryToc(itemId: number): CancellablePromise<ItemDetailDTO> {
  return unwrap(LibraryRefreshToc(itemId), 'LibraryRefreshToc')
}

/**
 * 取一章正文。
 *
 * `warnings` 是"内容拿到了但有件事该说"：可能被截断、编码像是错了、
 * 疑似字体码位混淆。三条都**不阻断显示**——判错了用户至少还能看到内容，
 * 判对了用户才知道该去改编码或换一个源。
 */
export function fetchChapterText(chapterId: number): CancellablePromise<ChapterTextDTO> {
  return unwrap(LibraryChapterText(chapterId), 'LibraryChapterText')
}

export function fetchNovelContext(location: NovelReadingLocationDTO) {
  return unwrap(LibraryNovelContext(location), 'LibraryNovelContext')
}

export function fetchComicContext(location: ReadingLocationDTO) {
  return unwrap(LibraryComicContext(location), 'LibraryComicContext')
}
export function refreshComicContext(location: ReadingLocationDTO) {
  return unwrap(LibraryRefreshComicContext(location), 'LibraryRefreshComicContext')
}

export function refreshNovelContext(location: NovelReadingLocationDTO) {
  return unwrap(LibraryRefreshNovelContext(location), 'LibraryRefreshNovelContext')
}

/**
 * 取一话的图片列表。
 *
 * 返回的 `pages[].url` 已经是同源代理地址（`/__cczj/img`），直接当 `<img src>` 用。
 * **不要**自己拿上游地址拼：真实漫画 CDN 校验完整 Referer 与源声明的请求头，
 * 那些由后端按 sourceKey 反查后带上，前端拿不到也不该拿到。
 */
export function fetchChapterPages(chapterId: number): CancellablePromise<ChapterPagesDTO> {
  return unwrap(LibraryChapterPages(chapterId), 'LibraryChapterPages')
}

export function refreshChapterPages(chapterId:number) { return unwrap(LibraryRefreshChapterPages(chapterId),'LibraryRefreshChapterPages') }
export function refreshReadingToc(itemId:number,chapterId:number) { return unwrap(LibraryRefreshReadingToc(itemId,chapterId),'LibraryRefreshReadingToc') }

// ============================ 源管理 ============================

export function listLibrarySources(kind: LibraryKind): CancellablePromise<SourceInfoDTO[]> {
  return unwrapList(LibrarySources(kind), 'LibrarySources')
}

export function addLibrarySource(kind: LibraryKind, name: string, apiUrl: string, document: string): CancellablePromise<SourceInfoDTO> {
  return unwrap(LibraryAddSource(kind, name, apiUrl, document), 'LibraryAddSource')
}

export function updateLibrarySource(sourceKey: string, name: string, apiUrl: string, document: string): CancellablePromise<void> {
  return LibraryUpdateSource(sourceKey, name, apiUrl, document)
}

export function deleteLibrarySource(sourceKey: string): CancellablePromise<void> {
  return LibraryDeleteSource(sourceKey)
}

export function setLibrarySourceEnabled(sourceKey: string, enabled: boolean): CancellablePromise<void> {
  return LibrarySetSourceEnabled(sourceKey, enabled)
}

/**
 * 逐级试一个源：配置 → 搜索 → 详情 → 目录 → 正文/图片。
 *
 * 逐级是因为"能搜到"与"能读"是两件事：目录规则写错时搜索是正常的。
 * 每一步失败都**继续**下一步，所以一次测试能把所有坏掉的步骤一次性报出来，
 * 不用修一步再测一次。
 */
export function testLibrarySource(sourceKey: string): CancellablePromise<TestOutcomeDTO> {
  return unwrap(LibraryTestSource(sourceKey), 'LibraryTestSource')
}

/** 把后端错误翻成界面能用的形状。所有 catch 都应该过这一道。 */
export { normalizeApiError }

export interface CatalogPageDTO { items: ItemDTO[]; genres: string[]; nextCursor: string }
export function fetchCatalogPage(kind: LibraryKind, options: { genre?: string; complete?: boolean; sort?: string; includeRetired?: boolean; cursor?: string; limit?: number } = {}) {
  return unwrap(LibraryCatalogPage({ kind, genre: options.genre ?? '', complete: options.complete ?? false, sort: options.sort ?? '', includeRetired: options.includeRetired ?? true, cursor: options.cursor ?? '', limit: options.limit ?? 60 }), 'LibraryCatalogPage').then(page => ({ ...page, items: (page.items ?? []).filter((item): item is ItemDTO => !!item) }))
}
