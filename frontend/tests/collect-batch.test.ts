import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

const calls = vi.hoisted(() => ({ single: vi.fn(), batch: vi.fn() }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))
vi.mock('../src/api/events', () => ({ onBackendEvent: vi.fn() }))
vi.mock('../src/api/app', async original => ({
  ...await original<typeof import('../src/api/app')>(),
  GetCollectStatus: calls.single,
  GetCollectStatuses: calls.batch,
}))
import { useCollectStore } from '../src/stores/collect'

beforeEach(() => {
  setActivePinia(createPinia()); calls.single.mockReset(); calls.batch.mockReset()
  calls.single.mockImplementation(async (key: string) => ({ source_key: key, current: 2, total: 10 }))
  calls.batch.mockImplementation(async (keys: string[]) => keys.map(key => ({ source_key: key, current: 2, total: 10 })))
})

it('synchronises 600 unique sources with 3 bounded RPCs and retains keyed status updates', async () => {
  const store = useCollectStore()
  const keys = Array.from({ length: 600 }, (_, i) => `source_${i}`)
  await store.syncAllFromBackend([...keys, ...keys, ''])
  expect(calls.single).not.toHaveBeenCalled()
  expect(calls.batch).toHaveBeenCalledTimes(3)
  expect(calls.batch.mock.calls.map(args => args[0].length)).toEqual([256, 256, 88])
  expect(Object.keys(store.backendStatus)).toHaveLength(600)
  expect(store.backendStatus.source_599).toMatchObject({ current: 2, total: 10 })
})
