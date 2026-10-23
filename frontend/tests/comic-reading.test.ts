import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
vi.mock('../bindings/cczjVideo/app/service/app', async original => ({ ...await original<typeof import('../bindings/cczjVideo/app/service/app')>(), ReadingStateLoad: vi.fn(async (_kind: string, raw: string) => JSON.parse(raw)), ReadingStateApply: vi.fn(async () => {}) }))
vi.mock('../src/platform/storage', async importOriginal => ({ ...await importOriginal<typeof import('../src/platform/storage')>(), readStorage: vi.fn() }))
vi.mock('../src/api/library', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../src/api/library')>()),
  searchLibrary: vi.fn(),
}))
import { ReadingStateApply } from '../bindings/cczjVideo/app/service/app'
import { readStorage } from '../src/platform/storage'
import { refKey, searchLibrary } from '../src/api/library'
import { useComicState } from '../src/comic/state'
import { searchComics } from '../src/comic/data/repository'
import { useComicPages } from '../src/comic/reader/useComicPages'
import { locateComicChapter, progressChapter } from '../src/comic/reader/progress'
import { isCompletedStatus, normalizeComicPreferences, splitGenres } from '../src/comic/domain'

const entry = { kind: 'comic' as const, sourceKey: 'src-a', remoteId: '/comic/1', itemId: 7, title: '作品A', cover: '/__cczj/img?u=x', addedAt: 5 }
const key = refKey(entry)
const item = { id: 7, kind: 'comic', sourceKey: 'src-a', sourceName: 'A源', remoteId: '/comic/1', title: '作品A', author: '', cover: '', summary: '', genres: '', status: '', latest: '', words: '', updatedAt: '', available: true }

beforeEach(() => {
  setActivePinia(createPinia())
  vi.mocked(readStorage).mockImplementation((_key, fallback) => fallback)
  vi.mocked(ReadingStateApply).mockResolvedValue(undefined)
})

it('imports v2 keys and keeps entries whose source can no longer be resolved', async () => {
  vi.mocked(readStorage).mockImplementation((storageKey, fallback) => storageKey.endsWith('follow.v2')
    ? [entry, entry, { ...entry, itemId: 0 }, { kind: 'comic', sourceKey: '', remoteId: 'x', itemId: 1 }, 'junk']
    : storageKey.endsWith('progress.v2')
      ? { [key]: { itemId: 7, title: '作品A', cover: '', episodeOrd: 3, episodeId: 99, episodeRemoteId: 'c3', page: 12, offset: 8, updatedAt: 7 }, 'broken': { itemId: 1, episodeOrd: 0, page: 0, updatedAt: 1 } }
      : fallback)
  const state = useComicState(); await state.init()
  expect(state.followed).toEqual([entry])
  expect(Object.keys(state.progress)).toEqual([key])
  expect(state.progress[key]).toMatchObject({ episodeOrd: 3, episodeId: 99, episodeRemoteId: 'c3', page: 12, offset: 1 })
  expect(state.recent[0].key).toBe(key)
  // 进度优先带稳定章节身份；没有进度时先回详情页，第一话的 ord 只有目录才知道。
  expect(state.resumePath({ itemId: 7, key })).toBe('/comic/read/7/3?episodeRemoteId=c3')
  expect(state.resumePath({ itemId: 7 })).toBe('/comic/series/7')
  expect(vi.mocked(readStorage).mock.calls.map(([storageKey]) => storageKey).filter(name => name.includes('.v1'))).toEqual([])
  expect(ReadingStateApply).not.toHaveBeenCalled()
})

it('resolves comic progress by remote chapter identity before mutable order', () => {
  const chapters = [
    { id: 1, remoteId: 'new', ord: 0 },
    { id: 2, remoteId: 'first', ord: 1 },
    { id: 3, remoteId: 'second', ord: 2 },
  ] as never[]
  const saved = { episodeOrd: 1, episodeRemoteId: 'second' } as never

  expect(progressChapter(chapters, saved)?.remoteId).toBe('second')
  expect(locateComicChapter(chapters, { remoteId: 'second', ord: 1 })).toMatchObject({ id: 3, ord: 2 })
  // A present-but-missing durable id must not silently fall through to a new chapter at the old ord.
  expect(locateComicChapter(chapters, { remoteId: 'deleted', ord: 1 })).toBeUndefined()
  expect(locateComicChapter(chapters, { ord: 1 })).toMatchObject({ remoteId: 'first' })
})

