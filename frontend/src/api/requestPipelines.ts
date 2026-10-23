import { CancelError, CancellablePromise } from '@wailsio/runtime'
import { normalizeApiError } from './errors'

export type RequestPolicy = 'read' | 'write' | 'isolated' | 'refresh'
export interface RequestIdentity { readonly requestId: string; readonly flightId: string; readonly pipeline: string }
export type PipelineRequest<T> = CancellablePromise<T> & { readonly request: RequestIdentity }
type Pending<T> = Promise<T> & { cancel?: (cause?: unknown) => unknown }
interface Subscriber { resolve: (value: unknown) => void; reject: (error: unknown) => void }
interface Flight { id: string; pending?: Pending<unknown>; subscribers: Set<Subscriber>; key?: string; finished: boolean }
interface Stat { pipeline: string; requested: number; dispatched: number; joined: number; active: number; cancelled: number; failed: number; rejected: number }

// Match the JSON values actually sent over IPC: object key order is irrelevant,
// array order and value types are significant. Never expose argument keys in diagnostics.
export function requestKey(args: readonly unknown[]): string {
  return JSON.stringify(args, (_key, value: unknown) => {
    if (value && typeof value === 'object' && !Array.isArray(value)) {
      return Object.fromEntries(Object.entries(value).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0))
    }
    return value
  })
}

// Generated DTOs are JSON data with class prototypes. Give shared consumers
// independent mutable values while retaining their generated model methods.
function copyDTO<T>(value: T): T {
  if (Array.isArray(value)) return value.map(copyDTO) as T
  if (value && typeof value === 'object') {
    const copy = Object.create(Object.getPrototypeOf(value))
    for (const key of Object.keys(value)) Object.defineProperty(copy, key, { value: copyDTO((value as Record<string, unknown>)[key]), writable: true, enumerable: true, configurable: true })
    return copy
  }
  return value
}

/** Owns in-flight reads only, never a result cache or a write retry queue. */
export function createRequestPipelines(limit = 128) {
  const reads = new Map<string, Flight>(), stats = new Map<string, Stat>()
  const session = Math.random().toString(36).slice(2, 10)
  let sequence = 0, revision = 0, activeReads = 0
  const nextID = () => `${session}-${++sequence}`
  function run<T>(name: string, policy: RequestPolicy, args: readonly unknown[], start: () => Pending<T>): PipelineRequest<T> {
    const pipeline = `rpc.${name}`
    let stat = stats.get(pipeline)
    if (!stat) { stat = { pipeline, requested: 0, dispatched: 0, joined: 0, active: 0, cancelled: 0, failed: 0, rejected: 0 }; stats.set(pipeline, stat) }
    stat.requested++
    const row = stat, requestId = nextID(), shared = policy === 'read' || policy === 'refresh'
    // A dispatched write and its acknowledgement each separate read generations.
    // Reads made during a write cannot join a pre-write or post-commit flight.
    const argumentsKey = shared ? requestKey(args) : undefined
    let key = shared ? `${revision}:${pipeline}:${argumentsKey}` : undefined
    let flight = key === undefined ? undefined : reads.get(key)
    const joined = !!flight
    if (!flight) {
      if (policy === 'refresh') { revision++; key = `${revision}:${pipeline}:${argumentsKey}` }
      flight = { id: nextID(), subscribers: new Set(), key, finished: false }
      if (shared && activeReads >= limit) {
        row.rejected++
        return Object.assign(CancellablePromise.reject<T>(Object.assign(new Error('并发读取已达上限，请稍后重试'), { code: 'UNAVAILABLE' })), { request: Object.freeze({ requestId, flightId: flight.id, pipeline }) })
      }
      if (key !== undefined) { reads.set(key, flight); activeReads++ }
    } else row.joined++
    const current = flight
    let subscriber!: Subscriber
    const request = new CancellablePromise<T>((resolve, reject) => {
      subscriber = { resolve: value => resolve(value as T), reject }; current.subscribers.add(subscriber)
    }, cause => {
      if (!current.subscribers.delete(subscriber)) return
      row.cancelled++
      if (!current.finished && current.subscribers.size === 0) {
        if (current.key && reads.get(current.key) === current) reads.delete(current.key)
        return Promise.resolve(current.pending?.cancel?.(cause)).then(() => {})
      }
    }) as PipelineRequest<T>
    Object.defineProperty(request, 'request', { value: Object.freeze({ requestId, flightId: current.id, pipeline }), enumerable: true })
    if (!joined) {
      row.dispatched++; row.active++
      if (policy === 'write') revision++
      function finish(succeeded: boolean, error: unknown, value?: T) {
        current.finished = true; row.active--
        if (shared) activeReads--
        if (current.key && reads.get(current.key) === current) reads.delete(current.key)
        if (policy === 'write' || policy === 'refresh') revision++
        const cancelled = !succeeded && (error instanceof CancelError || normalizeApiError(error).code === 'CANCELLED')
        if (!succeeded && !cancelled) row.failed++
        const subscribers = [...current.subscribers]; current.subscribers.clear()
        if (cancelled) row.cancelled += subscribers.length
        for (const subscriber of subscribers) {
          if (!succeeded) subscriber.reject(error)
          else {
            try { subscriber.resolve(subscribers.length > 1 ? copyDTO(value) : value) }
            catch (error) { subscriber.reject(error) }
          }
        }
      }
      try {
        const pending = start()
        current.pending = pending
        // Use a native promise observer so cancelling a subscriber never cancels
        // the shared observer, nor creates a runtime late-rejection chain.
        void Promise.resolve(pending).then(value => finish(true, undefined, value), error => finish(false, error))
      } catch (error) { finish(false, error) }
    }
    return request
  }
  return { run, snapshot: () => [...stats.values()].map(row => ({ ...row })) }
}

export const requestPipelines = createRequestPipelines()
