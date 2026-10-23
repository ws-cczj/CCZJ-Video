import { afterEach, describe, expect, it, vi } from 'vitest'
import { createVisiblePolling } from '../src/diagnostics/polling'
import { appendResourceSample } from '../src/diagnostics/history'

afterEach(() => { vi.clearAllTimers(); vi.useRealTimers() })

describe('retained diagnostics panel polling', () => {
  it('bounds history and preserves unknown native values', () => {
    let history: ReturnType<typeof appendResourceSample> = []
    for (let i = 1; i <= 1000; i++) history = appendResourceSample(history, { goroutines: 10, resources: { sampled_at_ms: i, native_available: false, heap_alloc_bytes: 1024 } })
    expect(history).toHaveLength(150)
    expect(history[0].at).toBe(851)
    expect(history[0].committed).toBeNull()
    expect(appendResourceSample(history, { resources: { sampled_at_ms: 1000 } })).toBe(history)
  })
  it('has at most one backend request in flight', async () => {
    vi.useFakeTimers()
    const request = vi.fn(() => new Promise(() => {}))
    const polling = createVisiblePolling(request, vi.fn(), vi.fn())
    polling.setActive(true)
    await vi.advanceTimersByTimeAsync(10_000)
    expect(request).toHaveBeenCalledOnce()
    polling.setActive(false)
  })

  it('ignores a response returned after hiding or disposing the panel', async () => {
    vi.useFakeTimers()
    let resolve!: (value: string) => void
    const receive = vi.fn()
    const polling = createVisiblePolling(() => new Promise<string>(yes => { resolve = yes }), receive, vi.fn())
    polling.setActive(true)
    polling.setActive(false)
    resolve('stale metrics')
    await vi.advanceTimersByTimeAsync(10_000)
    expect(receive).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(0)
  })
})
