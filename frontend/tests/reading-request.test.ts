import { expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { readingRequest } from '../src/api/requestChain'

it('cancels the active second binding in a composed request', async () => {
  const cancelled = vi.fn()
  const second = new CancellablePromise<string>(() => {}, cancelled)
  const started = vi.fn()
  const result = readingRequest(async request => {
    await request(CancellablePromise.resolve('detail'))
    started()
    return request(second)
  })
  const rejection = expect(result).rejects.toMatchObject({ name: 'CancelError' })
  await vi.waitFor(() => expect(started).toHaveBeenCalled())
  await result.cancel()
  await rejection
  expect(cancelled).toHaveBeenCalledTimes(1)
})

it('never starts follow-up work after the page cancels the first binding', async () => {
  const firstCancelled = vi.fn(), next = vi.fn()
  const first = new CancellablePromise<string>(() => {}, firstCancelled)
  const result = readingRequest(async request => { await request(first); next(); return 'late' })
  const rejected = expect(result).rejects.toMatchObject({ name: 'CancelError' })
  await Promise.resolve(); await Promise.resolve()
  await result.cancel(); await rejected
  expect(firstCancelled).toHaveBeenCalledOnce()
  expect(next).not.toHaveBeenCalled()
})
