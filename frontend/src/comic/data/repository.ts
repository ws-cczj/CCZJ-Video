import { readingRequest } from '../../api/requestChain'
import { rankSearchItems } from '../../reading/searchRelevance'
/**
 * 漫画模式的数据访问层：把 api/library.ts 的通用资源接口收成漫画自己的形状。
 *
 * 视图不直接调 api/library.ts，是为了让搜索、详情与来源的组合请求
 * 只在一处拼装，也让逐源搜索结果、可空的嵌套数组这些 DTO 细节不外泄到模板里。
 */
import {
  normalizeApiError,
  addLibrarySource,
  deleteLibrarySource,
  fetchLibraryItem,
  fetchCatalogPage,
  listLibraryItems,
  listLibrarySources,
  refKey,
  refreshLibraryToc,
  resolveLibraryRef,
  resolveLibraryRefs,
  searchLibrary,
  setLibrarySourceEnabled,
  testLibrarySource,
  type ItemDTO,
  type SourceInfoDTO,
  type TestOutcomeDTO,
} from '../../api/library'
import type { ComicChapter, ComicPage, ComicSeries, ComicSeriesDetail, ComicShelfEntry } from '../domain'

/** Normalize structured backend errors; legacy text remains adapter compatibility. */
export function notFound(message: string): Error {
  return new Error(`NOT_FOUND: ${message}`)
}

export interface ComicSourceOutcome {
  sourceKey: string
  sourceName: string
  items: ComicSeries[]
  failed: boolean
  errorCode: string
  errorMessage: string
  hasNext: boolean
}

export interface ComicSearchResult {
  keyword: string
  page: number
  items: ComicSeries[]
  sources: ComicSourceOutcome[]
  total: number
  failedCount: number
  hasNext: boolean
  nextCursor: string
  /** 一个源都没有：这不是失败，是"还没加源"，界面要显示引导而不是错误。 */
  noSources: boolean
  pendingSources: number
  searchedSources: number
}

export function searchComics(keyword: string, page: number, cursor = '', onProgress?: (page:ComicSearchResult)=>void) {
  function map(outcome:import('../../api/library').SearchOutcomeDTO):ComicSearchResult {
    const results = (outcome.results ?? []).filter((result): result is NonNullable<typeof result> => !!result)
    const sources: ComicSourceOutcome[] = []
    const items: ComicSeries[] = []
    for (const result of results) {
      const found = (result.items ?? []).filter((item): item is ItemDTO => !!item)
      items.push(...found)
      sources.push({
        sourceKey: result.sourceKey,
        sourceName: result.sourceName || result.sourceKey,
        items: found,
        failed: !!result.errorCode,
        errorCode: result.errorCode ?? '',
        errorMessage: result.errorMessage ?? '',
        hasNext: !!result.hasNext,
      })
    }
    return {
      keyword: outcome.keyword ?? keyword,
      page: outcome.page ?? page,
      items: rankSearchItems(items,outcome.keyword || keyword),
      sources,
      total: outcome.total ?? items.length,
      failedCount: outcome.failedCount ?? sources.filter(source => source.failed).length,
      hasNext: !!outcome.nextCursor,
      nextCursor: outcome.nextCursor || '',
      noSources: results.length === 0,
      pendingSources: outcome.pendingSources||0,
      searchedSources: outcome.searchedSources??results.length,
    }
  }
  return searchLibrary('comic',keyword,page,cursor,onProgress?outcome=>onProgress(map(outcome)):undefined).then(map)
}

/** 发现页与分类页展示的是"已经入库的全部作品"，不隶属任何单个源。 */
export function listComicLibrary(includeRetired = true): Promise<ComicSeries[]> {
  return fetchCatalogPage('comic', { includeRetired, limit: 60 }).then(page => page.items)
}

export interface ComicSeriesView {
  item: ComicSeries
  chapters: ComicChapter[]
  /** 目录刷新失败时非空。它不替换详情：元信息与旧目录仍然能用。 */
  tocError: string
}

function chaptersOf(detail: ComicSeriesDetail): ComicChapter[] {
  return (detail.chapters ?? []).filter((chapter): chapter is ComicChapter => !!chapter)
}

function viewOf(detail: ComicSeriesDetail): ComicSeriesView {
  if (!detail.item) throw notFound('这部作品不在库里')
  return { item: detail.item, chapters: chaptersOf(detail), tocError: '' }
}

/**
 * 目录是空的就顺手刷一次：搜索只把作品元数据落了库，目录要 Detail/Toc 才有
 * （app/library/service.go 的 searchOne 不写 chapters），所以第一次打开一部作品
 * 必须去源站取一遍，否则目录永远是空的。
 */
export function loadComicSeries(itemId: number) {
 return readingRequest(async request => {
  const view = viewOf(await request(fetchLibraryItem(itemId, false)))
  if (view.chapters.length || !view.item.available) return view
  try {
    return viewOf(await request(refreshLibraryToc(itemId)))
  } catch (error) {
    // 刷新失败不影响已有数据：作品还在架上，用户至少看得到元信息。
    if ((error as Error)?.name === 'CancelError' || normalizeApiError(error).code === 'CANCELLED') throw error
    return { ...view, tocError: messageOf(error) }
  }
 })
}

export function refreshComicToc(itemId: number) {
  return refreshLibraryToc(itemId).then(viewOf)
}

export interface ResolvedEntry {
  key: string
  /** 源被删了、条目下架了、或自然键解析不到。条目保留，只是标出来。 */
  available: boolean
  item: ComicSeries | null
}

/**
 * 批量取回追漫/最近阅读的条目。
 *
 * 按自然键解析（resolveLibraryRef）而不是按本地 id：id 是自增主键，换库或重装就变了，
 * 而追漫列表要跨这些活下来。单条失败不影响其他条目，失败就是"来源已下架"。
 */
export function resolveShelfEntries(entries: readonly ComicShelfEntry[]) {
  return readingRequest(async request => {
    const items = await request(resolveLibraryRefs(entries.map(entry => ({ kind: entry.kind, sourceKey: entry.sourceKey, remoteId: entry.remoteId }))))
    return new Map(entries.map((entry, index) => {
      const key = refKey(entry), item = items[index]
      return [key, { key, available: item?.available ?? false, item }] as const
    }))
  })
}

/**
 * 追漫快照兜出来的作品形状：源已经取不到时，列表仍然要显示出这是哪一部，
 * 而不是凭空少一行（ADR 0016）。available 为假，界面据此标"来源已下架"。
 */
export function snapshotItem(entry: ComicShelfEntry): ComicSeries {
  return {
    id: entry.itemId,
    kind: entry.kind,
    sourceKey: entry.sourceKey,
    sourceName: entry.sourceKey,
    remoteId: entry.remoteId,
    title: entry.title,
    author: '',
    cover: entry.cover,
    summary: '',
    genres: '',
    status: '',
    latest: '',
    words: '',
    updatedAt: '',
    available: false,
  }
}

export function listComicSources(): Promise<SourceInfoDTO[]> {
  return listLibrarySources('comic')
}


export { deleteLibrarySource as deleteComicSource, setLibrarySourceEnabled as setComicSourceEnabled, testLibrarySource as testComicSource }
export type { SourceInfoDTO, TestOutcomeDTO }

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error ?? '')
}
