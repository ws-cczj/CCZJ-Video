import { computed, ref } from 'vue'
import { defineStore } from 'pinia'
import { parseRefKey, refKey, refOf } from '../api/library'
import { readStorage } from '../platform/storage'
import { normalizeComicPreferences, type ComicPreferences, type ComicProgress, type ComicRecentEntry, type ComicSeries, type ComicShelfEntry } from './domain'

import { readingPersistence, readingRecord } from '../reading/persistence'

// v2：键从作品 id 换成 {kind, sourceKey, remoteId} 复合键（ADR 0016）。
// 旧的 v1 键不再读——里面只有已下线的演示作品。
const FOLLOW_KEY = 'cczj.comic.follow.v2'
const PROGRESS_KEY = 'cczj.comic.progress.v2'
const PREFERENCES_KEY = 'cczj.comic.preferences.v2'

function readFollowed(): ComicShelfEntry[] {
  const raw = readStorage<unknown>(FOLLOW_KEY, [])
  if (!Array.isArray(raw)) return []
  const seen = new Set<string>()
  const entries: ComicShelfEntry[] = []
  for (const value of raw) {
    if (!value || typeof value !== 'object') continue
    const candidate = value as Partial<ComicShelfEntry>
    const kind = candidate.kind === 'comic' ? 'comic' : candidate.kind === 'novel' ? 'novel' : null
    const itemId = Number(candidate.itemId)
    if (kind !== 'comic' || !candidate.sourceKey || !candidate.remoteId || !Number.isInteger(itemId) || itemId <= 0) continue
    const entry: ComicShelfEntry = {
      kind,
      sourceKey: String(candidate.sourceKey),
      remoteId: String(candidate.remoteId),
      itemId,
      title: String(candidate.title ?? ''),
      cover: String(candidate.cover ?? ''),
      addedAt: Number.isFinite(Number(candidate.addedAt)) ? Number(candidate.addedAt) : 0,
    }
    const key = refKey(entry)
    if (seen.has(key)) continue
    seen.add(key)
    entries.push(entry)
  }
  return entries
}

function readProgress(): Record<string, ComicProgress> {
  const raw = readStorage<unknown>(PROGRESS_KEY, {})
  const table: Record<string, ComicProgress> = {}
  if (!raw || typeof raw !== 'object') return table
  for (const [key, value] of Object.entries(raw)) {
    // 只丢弃形状坏掉的记录。源没了、作品下架了都留着，界面标"来源已下架"，
    // 由用户自己决定要不要移走——凭空消失会让人以为数据丢了。
    if (!value || typeof value !== 'object' || parseRefKey(key)?.kind !== 'comic') continue
    const entry = value as Partial<ComicProgress>
    const itemId = Number(entry.itemId), ord = Number(entry.episodeOrd), page = Number(entry.page), updatedAt = Number(entry.updatedAt)
    if (!Number.isInteger(itemId) || itemId <= 0 || !Number.isInteger(ord) || ord < 0 || !Number.isInteger(page) || page < 0 || !Number.isFinite(updatedAt)) continue
    const offset = Number(entry.offset)
    table[key] = {
      itemId,
      title: String(entry.title ?? ''),
      cover: String(entry.cover ?? ''),
      episodeOrd: ord,
      episodeId: Number(entry.episodeId) || 0,
      episodeRemoteId: String(entry.episodeRemoteId ?? ''),
      page,
      offset: Number.isFinite(offset) ? Math.max(0, Math.min(1, offset)) : 0,
      updatedAt,
    }
  }
  return table
}

