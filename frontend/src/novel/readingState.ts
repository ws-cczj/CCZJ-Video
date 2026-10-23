import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { readStorage } from '../platform/storage'
import { parseRefKey, refKey, refOf } from '../api/library'
import type { ItemDTO } from '../api/library'
import { normalizeReadingPreferences, type NovelBook, type ReadingPreferences, type ReadingProgress } from './domain'

import { readingPersistence, readingRecord } from '../reading/persistence'

const SHELF_KEY = 'cczj.novel.shelf.v2'
const PROGRESS_KEY = 'cczj.novel.progress.v2'
const PREFERENCES_KEY = 'cczj.novel.reader.v2'

/**
 * 书架条目 = 自然键 + 本地主键 + 入库时的条目快照。
 *
 * 快照不是冗余：后端没有「按 ref 批量取回条目」的接口，而书架必须在离线、
 * 源被删、条目已下架时仍然显示书名和封面——凭空消失是最坏的处置。
 * 快照由书架页的再校验（resolveShelfEntries）负责刷新。
 */
export interface ShelfEntry {
  key: string
  itemId: number
  addedAt: number
  available: boolean
  item: NovelBook
}

function sanitizeItem(raw: unknown): NovelBook | null {
  if (!raw || typeof raw !== 'object') return null
  const value = raw as Partial<ItemDTO>
  if (typeof value.id !== 'number' || !Number.isInteger(value.id) || value.id <= 0) return null
  if (typeof value.title !== 'string' || typeof value.sourceKey !== 'string' || typeof value.remoteId !== 'string') return null
  return {
    id: value.id,
    kind: value.kind || 'novel',
    sourceKey: value.sourceKey,
    sourceName: typeof value.sourceName === 'string' ? value.sourceName : '',
    remoteId: value.remoteId,
    title: value.title,
    author: typeof value.author === 'string' ? value.author : '',
    cover: typeof value.cover === 'string' ? value.cover : '',
    summary: typeof value.summary === 'string' ? value.summary : '',
    genres: typeof value.genres === 'string' ? value.genres : '',
    status: typeof value.status === 'string' ? value.status : '',
    latest: typeof value.latest === 'string' ? value.latest : '',
    words: typeof value.words === 'string' ? value.words : '',
    updatedAt: typeof value.updatedAt === 'string' ? value.updatedAt : '',
    available: value.available !== false,
  }
}

function parseShelf(raw: unknown): ShelfEntry[] {
  if (!Array.isArray(raw)) return []
  const seen = new Set<string>()
  const entries: ShelfEntry[] = []
  for (const value of raw) {
    if (!value || typeof value !== 'object') continue
    const candidate = value as Partial<ShelfEntry>
    const item = sanitizeItem(candidate.item)
    if (!item || item.kind !== 'novel' || typeof candidate.key !== 'string' || candidate.key !== refKey(refOf(item)) || seen.has(candidate.key)) continue
    seen.add(candidate.key)
    // 不校验来源有效性：源没了也要留着条目，标成不可用，让用户自己决定去留。
    entries.push({
      key: candidate.key,
      itemId: item.id,
      addedAt: typeof candidate.addedAt === 'number' && Number.isFinite(candidate.addedAt) ? candidate.addedAt : Date.now(),
      available: candidate.available !== false && item.available !== false,
      item,
    })
  }
  return entries
}

function parseProgress(raw: unknown): Record<string, ReadingProgress> {
  const result: Record<string, ReadingProgress> = {}
  if (!raw || typeof raw !== 'object') return result
  for (const [key, value] of Object.entries(raw)) {
    if (parseRefKey(key)?.kind !== 'novel' || !value || typeof value !== 'object') continue
    const p = value as Partial<ReadingProgress>
    if (typeof p.itemId !== 'number' || !Number.isInteger(p.itemId) || p.itemId <= 0) continue
    if (typeof p.chapterId !== 'number' || !Number.isInteger(p.chapterId) || p.chapterId < 0) continue
    if (typeof p.chapterOrd !== 'number' || !Number.isInteger(p.chapterOrd) || p.chapterOrd < 0) continue
    if (typeof p.ratio !== 'number' || !Number.isFinite(p.ratio)) continue
    if (typeof p.updatedAt !== 'number' || !Number.isFinite(p.updatedAt)) continue
    result[key] = {
      itemId: p.itemId,
      title: typeof p.title === 'string' ? p.title : '',
      chapterOrd: p.chapterOrd,
      chapterId: p.chapterId,
      chapterRemoteId: typeof p.chapterRemoteId === 'string' ? p.chapterRemoteId : '',
      ratio: Math.max(0, Math.min(1, p.ratio)),
      updatedAt: p.updatedAt,
    }
  }
  return result
}

