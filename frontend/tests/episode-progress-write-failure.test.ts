import { afterEach, beforeEach, expect, it, vi } from 'vitest'

const storage = vi.hoisted(() => ({ writes: [] as unknown[], succeeds: false, input: undefined as unknown }))
vi.mock('../src/platform/storage', () => ({
  readStorage: (_key: string, fallback: unknown) => storage.input === undefined ? fallback : storage.input,
  writeStorage: (_key: string, value: unknown) => {
    storage.writes.push(value)
    return storage.succeeds
  },
  removeStorage: vi.fn(),
}))

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers()
  storage.writes.length = 0
  storage.succeeds = false
  storage.input = undefined
})

afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

it('retries unsaved episode progress after a storage write failure', async () => {
  const progress = await import('../src/utils/episodeProgress')
  progress.saveEpProgress('work-1', 120, 600)
  progress.flushEpProgress()
  expect(storage.writes).toHaveLength(1)
  storage.succeeds = true
  progress.flushEpProgress()
  expect(storage.writes).toHaveLength(2)
  expect((storage.writes[1] as Record<string, { position: number }>)['work-1'].position).toBe(120)
  vi.clearAllTimers()
  vi.useRealTimers()
})

it.each(['broken root', 123, [], null])('recovers malformed progress storage: %j', async (input) => {
  storage.input = input
  const progress = await import('../src/utils/episodeProgress')
  expect(progress.loadEpProgress()).toEqual({})
  progress.saveEpProgress('new-1', 12, 120)
  expect(progress.getEpProgress('new-1')?.position).toBe(12)
})

it('keeps valid rows and rejects invalid positions, durations and timestamps', async () => {
  const now = Date.now()
  storage.input = { good: { position: 40, duration: 100, updatedAt: now }, badPosition: { position: '40', updatedAt: now }, badDuration: { position: 1, duration: -2, updatedAt: now }, badTime: { position: 1, updatedAt: 'today' }, expired: { position: 1, updatedAt: now - 31 * 86400_000 } }
  const progress = await import('../src/utils/episodeProgress')
  expect(Object.keys(progress.loadEpProgress())).toEqual(['good'])
  for (const position of [NaN, Infinity, -1]) progress.saveEpProgress('invalid', position)
  expect(progress.getEpProgress('invalid')).toBeUndefined()
  progress.saveEpProgress('good', 50, Infinity)
  expect(progress.getEpProgress('good')?.duration).toBe(100)
})
