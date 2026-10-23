import { expect, it, vi } from 'vitest'
import { useAsyncResource } from '../src/reading/asyncResource'

function deferred<T>() {
  let resolve!: (value: T) => void
  const cancel = vi.fn()
  const promise = Object.assign(new Promise<T>(done => { resolve = done }), { cancel })
  return { promise, resolve, cancel }
}

it('cancels old work and keeps B when A completes late', async () => {
  const a = deferred<string>(), b = deferred<string>()
  const onData = vi.fn()
  const load = vi.fn().mockReturnValueOnce(a.promise).mockReturnValueOnce(b.promise)
  const resource = useAsyncResource<string>({ load, onData })
  const first = resource.run()
  const second = resource.run()
  expect(a.cancel).toHaveBeenCalledOnce()
  b.resolve('B')
  await second
  a.resolve('A')
  await first
  expect(resource.data.value).toBe('B')
  expect(onData.mock.calls).toEqual([['B']])
  expect(resource.status.value).toBe('ready')
})

it('releases a pending request and ignores its late completion after cancel', async () => {
  const a = deferred<string>()
  const resource = useAsyncResource({ load: () => a.promise })
  const pending = resource.run()
  resource.cancel()
  expect(a.cancel).toHaveBeenCalledOnce()
  a.resolve('late')
  await pending
  expect(resource.status.value).toBe('idle')
  expect(resource.data.value).toBeNull()
})