export const useComicState = defineStore('comicReading', () => {
  const followed = ref<ComicShelfEntry[]>(readFollowed())
  const progress = ref<Record<string, ComicProgress>>(readProgress())
  const preferences = ref(normalizeComicPreferences(readStorage(PREFERENCES_KEY, {})))
  const error = ref(false)

  const followIndex = computed(() => new Map(followed.value.map(entry => [refKey(entry), entry])))
  const recent = computed<ComicRecentEntry[]>(() => Object.entries(progress.value)
    .sort((a, b) => b[1].updatedAt - a[1].updatedAt)
    .map(([key, value]) => ({ key, ...value })))

  const ready = ref(false)
  const persistence = readingPersistence({
    kind: 'comic',
    legacy: () => [
      ...followed.value.map(entry => readingRecord('shelf', refKey(entry), entry)),
      ...Object.entries(progress.value).map(([key, value]) => readingRecord('progress', key, value)),
      readingRecord('preferences', 'default', preferences.value),
    ],
    apply: records => {
      followed.value = records.filter(r => r.bucket === 'shelf').map(r => JSON.parse(r.value) as ComicShelfEntry).sort((a, b) => a.addedAt - b.addedAt)
      progress.value = Object.fromEntries(records.filter(r => r.bucket === 'progress').map(r => [r.key, JSON.parse(r.value) as ComicProgress]))
      preferences.value = normalizeComicPreferences(JSON.parse(records.find(r => r.bucket === 'preferences')?.value ?? '{}'))
    },
    status: (loaded, failed) => { ready.value = loaded; error.value = failed },
  })
  function keyOf(item: ComicSeries): string { return refKey(refOf(item)) }
  function isFollowed(key: string): boolean { return followIndex.value.has(key) }

  async function follow(item: ComicSeries): Promise<boolean> {
    if (!await persistence.init()) return false
    const key = keyOf(item)
    if (!item.id || item.kind !== 'comic' || !item.sourceKey || !item.remoteId || isFollowed(key)) return false
    followed.value = [...followed.value, { ...refOf(item), itemId: item.id, title: item.title, cover: item.cover, addedAt: Date.now() }]
    persistence.enqueue(readingRecord('shelf', key, followed.value[followed.value.length - 1]))
    return persistence.flush()
  }

  async function unfollow(key: string): Promise<boolean> {
    if (!await persistence.init()) return false
    if (!isFollowed(key)) return true
    followed.value = followed.value.filter(entry => refKey(entry) !== key)
    persistence.enqueue({ bucket: 'shelf', key, value: '', delete: true })
    return persistence.flush()
  }

  async function toggle(item: ComicSeries): Promise<boolean> {
    if (!await persistence.init()) return false
    const key = keyOf(item)
    return isFollowed(key) ? unfollow(key) : follow(item)
  }

  async function remember(item: ComicSeries, episode: { ord: number; id: number; remoteId: string }, page: number, offset: number): Promise<boolean> {
    if (!await persistence.init()) return false
    if (!item.id || item.kind !== 'comic' || !item.sourceKey || !item.remoteId) return false
    if (!Number.isInteger(episode.ord) || episode.ord < 0 || !Number.isInteger(page) || page < 0 || !Number.isFinite(offset) || !Number.isInteger(episode.id) || episode.id < 0) return false
    const key = keyOf(item)
    progress.value = {
      ...progress.value,
      [key]: {
        itemId: item.id,
        title: item.title,
        cover: item.cover,
        episodeOrd: episode.ord,
        episodeId: episode.id,
        episodeRemoteId: episode.remoteId,
        page,
        offset: Math.max(0, Math.min(1, offset)),
        updatedAt: Date.now(),
      },
    }
    persistence.enqueue(readingRecord('progress', key, progress.value[key]))
    return persistence.flush()
  }

  async function setPreferences(value: Partial<ComicPreferences>): Promise<boolean> {
    if (!await persistence.init()) return false
    preferences.value = normalizeComicPreferences({ ...preferences.value, ...value })
    persistence.enqueue(readingRecord('preferences', 'default', preferences.value))
    return persistence.flush()
  }

  function seriesPath(itemId: number): string { return `/comic/series/${itemId}` }
  /**
   * Keep the old order segment for deep-link compatibility, but carry the
   * source chapter identity for new links. A source may insert or reorder
   * chapters without changing that identity.
   */
  function readPath(itemId: number, ord: number, episodeRemoteId = ''): string {
    const query = episodeRemoteId ? `?episodeRemoteId=${encodeURIComponent(episodeRemoteId)}` : ''
    return `/comic/read/${itemId}/${ord}${query}`
  }
  /** 没有进度的条目先进详情页：第一话的 ord 只有目录加载完才知道。 */
  function resumePath(entry: { itemId: number; key?: string }): string {
    const saved = entry.key ? progress.value[entry.key] : undefined
    return saved ? readPath(entry.itemId, saved.episodeOrd, saved.episodeRemoteId) : seriesPath(entry.itemId)
  }

  return { ready, init: persistence.init, retry: persistence.retry, flush: persistence.flush, followed, followIndex, progress, recent, preferences, error, keyOf, isFollowed, follow, unfollow, toggle, remember, setPreferences, seriesPath, readPath, resumePath }
})
