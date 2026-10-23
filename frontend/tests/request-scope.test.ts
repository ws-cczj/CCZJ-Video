import { expect, it, vi } from 'vitest'
import { CancellablePromise } from '@wailsio/runtime'
import { createRequestScope } from '../src/api/requestScope'
import { createRequestPipelines } from '../src/api/requestPipelines'

it('releases the leaving view without interrupting another subscriber or publishing its late value', async () => {
  const pipelines = createRequestPipelines(), scope = createRequestScope()
  const pending = CancellablePromise.withResolvers<string>(), start = vi.fn(() => pending.promise), published = vi.fn()
  const a = scope.run(() => pipelines.run('ProbeSources', 'refresh', [], start))
  const observing = a.then(published, () => {})
  const b = pipelines.run('ProbeSources', 'refresh', [], start)
  scope.cancel(); pending.resolve('fresh')
  await observing; expect(await b).toBe('fresh')
  expect(start).toHaveBeenCalledOnce(); expect(published).not.toHaveBeenCalled()
})

it('allows a reactivated view to start fresh after cancelling its previous work', async () => {
  const scope = createRequestScope(), stopped = vi.fn()
  const old = scope.run(() => new CancellablePromise<string>(() => {}, stopped))
  const rejected = expect(old).rejects.toMatchObject({ name: 'CancelError' })
  scope.cancel(); await rejected
  expect(stopped).toHaveBeenCalledOnce()
  expect(await scope.run(() => CancellablePromise.resolve('new'))).toBe('new')
})
