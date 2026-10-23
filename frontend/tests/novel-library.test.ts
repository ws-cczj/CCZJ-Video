import { beforeEach, expect, it, vi } from 'vitest'

vi.mock('../src/api/library', async (importOriginal) => ({
  ...(await importOriginal<typeof import('../src/api/library')>()),
  fetchLibraryItem: vi.fn(),
  refreshLibraryToc: vi.fn(),
}))

import { fetchLibraryItem, refreshLibraryToc } from '../src/api/library'
import { loadNovelBook, refreshNovelToc } from '../src/novel/data/novelLibrary'

const item = {
  id: 3, kind: 'novel', sourceKey: 'src-a', sourceName: 'A源', remoteId: '/book/3',
  title: '远山', author: '', cover: '', summary: '', genres: '', status: '', latest: '',
  words: '', updatedAt: '', available: true,
}
const chapter = { id: 11, ord: 0, title: '第一章', volume: '', isVolume: false, access: 'free', available: true }
const volume = { id: 12, ord: 1, title: '第一卷', volume: '第一卷', isVolume: true, access: 'free', available: true }

function detail(chapters: unknown[], overrides: Record<string, unknown> = {}) {
  return { item, chapters, chapterCount: chapters.length, ...overrides } as never
}

beforeEach(() => {
  vi.mocked(fetchLibraryItem).mockReset()
  vi.mocked(refreshLibraryToc).mockReset()
})

it('pulls the contents on first open because a search only stored the item', async () => {
  vi.mocked(fetchLibraryItem).mockResolvedValue(detail([]))
  vi.mocked(refreshLibraryToc).mockResolvedValue(detail([chapter]))
  const view = await loadNovelBook(3)
  expect(refreshLibraryToc).toHaveBeenCalledTimes(1)
  expect(view?.chapters.map(row => row.id)).toEqual([11])
  expect(view?.tocError).toBe('')
})

it('does not touch the source when the stored contents are already there', async () => {
  vi.mocked(fetchLibraryItem).mockResolvedValue(detail([chapter, volume]))
  const view = await loadNovelBook(3)
  expect(refreshLibraryToc).not.toHaveBeenCalled()
  expect(view?.chapters).toHaveLength(2)
})

it('leaves a book whose source is gone alone instead of retrying it', async () => {
  vi.mocked(fetchLibraryItem).mockResolvedValue(detail([], { item: { ...item, available: false } }))
  await loadNovelBook(3)
  expect(refreshLibraryToc).not.toHaveBeenCalled()
})

it('keeps showing what it has and reports an automatic refresh failure', async () => {
  vi.mocked(fetchLibraryItem).mockResolvedValue(detail([]))
  vi.mocked(refreshLibraryToc).mockRejectedValue(new Error('UNAVAILABLE: 源站没有响应'))
  const view = await loadNovelBook(3)
  expect(view?.item.title).toBe('远山')
  expect(view?.tocError).toContain('源站没有响应')
})

it('propagates the failure when the user pressed refresh', async () => {
  vi.mocked(refreshLibraryToc).mockRejectedValue(new Error('CORRUPT: 目录规则取不到行'))
  await expect(refreshNovelToc(3)).rejects.toThrow('目录规则取不到行')
})

it('reports a book that left the library as missing, not as an error', async () => {
  vi.mocked(fetchLibraryItem).mockResolvedValue({ item: null, chapters: [], chapterCount: 0 } as never)
  await expect(loadNovelBook(3)).resolves.toBeNull()
})
