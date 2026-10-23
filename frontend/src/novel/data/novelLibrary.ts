import { readingRequest } from '../../api/requestChain'
import { rankSearchItems } from '../../reading/searchRelevance'
import {
  parseRefKey,
  resolveLibraryRefs,
  fetchChapterText,
  fetchLibraryItem,
  listLibraryItems,
  listLibrarySources,
  normalizeApiError,
  refreshLibraryToc,
  searchLibrary,
} from '../../api/library'
import type {
  ChapterTextDTO,
  ItemDetailDTO,
  ItemDTO,
  SourceInfoDTO,
  SearchOutcomeDTO,
} from '../../api/library'
import type { NovelChapter } from '../domain'

export interface NovelSearchFailure {
  sourceKey: string
  sourceName: string
  errorCode: string
  errorMessage: string
}

export interface NovelSearchPage {
  keyword?: string
  items: ItemDTO[]
  /** errorCode 非空的源。空 items 且不在这里，才代表「这个源确实没有这本书」。 */
  failures: NovelSearchFailure[]
  total: number
  failedCount: number
  page: number
  hasNext: boolean
  nextCursor: string
  pendingSources: number
  searchedSources: number
}

export function searchNovels(keyword: string, page = 1, cursor = '', onProgress?: (page:NovelSearchPage)=>void) {
  function map(outcome:SearchOutcomeDTO):NovelSearchPage {
    const results = (outcome.results ?? []).filter((result): result is NonNullable<typeof result> => !!result)
    const items: ItemDTO[] = []
    const failures: NovelSearchFailure[] = []
    for (const result of results) {
      for (const item of result.items ?? []) { if (item) items.push(item) }
      if (result.errorCode) {
        failures.push({
          sourceKey: result.sourceKey,
          sourceName: result.sourceName || result.sourceKey,
          errorCode: result.errorCode,
          errorMessage: result.errorMessage ?? '',
        })
        continue
      }
    }
    return { keyword:outcome.keyword || keyword, items:rankSearchItems(items,outcome.keyword || keyword), failures, total: outcome.total, failedCount: outcome.failedCount, page: outcome.page || page, hasNext: !!outcome.nextCursor, nextCursor: outcome.nextCursor || '',pendingSources:outcome.pendingSources||0,searchedSources:outcome.searchedSources??results.length }
  }
  return searchLibrary('novel',keyword,page,cursor,onProgress?outcome=>onProgress(map(outcome)):undefined).then(map)
}

/**
 * 一部书在界面上的样子：作品元信息 + 目录行 + 一句「有件事该说」。
 *
 * tocError 不阻断显示——目录取不到时库里那一份仍然要能翻，否则用户连
 * 该重试哪一步都看不到。漫画侧是同一个形状（comic/data/repository.ts）。
 */
export interface NovelBookView {
  item: ItemDTO
  /** 全部目录行，含卷标记（isVolume）。可阅读的行由视图自己筛。 */
  chapters: NovelChapter[]
  tocError: string
}

function viewOf(detail: ItemDetailDTO): NovelBookView | null {
  if (!detail.item) return null
  return { item: detail.item, chapters: detailChapters(detail), tocError: '' }
}

/**
 * 目录是空的就顺手去源站取一次。
 *
 * 搜索只把作品元数据落库（`app/library/service.go` 的 searchOne 不写 chapters），
 * 目录要 Detail+Toc 两步才有。少了这一步，从搜索点开一本书必须手动按「刷新目录」
 * 才能读到第一章——而界面上只会说「还没有章节」。
 *
 * 返回 null 表示这条作品已经不在库里，界面显示成「找不到这本书」而不是报错。
 */
export function loadNovelBook(itemId: number) {
 return readingRequest(async request => {
  const view = viewOf(await request(fetchLibraryItem(itemId, false)))
  if (!view) return null
  if (view.chapters.length || !view.item.available) return view
  try {
    return viewOf(await request(refreshLibraryToc(itemId))) ?? view
  } catch (error) {
    if (normalizeApiError(error).code === 'CANCELLED' || (error as Error)?.name === 'CancelError') throw error
    return { ...view, tocError: normalizeApiError(error).message || String(error) }
  }
 })
}

/** 用户明确按「刷新目录」时走这里：失败要抛出去，不能静默留着旧目录说成功。 */
export function refreshNovelToc(itemId: number) {
  return refreshLibraryToc(itemId).then(viewOf)
}

export function fetchNovelChapterText(chapterId: number) {
  return fetchChapterText(chapterId)
}

export function listNovelSources() {
  return listLibrarySources('novel')
}

/**
 * 已入库的全部作品（跨源），供书城/分类页浏览。
 *
 * 与书架是两件事：书架是用户主动保存的那一份，这里是所有搜到过、已落库的作品。
 * includeRetired 为真时连来源已下架的一起返回——它们仍然要显示成
 * 「来源已下架」，而不是从列表里凭空消失。
 */
export function listNovelItems(includeRetired = false) {
  return listLibraryItems('novel', includeRetired)
}

function detailChapters(detail: ItemDetailDTO): NovelChapter[] {
  return (detail.chapters ?? []).filter((chapter): chapter is NovelChapter => !!chapter)
}

export function splitGenres(genres: string): string[] {
  return String(genres ?? '').split(/[,，、;；/|\s]+/).map(part => part.trim()).filter(Boolean)
}

/** 源的 status 是自由文本；只认最保守的「完结」写法，认不出就当连载显示原文。 */
export function isCompletedStatus(status: string): boolean {
  const value = String(status ?? '').trim().toLowerCase()
  return !!value && (value.includes('完') || /^(completed?|finished|done|ended)$/.test(value))
}

export interface ShelfResolution {
  key: string
  itemId: number
  /** null 表示这一条已经从本地库里消失（换库/清库），条目标成不可用但不删除。 */
  item: ItemDTO | null
}

export function resolveShelfEntries(entries: readonly { key: string; itemId: number }[]) {
  return readingRequest(async request => {
    const refs = entries.map(entry => parseRefKey(entry.key))
    const valid = refs.filter((ref): ref is NonNullable<typeof ref> => ref !== null && ref.kind === 'novel')
    const found = await request(resolveLibraryRefs(valid))
    const indexed = new Map(valid.map((ref, index) => [`${ref.kind}\u0000${ref.sourceKey}\u0000${ref.remoteId}`, found[index]]))
    return entries.map(entry => ({ key: entry.key, itemId: indexed.get(entry.key)?.id ?? entry.itemId, item: indexed.get(entry.key) ?? null }))
  })
}