it('follows by composite key, bounds progress and preferences, and writes per identity', async () => {
  const state = useComicState(); await state.init()
  await state.toggle(item as never)
  expect(state.followed).toEqual([{ kind: 'comic', sourceKey: 'src-a', remoteId: '/comic/1', itemId: 7, title: '作品A', cover: '', addedAt: expect.any(Number) }])
  expect(state.isFollowed(key)).toBe(true)
  await state.toggle(item as never)
  expect(state.followed).toEqual([])
  await state.follow(item as never)
  await state.remember(item as never, { ord: 2, id: 55, remoteId: 'c2' }, 4, .25)
  expect(state.progress[key]).toMatchObject({ itemId: 7, episodeOrd: 2, episodeId: 55, episodeRemoteId: 'c2', page: 4, offset: .25 })
  await state.remember(item as never, { ord: 2, id: 55, remoteId: 'c2' }, -1, 0)
  await state.remember(item as never, { ord: 2, id: 55, remoteId: 'c2' }, 1, NaN)
  expect(state.progress[key].page).toBe(4)
  await state.setPreferences({ brightness: 9, flow: 'wrong' as never, direction: 'rtl' })
  expect(state.preferences).toEqual({ flow: 'strip', direction: 'rtl', brightness: 1, systemBrightness: true,wheelPaging:true,keyboardPaging:true,previousKey:'ArrowLeft',nextKey:'ArrowRight' })
  const keys = vi.mocked(ReadingStateApply).mock.calls.map(([kind]) => kind)
  expect(keys.length).toBeGreaterThan(0)
  expect(keys.every(name => name === 'comic')).toBe(true)
  vi.mocked(ReadingStateApply).mockRejectedValueOnce(new Error('disk full'))
  await state.unfollow(key)
  expect(state.error).toBe(true)
})

it('keeps a failed source visible in search results instead of collapsing it into "no matches"', async () => {
  vi.mocked(searchLibrary).mockResolvedValue({
    keyword: 'x', page: 1, total: 1, failedCount: 1, nextCursor: 'opaque-next-page',
    results: [
      { sourceKey: 'src-a', sourceName: 'A源', items: [item], nextUrl: '', hasNext: true },
      { sourceKey: 'src-b', sourceName: 'B源', items: [], nextUrl: '', hasNext: false, errorCode: 'TIMEOUT', errorMessage: '超时' },
    ],
  } as never)
  const result = await searchComics('x', 1)
  expect(result.items).toHaveLength(1)
  expect(result.noSources).toBe(false)
  expect(result.failedCount).toBe(1)
  expect(result.hasNext).toBe(true)
  expect(result.sources[0]).toMatchObject({ sourceKey: 'src-a', failed: false })
  expect(result.sources[1]).toMatchObject({ sourceKey: 'src-b', failed: true, errorCode: 'TIMEOUT', errorMessage: '超时' })
  // 一个源都没有与"搜了但没有"是两种处置，不能都显示成空结果。
  vi.mocked(searchLibrary).mockResolvedValue({ keyword: 'x', page: 1, total: 0, failedCount: 0, results: [] } as never)
  expect((await searchComics('x', 1)).noSources).toBe(true)
})

it('loads only the prefetch window, cancels back to queued and drops late callbacks across episodes', () => {
  const pager = useComicPages(2)
  pager.reset(10, 4)
  const generation = pager.generation.value
  expect(pager.current.value).toBe(4)
  expect(pager.pages.value.map(page => page.status)).toEqual(['queued', 'queued', 'loading', 'loading', 'loading', 'loading', 'loading', 'queued', 'queued', 'queued'])
  pager.succeed(4, generation, .6)
  expect(pager.pages.value[4]).toEqual({ status: 'ready', ratio: .6, nonce: 0 })
  // 翻页：滚出窗口的在途加载回到 queued（取消可重试），已就绪的页保持 ready，位图由卸载的 <img> 释放。
  pager.seek(8)
  expect(pager.pages.value[2].status).toBe('queued')
  expect(pager.pages.value[4].status).toBe('ready')
  expect(pager.pages.value.map((page, index) => page.status === 'loading' ? index : -1).filter(index => index >= 0)).toEqual([6, 7, 8, 9])
  // 切话：整表作废、代号加一，上一话迟到的响应写不进新话的页。
  pager.reset(3, 0)
  expect(pager.generation.value).toBe(generation + 1)
  expect(pager.pages.value).toHaveLength(3)
  pager.succeed(0, generation, 1)
  expect(pager.pages.value[0].status).toBe('loading')
  // 单页失败只重试那一页。
  pager.fail(1, pager.generation.value)
  expect(pager.pages.value[1].status).toBe('error')
  pager.retry(1)
  expect(pager.pages.value[1]).toMatchObject({ status: 'loading', nonce: 1 })
  expect(pager.clamp(99)).toBe(2)
})

it('treats genres and status as source-provided free text', () => {
  expect(splitGenres('冒险, 幻想，日常,冒险')).toEqual(['冒险', '幻想', '日常'])
  expect(splitGenres('')).toEqual([])
  expect(isCompletedStatus('已完结')).toBe(true)
  expect(isCompletedStatus('Completed')).toBe(true)
  expect(isCompletedStatus('连载中')).toBe(false)
  expect(isCompletedStatus('')).toBe(false)
  expect(normalizeComicPreferences(null)).toEqual({ flow: 'strip', direction: 'ltr', brightness: .8, systemBrightness: true,wheelPaging:true,keyboardPaging:true,previousKey:'ArrowLeft',nextKey:'ArrowRight' })
})
