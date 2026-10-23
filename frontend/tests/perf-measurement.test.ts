import { beforeEach, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePerfStore } from '../src/stores/perf'

vi.mock('../src/diagnostics/recording', () => ({
  recordDiagnosticEvent: vi.fn(), registerPlaybackSnapshot: vi.fn(),
}))
vi.mock('../src/diagnostics/browserResources', () => ({ captureBrowserResources: () => ({}) }))

beforeEach(() => setActivePinia(createPinia()))

it('keeps insufficient decoder samples distinct from a measured zero drop rate', () => {
  const perf = usePerfStore()
  expect(perf.dropRate).toBeNull()
  // Actual short Android recording: hiding this rate must not report 0%.
  perf.addFrames(181, 23)
  expect(perf.dropRate).toBeNull()
  expect(perf.snapshot().frames).toEqual({ total: 181, dropped: 23 })
  perf.addFrames(19, 0)
  expect(perf.dropRate).toBe(11.5)
})

it('reports zero after enough frames have actually been sampled without drops', () => {
  const perf = usePerfStore()
  perf.addFrames(200, 0)
  expect(perf.dropRate).toBe(0)
})
