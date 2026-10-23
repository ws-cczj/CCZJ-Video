import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { Events } from '@wailsio/runtime'
import { onBackendEvent } from '../src/api/events'
import { applyUpdateDownloadProgress, updateDownloadProgress } from '../src/stores/updateState'

vi.mock('@wailsio/runtime', async importOriginal => ({ ...await importOriginal<typeof import('@wailsio/runtime')>(), Events: { On: vi.fn(() => vi.fn()), Off: vi.fn() } }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

describe('update progress through the Wails event adapter', () => {
  beforeEach(() => {
    vi.useFakeTimers()
    Object.assign(updateDownloadProgress, { downloaded: 0, total: 0, percent: 0, speed_bps: 0 })
  })
  afterEach(() => vi.useRealTimers())

  it('updates the shared state used by the background indicator', () => {
    onBackendEvent('update:download:progress', applyUpdateDownloadProgress)
    const deliver = vi.mocked(Events.On).mock.calls[0][1]
    deliver({ name: 'update:download:progress', data: { downloaded: 512, total: 1024, speed_bps: 256 } })
    vi.runOnlyPendingTimers()
    expect(updateDownloadProgress).toEqual({ downloaded: 512, total: 1024, percent: 50, speed_bps: 256 })
  })

  it('resets percentage for an unknown-length fallback source', () => {
    applyUpdateDownloadProgress({ downloaded: 50, total: 100, speed_bps: 20 })
    onBackendEvent('update:download:progress', applyUpdateDownloadProgress)
    const deliver = vi.mocked(Events.On).mock.calls[0][1]
    deliver({ name: 'update:download:progress', data: { downloaded: 20, total: -1, speed_bps: 10 } })
    vi.runOnlyPendingTimers()
    expect(updateDownloadProgress).toEqual({ downloaded: 20, total: 0, percent: 0, speed_bps: 10 })
  })

  it('keeps malformed or overflowing progress out of the indicator', () => {
    applyUpdateDownloadProgress({ downloaded: 110, total: 100, speed_bps: NaN })
    expect(updateDownloadProgress.percent).toBe(100)
    expect(updateDownloadProgress.speed_bps).toBe(0)
    applyUpdateDownloadProgress({ downloaded: NaN, total: Infinity, speed_bps: -10 })
    expect(updateDownloadProgress).toEqual({ downloaded: 0, total: 0, percent: 0, speed_bps: 0 })
  })
})