export const useNovelReading = defineStore('novelReading', () => {
  const shelf = ref<ShelfEntry[]>(parseShelf(readStorage<unknown>(SHELF_KEY, [])))
  const progress = ref<Record<string, ReadingProgress>>(parseProgress(readStorage<unknown>(PROGRESS_KEY, {})))
  const preferences = ref(normalizeReadingPreferences(readStorage<unknown>(PREFERENCES_KEY, {})))
  const storageError = ref(false)
  const shelfKeys = computed(() => new Set(shelf.value.map(entry => entry.key)))
  function inShelf(key: string): boolean { return shelfKeys.value.has(key) }
  const lastRead = computed(() => {
    const entries = Object.entries(progress.value).sort((a, b) => b[1].updatedAt - a[1].updatedAt)
    return entries.length ? { key: entries[0][0], progress: entries[0][1] } : null
  })
  const ready = ref(false)
  const persistence = readingPersistence({
    kind: 'novel',
    legacy: () => [
      ...shelf.value.map(entry => readingRecord('shelf', entry.key, entry)),
      ...Object.entries(progress.value).map(([key, value]) => readingRecord('progress', key, value)),
      readingRecord('preferences', 'default', preferences.value),
    ],
    apply: records => {
      shelf.value = parseShelf(records.filter(r => r.bucket === 'shelf').map(r => JSON.parse(r.value))).sort((a, b) => b.addedAt - a.addedAt)
      progress.value = parseProgress(Object.fromEntries(records.filter(r => r.bucket === 'progress').map(r => [r.key, JSON.parse(r.value)])))
      preferences.value = normalizeReadingPreferences(JSON.parse(records.find(r => r.bucket === 'preferences')?.value ?? '{}'))
    },
    status: (loaded, failed) => { ready.value = loaded; storageError.value = failed },
  })
  async function toggleShelf(item: ItemDTO): Promise<boolean> {
    if (!await persistence.init()) return false
    const clean = sanitizeItem(item)
    if (!clean || clean.kind !== 'novel') return false
    const key = refKey(refOf(clean))
    if (shelf.value.some(entry => entry.key === key)) return removeShelf(key)
    const entry = { key, itemId: item.id, addedAt: Date.now(), available: item.available !== false, item: clean }
    shelf.value = [entry, ...shelf.value]
    persistence.enqueue(readingRecord('shelf', key, entry))
    return persistence.flush()
  }
  async function removeShelf(key: string): Promise<boolean> {
    if (!await persistence.init()) return false
    if (!shelf.value.some(entry => entry.key === key)) return true
    shelf.value = shelf.value.filter(entry => entry.key !== key)
    persistence.enqueue({ bucket: 'shelf', key, value: '', delete: true })
    return persistence.flush()
  }
  async function updateShelfEntry(key: string, patch: { item?: ItemDTO; available?: boolean }): Promise<boolean> {
    if (!await persistence.init()) return false
    const entry = shelf.value.find(candidate => candidate.key === key)
    if (!entry) return false
    if (patch.item) {
      const item = sanitizeItem(patch.item)
      if (item && refKey(refOf(item)) === key) { entry.item = item; entry.itemId = item.id }
    }
    if (patch.available !== undefined) entry.available = patch.available
    persistence.enqueue(readingRecord('shelf', key, entry))
    return persistence.flush()
  }
  async function remember(key: string, value: { itemId: number; title: string; chapterOrd: number; chapterId: number; chapterRemoteId?: string; ratio: number }): Promise<boolean> {
    if (!await persistence.init()) return false
    if (parseRefKey(key)?.kind !== 'novel' || !Number.isFinite(value.ratio) || !Number.isInteger(value.itemId) || value.itemId <= 0 || !Number.isInteger(value.chapterId) || value.chapterId < 0 || !Number.isInteger(value.chapterOrd) || value.chapterOrd < 0) return false
    const record = { ...value, chapterRemoteId: value.chapterRemoteId ?? '', ratio: Math.max(0, Math.min(1, value.ratio)), updatedAt: Date.now() }
    progress.value[key] = record
    persistence.enqueue(readingRecord('progress', key, record))
    return persistence.flush()
  }
  async function setPreferences(value: Partial<ReadingPreferences>): Promise<boolean> {
    if (!await persistence.init()) return false
    preferences.value = normalizeReadingPreferences({ ...preferences.value, ...value })
    persistence.enqueue(readingRecord('preferences', 'default', preferences.value))
    return persistence.flush()
  }
  /** 没有进度时落到 `first`，由阅读页取目录里的第一个可用章。 */
  function readingPath(key: string): string | null {
    const record = progress.value[key]
    const itemId = record?.itemId ?? shelf.value.find(entry => entry.key === key)?.itemId
    if (itemId === undefined) return null
    return `/novel/read/${itemId}/${record ? record.chapterId : 'first'}`
  }
  return { ready, init: persistence.init, retry: persistence.retry, flush: persistence.flush, shelf, progress, preferences, storageError, shelfKeys, inShelf, lastRead, toggleShelf, removeShelf, updateShelfEntry, remember, setPreferences, readingPath }
})
