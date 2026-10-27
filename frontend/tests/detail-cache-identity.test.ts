import { beforeEach, expect, it, vi } from 'vitest'

const disk = vi.hoisted(() => ({ rows: {} as Record<string, unknown> }))
vi.mock('../src/api/app', () => ({}))
vi.mock('../src/platform/storage', async original => ({
  ...await original<typeof import('../src/platform/storage')>(),
  readStorage: (key: string, fallback: unknown) => key === 'cczj_detail_cache_v3' ? disk.rows : fallback,
}))
vi.mock('../src/platform/debouncedWrite', () => ({ createDebouncedWriter: () => ({ schedule: vi.fn(), cancel: vi.fn() }) }))

beforeEach(() => { vi.resetModules(); disk.rows = {} })

it('evicts an older wrong-film payload without clearing unrelated cache entries', async () => {
  const entry = (id: string) => ({ format: 'json', chunks: [JSON.stringify({ video: { vod_id: id, vod_name: id }, lines: [] })] })
  disk.rows = { 'source:wanted': entry('other'), 'source:valid': entry('valid') }
  const cache = await import('../src/stores/detailCache')
  expect(await cache.readDetailCache('source', 'wanted')).toBeNull()
  expect(disk.rows['source:wanted']).toBeUndefined()
  expect((await cache.readDetailCache('source', 'valid'))?.video?.vod_id).toBe('valid')
})

it('does not persist an unrelated remote detail under the requested source ID', async () => {
  const cache = await import('../src/stores/detailCache')
  await cache.writeDetailCache('source', 'wanted', { video: { vod_id: 'other', vod_name: 'other' }, episodes: [] })
  expect(await cache.readDetailCache('source', 'wanted')).toBeNull()
})
