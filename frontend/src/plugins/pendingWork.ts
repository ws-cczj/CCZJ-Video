export class PluginWorkCancelled extends Error {}

/** Abort a pending import/setup wait without blocking the other plugins. */
export function waitForPluginWork<T>(work: Promise<T>, signal: AbortSignal, timeout = 30000): Promise<T> {
  return new Promise<T>((resolve, reject) => {
    let settled = false
    const finish = (error: unknown, value?: T) => {
      if (settled) return
      settled = true
      clearTimeout(timer)
      signal.removeEventListener('abort', abort)
      if (error) reject(error)
      else resolve(value as T)
    }
    const abort = () => finish(new PluginWorkCancelled('Plugin initialization cancelled'))
    const timer = setTimeout(() => finish(new Error('Plugin initialization exceeded 30 seconds')), timeout)
    signal.addEventListener('abort', abort, { once: true })
    // Always attach both handlers, including after cancellation: late rejection
    // belongs to this cancelled job, not to the application's error popup.
    work.then(value => finish(null, value), error => finish(error))
    if (signal.aborted) abort()
  })
}
