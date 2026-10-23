import { expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { createRequestPipelines, requestKey } from '../src/api/requestPipelines'

it('cancels one subscriber independently, and cancels upstream only after the last leaves', async () => {
  const broker = createRequestPipelines(), cancel = vi.fn()
  const start = vi.fn(() => new CancellablePromise<string>(() => {}, cancel))
  const a = broker.run('read', 'read', [1], start), b = broker.run('read', 'read', [1], start)
  expect(a.request.requestId).not.toBe(b.request.requestId)
  expect(a.request.flightId).toBe(b.request.flightId)
  const rejectedA = expect(a).rejects.toMatchObject({ name: 'CancelError' })
  await a.cancel(); await rejectedA; expect(cancel).not.toHaveBeenCalled()
  const rejectedB = expect(b).rejects.toMatchObject({ name: 'CancelError' })
  await b.cancel(); await rejectedB; expect(cancel).toHaveBeenCalledOnce()
  await vi.waitFor(() => expect(broker.snapshot()[0]?.active).toBe(0))
})

it('keeps a surviving subscriber live after another page leaves', async () => {
  const broker = createRequestPipelines(), pending = CancellablePromise.withResolvers<string>()
  const a = broker.run('read', 'read', [], () => pending.promise), b = broker.run('read', 'read', [], () => pending.promise)
  await a.cancel('page left'); pending.resolve('loaded')
  expect(await b).toBe('loaded')
})

it('separates sources, methods, pages and argument types while canonicalising object key order', async () => {
  const broker = createRequestPipelines(), pending = CancellablePromise.withResolvers<string>(), start = vi.fn(() => pending.promise)
  const requests = [
    broker.run('read', 'read', [{ source: 'a', page: 1 }], start),
    broker.run('read', 'read', [{ page: 1, source: 'a' }], start),
    broker.run('read', 'read', [{ source: 'b', page: 1 }], start),
    broker.run('read', 'read', [{ source: 'a', page: 2 }], start),
    broker.run('other', 'read', [{ source: 'a', page: 1 }], start),
    broker.run('read', 'read', [{ source: 'a', page: '1' }], start),
  ]
  expect(start).toHaveBeenCalledTimes(5)
  pending.resolve('value'); await Promise.all(requests)
  expect(requestKey([[1, 2]])).not.toBe(requestKey([[2, 1]]))
})

it('isolates generated DTO mutations between subscribers and preserves prototypes', async () => {
  class DTO { values = [{ title: 'original' }]; title() { return this.values[0]?.title } }
  const broker = createRequestPipelines(), pending = CancellablePromise.withResolvers<DTO>()
  const a = broker.run('read', 'read', [], () => pending.promise), b = broker.run('read', 'read', [], () => pending.promise)
  pending.resolve(new DTO()); const first = await a, second = await b
  first.values[0]!.title = 'edited'
  expect(second.title()).toBe('original'); expect(second).toBeInstanceOf(DTO)
})

it('never joins reads across write dispatch or acknowledgement, and retains no completed results', async () => {
  const broker = createRequestPipelines(), pending = CancellablePromise.withResolvers<string>(), write = CancellablePromise.withResolvers<void>()
  const start = vi.fn(() => pending.promise)
  const a = broker.run('read', 'read', [], start)
  const saving = broker.run('save', 'write', [], () => write.promise)
  const b = broker.run('read', 'read', [], start)
  write.resolve(); await saving
  const c = broker.run('read', 'read', [], start)
  expect(start).toHaveBeenCalledTimes(3)
  pending.resolve('value'); await Promise.all([a, b, c])
  await broker.run('read', 'read', [], start); expect(start).toHaveBeenCalledTimes(4)
})

it('joins refreshes but separates cached reads before and during refresh', async () => {
  const broker = createRequestPipelines(), pending = CancellablePromise.withResolvers<string>(), start = vi.fn(() => pending.promise)
  const before = broker.run('read', 'read', [], start)
  const refresh = broker.run('refresh', 'refresh', [], start), same = broker.run('refresh', 'refresh', [], start)
  const during = broker.run('read', 'read', [], start)
  expect(refresh.request.flightId).toBe(same.request.flightId)
  expect(before.request.flightId).not.toBe(during.request.flightId)
  expect(start).toHaveBeenCalledTimes(3)
  pending.resolve('value'); await Promise.all([before, refresh, same, during])
})

it('releases failed flights for explicit retry, including synchronous transport failure', async () => {
  const broker = createRequestPipelines(), failure = new Error('offline')
  const start = vi.fn(() => { throw failure })
  await expect(broker.run('read', 'read', [], start)).rejects.toBe(failure)
  await expect(broker.run('read', 'read', [], start)).rejects.toBe(failure)
  expect(start).toHaveBeenCalledTimes(2); expect(broker.snapshot()[0]).toMatchObject({ active: 0, failed: 2 })
})

it('bounds distinct in-flight reads, allows existing subscribers to join and isolates event streams', async () => {
  const broker = createRequestPipelines(1), pending = CancellablePromise.withResolvers<string>(), start = vi.fn(() => pending.promise)
  const a = broker.run('read', 'read', [1], start), b = broker.run('read', 'read', [1], start)
  await expect(broker.run('read', 'read', [2], start)).rejects.toMatchObject({ code: 'UNAVAILABLE' })
  const streams = [broker.run('stream', 'isolated', [], start), broker.run('stream', 'isolated', [], start)]
  expect(streams[0]?.request.flightId).not.toBe(streams[1]?.request.flightId)
  pending.resolve('value'); await Promise.all([a, b, ...streams])
  expect(start).toHaveBeenCalledTimes(3)
})

it('coalesces a burst of 64 callers and exposes only bounded numeric pipeline observations', async () => {
  const broker = createRequestPipelines(), pending = CancellablePromise.withResolvers<string>(), start = vi.fn(() => pending.promise)
  const requests = Array.from({ length: 64 }, () => broker.run('GetSetting', 'read', ['private-key'], start))
  expect(start).toHaveBeenCalledOnce()
  expect(new Set(requests.map(request => request.request.requestId)).size).toBe(64)
  pending.resolve('value'); await Promise.all(requests)
  expect(broker.snapshot()).toEqual([{ pipeline: 'rpc.GetSetting', requested: 64, dispatched: 1, joined: 63, active: 0, cancelled: 0, failed: 0, rejected: 0 }])
  expect(JSON.stringify(broker.snapshot())).not.toContain('private-key')
})

it('preserves a malformed undefined rejection as failure and classifies backend cancellation', async () => {
  const broker = createRequestPipelines()
  await expect(broker.run('broken', 'read', [], () => Promise.reject(undefined))).rejects.toBeUndefined()
  await expect(broker.run('cancelled', 'read', [], () => Promise.reject({ code: 'CANCELLED', message: 'mode exited' }))).rejects.toMatchObject({ code: 'CANCELLED' })
  expect(broker.snapshot()).toEqual(expect.arrayContaining([
    expect.objectContaining({ pipeline: 'rpc.broken', failed: 1, active: 0 }),
    expect.objectContaining({ pipeline: 'rpc.cancelled', failed: 0, cancelled: 1, active: 0 }),
  ]))
})

it('starts a fresh flight after the last caller cancels and ignores old settlement', async () => {
  const broker = createRequestPipelines(), old = CancellablePromise.withResolvers<string>(), fresh = CancellablePromise.withResolvers<string>()
  const a = broker.run('read', 'read', [], () => old.promise)
  await a.cancel()
  const b = broker.run('read', 'read', [], () => fresh.promise)
  expect(b.request.flightId).not.toBe(a.request.flightId)
  fresh.resolve('new'); expect(await b).toBe('new')
  old.resolve('old')
  await vi.waitFor(() => expect(broker.snapshot()[0]?.active).toBe(0))
})
