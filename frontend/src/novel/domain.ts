import {normalizeReaderKey} from '../reading/inputPreferences'
export {normalizeReaderKey} from '../reading/inputPreferences'
import type { ChapterTextDTO, ItemDetailDTO, ItemDTO } from '../api/library'

export type NovelBook = ItemDTO
export type NovelChapter = NonNullable<ItemDetailDTO['chapters'][number]>
export type NovelChapterText = ChapterTextDTO
export type ReadingTheme = 'paper' | 'white' | 'night'

export interface ReadingProgress {
  /** 本地 items 主键：取详情与拼阅读路由用。换库后会失效，届时条目仍在书架上标成不可用。 */
  itemId: number
  title: string
  /** 展示序，用于快速恢复与「第 N 章」显示。 */
  chapterOrd: number
  /** 本地 chapters 主键，取正文用它。 */
  chapterId: number
  /** 来源内的稳定章节身份，用于本地章节 ID 变化后的进度恢复。 */
  chapterRemoteId: string
  ratio: number
  updatedAt: number
}

export interface ReadingPreferences { wheelPaging:boolean; keyboardPaging:boolean; previousKey:string; nextKey:string; size: number; theme: ReadingTheme; serif: boolean; flow: 'paged' | 'scroll'; leading: number; margin: number; brightness: number; systemBrightness: boolean }
export const DEFAULT_READING_PREFERENCES: ReadingPreferences = { wheelPaging:true,keyboardPaging:true,previousKey:'ArrowLeft',nextKey:'ArrowRight',size: 20, theme: 'paper', serif: true, flow: 'scroll', leading: 1.85, margin: 24, brightness: .8, systemBrightness: true }

export function normalizeReadingPreferences(raw: unknown): ReadingPreferences {
  const value = raw && typeof raw === 'object' ? raw as Partial<ReadingPreferences> : {}
  const previousKey=normalizeReaderKey(value.previousKey,'ArrowLeft')
  const nextKey=normalizeReaderKey(value.nextKey,'ArrowRight')
  return { wheelPaging:typeof value.wheelPaging==='boolean'?value.wheelPaging:true,keyboardPaging:typeof value.keyboardPaging==='boolean'?value.keyboardPaging:true,previousKey,nextKey:nextKey===previousKey?(previousKey==='ArrowRight'?'ArrowLeft':'ArrowRight'):nextKey,size: typeof value.size === 'number' && Number.isFinite(value.size) ? Math.max(16,Math.min(28,Math.round(value.size))) : 20,
    theme: ['paper','white','night'].includes(String(value.theme)) ? value.theme as ReadingTheme : 'paper', serif: typeof value.serif === 'boolean' ? value.serif : true,
    flow: value.flow === 'paged' ? 'paged' : 'scroll', leading: [1.6,1.85,2.1].includes(Number(value.leading)) ? Number(value.leading) : 1.85,
    margin: [16,24,32].includes(Number(value.margin)) ? Number(value.margin) : 24,
    brightness: typeof value.brightness === 'number' && Number.isFinite(value.brightness) ? Math.max(.2,Math.min(1,value.brightness)) : .8,
    systemBrightness: typeof value.systemBrightness === 'boolean' ? value.systemBrightness : true }
}


