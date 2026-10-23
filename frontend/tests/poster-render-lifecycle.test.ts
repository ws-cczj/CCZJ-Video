import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { effect, stop } from 'vue'
import { usePosterCacheStore } from '../src/stores/posterCache'
vi.mock('../src/api/app', () => ({ GetVideoDetail: vi.fn() }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))
vi.mock('../src/platform/storage', async importOriginal => ({ ...await importOriginal<typeof import('../src/platform/storage')>(), readStorage: (_key: string, fallback: unknown) => fallback, removeStorage: vi.fn() }))
vi.mock('../src/platform/debouncedWrite', () => ({ createDebouncedWriter: () => ({ schedule: vi.fn(), cancel: vi.fn(), flush: vi.fn() }) }))
beforeEach(() => { setActivePinia(createPinia()); let now = 100000; vi.spyOn(Date, 'now').mockImplementation(() => ++now) })
afterEach(() => vi.restoreAllMocks())
it('poster reads refresh retention metadata without invalidating views; content changes still invalidate', () => {
  const cache = usePosterCacheStore()
  cache.set('source', '1', { vod_name: 'Video', vod_pic: 'old.jpg' })
  const invalidate = vi.fn()
  const render = effect(() => cache.get('source', '1')?.vod_pic, { scheduler: invalidate })
  try {
    const before = cache.cache['source:1'].last_accessed
    cache.get('source', '1')
    expect(cache.cache['source:1'].last_accessed).toBeGreaterThan(before)
    expect(invalidate).not.toHaveBeenCalled()
    cache.set('source', '1', { vod_pic: 'new.jpg' })
    expect(invalidate).toHaveBeenCalledOnce()
  } finally { stop(render) }
})
