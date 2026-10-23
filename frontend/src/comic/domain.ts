import {normalizeReadingInput,type ReadingInputPreferences} from '../reading/inputPreferences'
/**
 * 漫画模式的数据形状直接取后端 DTO：字段名以 bindings/cczjVideo/app/library/models.js
 * 为准，前端不再另立一套模型（ADR 0016）。演示用的 scene/color/tagline 已随
 * demoRepository 一起下线。
 */
import type { ChapterDTO, ChapterPagesDTO, ItemDTO, ItemDetailDTO, LibraryRef } from '../api/library'

export type ComicSeries = ItemDTO
export type ComicSeriesDetail = ItemDetailDTO
export type ComicChapter = ChapterDTO
export type ComicPage = NonNullable<ChapterPagesDTO['pages'][number]>

/**
 * 页级状态。ADR 0016 采纳 Mihon 的 Queue → LoadPage → DownloadImage → Ready，
 * 前端只负责最后那一步（页表由后端给），所以收成四态。
 * 取消（滚出预取窗口）回到 queued 而不是 error：取消是可重试的，不是失败。
 */
export type ComicPageStatus = 'queued' | 'loading' | 'ready' | 'error'

/** ratio 是图片的宽高比，量到一次就记住：位图释放之后格子的高度不能跟着塌。 */
export interface ComicPageState { status: ComicPageStatus; ratio: number; nonce: number }

/**
 * 追漫条目。身份是自然键 {kind, sourceKey, remoteId}（ADR 0016：本地 id 换库就失效，
 * 标题跨源会撞），itemId 是取详情用的本地句柄，title/cover 是快照——
 * 源被删了或库被换了，列表仍然要能显示出这是哪一部，而不是凭空少一行。
 */
export interface ComicShelfEntry extends LibraryRef {
  itemId: number
  title: string
  cover: string
  addedAt: number
}

export interface ComicProgress {
  itemId: number
  title: string
  cover: string
  /** 展示序：恢复位置与显示"第 N 话"用它。 */
  episodeOrd: number
  /** 本地主键：取图片列表用它。 */
  episodeId: number
  /** 稳定身份：源改版后靠它对上同一话。 */
  episodeRemoteId: string
  page: number
  offset: number
  updatedAt: number
}

export interface ComicRecentEntry extends ComicProgress { key: string }

export interface ComicPreferences extends ReadingInputPreferences { flow: 'strip' | 'pages'; direction: 'ltr' | 'rtl'; brightness: number; systemBrightness: boolean }

export function normalizeComicPreferences(raw: unknown): ComicPreferences {
  const p = raw && typeof raw === 'object' ? raw as Partial<ComicPreferences> : {}
  return { ...normalizeReadingInput(p), flow: p.flow === 'pages' ? 'pages' : 'strip', direction: p.direction === 'rtl' ? 'rtl' : 'ltr', brightness: typeof p.brightness === 'number' && Number.isFinite(p.brightness) ? Math.max(.2, Math.min(1, p.brightness)) : .8, systemBrightness: typeof p.systemBrightness === 'boolean' ? p.systemBrightness : true }
}

/** genres 是源给的自由文本，不再是固定枚举；空的与重复的标签不要。 */
export function splitGenres(raw: string): string[] {
  return [...new Set(String(raw ?? '').split(/[,，]/).map(part => part.trim()).filter(Boolean))]
}

/** status 也是源给的自由文本。认不出"完结"这一类词时按连载中显示。 */
export function isCompletedStatus(status: string): boolean {
  return /完结|完結|已完|完成|ended|complete|finished/i.test(String(status ?? '').trim())
}
