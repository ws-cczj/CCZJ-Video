import { describe, expect, it, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { createMemoryHistory, createRouter } from 'vue-router'
import { leaveReader } from '../src/reading/navigation'

describe.each(['novel', 'comic'] as const)('%s reading exit', mode => {
  function setup(previous: string | null) {
    const history = createMemoryHistory()
    const router = createRouter({ history, routes: [] })
    Object.defineProperty(history, 'state', { get: () => ({ back: previous }) })
    const back = vi.spyOn(router, 'back').mockImplementation(() => {})
    const replace = vi.spyOn(router, 'replace').mockResolvedValue()
    const push = vi.spyOn(router, 'push')
    return { router, back, replace, push }
  }
  it.each([`/${mode}`, `/${mode}/search?q=book&page=2`, `/${mode}/${mode === 'novel' ? 'book' : 'series'}/7`])('returns to entry %s without adding a history entry', entry => {
    const s = setup(entry)
    leaveReader(s.router, mode, `/${mode}`)
    expect(s.back).toHaveBeenCalledOnce()
    expect(s.replace).not.toHaveBeenCalled()
    expect(s.push).not.toHaveBeenCalled()
  })
  it.each([null, '/video', '/novelty', '/comic-other', 'https://example.com', `/${mode}/read/7/2`])('replaces unsafe or absent entry %s with fallback', entry => {
    const s = setup(entry)
    leaveReader(s.router, mode, `/${mode}`)
    expect(s.back).not.toHaveBeenCalled()
    expect(s.replace).toHaveBeenCalledWith(`/${mode}`)
    expect(s.push).not.toHaveBeenCalled()
  })
})

it('uses history-aware exit actions for terminal reader states', () => {
  const novel = readFileSync(new URL('../src/novel/views/NovelReader.vue', import.meta.url), 'utf8')
  const comic = readFileSync(new URL('../src/comic/views/ComicReader.vue', import.meta.url), 'utf8')

  expect(novel).not.toContain('<router-link v-else :to="`/novel/book/${book.id}`"')
  expect(novel).not.toContain('<router-link class="novel-button" :to="`/novel/book/${itemId}`"')
  expect(comic).not.toContain('<router-link v-else :to="`/comic/series/${itemId}`">')
  expect(comic).not.toContain('<template #actions><router-link :to="`/comic/series/${itemId}`"')
})
