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

it('does not invent a percentage for history without duration',async()=>{
 const progress=await import('../src/utils/episodeProgress')
 expect(progress.getEpProgressPct({position:120,updatedAt:Date.now()})).toBe(0)
 expect(progress.getEpProgressPct({position:0,duration:100,updatedAt:Date.now()})).toBe(0)
 expect(progress.getEpProgressPct({position:99,duration:100,updatedAt:Date.now()})).toBe(99)
})
it('rejects an incompatible old duration and preserves a newer local position',async()=>{
 const progress=await import('../src/utils/episodeProgress')
 progress.saveEpProgress('real-1',20,40)
 progress.mergeEpHistory('real-1',{position:2790,updated_at:new Date(Date.now()+1000).toISOString()})
 expect(progress.loadEpProgress()['real-1'].position).toBe(2790)
 expect(progress.loadEpProgress()['real-1'].duration).toBeUndefined()
 progress.mergeEpHistory('real-1',{position:7,duration:2830,updated_at:new Date(Date.now()-1000).toISOString()})
 expect(progress.loadEpProgress()['real-1'].position).toBe(2790)
 expect(progress.loadEpProgress()['real-1'].duration).toBe(2830)
})

it('compares SQLite UTC timestamps against local checkpoints without a timezone shift',async()=>{
 const progress=await import('../src/utils/episodeProgress')
 const {parseTimestamp}=await import('../src/utils/time')
 const stamp='2026-10-26 12:30:10.125'
 expect(parseTimestamp(stamp)).toBe(Date.parse('2026-10-26T12:30:10.125Z'))
 expect(parseTimestamp('2026-10-26T20:30:10.125+08:00')).toBe(parseTimestamp(stamp))
 vi.setSystemTime(Date.parse('2026-10-26T12:30:09Z'))
 progress.saveEpProgress('utc-1',7,100)
 progress.mergeEpHistory('utc-1',{position:99,duration:100,updated_at:stamp})
 expect(progress.loadEpProgress()['utc-1'].position).toBe(99)
})
