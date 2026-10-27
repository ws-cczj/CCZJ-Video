import { CancelError, CancellablePromise } from '@wailsio/runtime'
import { toApiFailure } from './errors'

type Pending<T> = Promise<T> & { cancel?: (cause?: unknown) => unknown }

/** A composed read owns every currently active binding, including requests
 * started after an earlier await. Cancelling a page releases the actual I/O.
 */
export function applicationRequest<T>(work: (request: <R>(pending: Pending<R>) => Promise<R>) => Promise<T>): CancellablePromise<T> {
  let cancelled = false
  let cancellationCause: unknown
  const active = new Set<Pending<unknown>>()
  function check(): void { if (cancelled) throw new CancelError('Request cancelled', { cause: cancellationCause }) }
  // The runtime recognises this exact cancellation type and cause when an
  // already-cancelled composition receives its underlying rejection later.
  function failure(error:unknown):unknown {
    if (cancelled) return new CancelError('Request cancelled', { cause: cancellationCause })
    return error instanceof CancelError ? error : toApiFailure(error)
  }
  async function request<R>(pending: Pending<R>): Promise<R> {
    if (cancelled) { cancel(pending); check() }
    active.add(pending)
    try { const value = await pending; check(); return value } catch(error){throw failure(error)} finally { active.delete(pending) }
  }
  function cancel(pending: Pending<unknown>): void {
    try { Promise.resolve(pending.cancel?.(cancellationCause)).catch(() => {}) } catch { /* already settled */ }
  }
  return new CancellablePromise<T>((resolve, reject) => {
    // Register an already-created binding before exposing cancel(). Deferring
    // this first step can orphan its I/O when cancellation arrives immediately.
    try {
      void work(request).then(value => { check(); resolve(value) }).catch(error => reject(failure(error)))
    } catch (error) { reject(failure(error)) }
  }, cause => { cancelled = true; cancellationCause = cause; for (const pending of active) cancel(pending) })
}

// Existing reading API adapters keep their public import during migration.
export const readingRequest = applicationRequest
