import { CancellablePromise } from '@wailsio/runtime'
import { applicationRequest } from './requestChain'

/** A view owns its subscriptions even when another view shares the same read. */
export function createRequestScope() {
  const pending = new Set<CancellablePromise<unknown>>()
  function run<T>(start: () => CancellablePromise<T>): CancellablePromise<T> {
    const request = applicationRequest(consume => consume(start()))
    pending.add(request)
    const release = () => { pending.delete(request) }
    void Promise.resolve(request).then(release, release)
    return request
  }
  return { run, cancel() { for (const request of pending) void request.cancel(); pending.clear() } }
}
