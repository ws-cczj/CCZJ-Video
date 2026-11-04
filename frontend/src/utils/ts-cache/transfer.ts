import type { LoaderStats } from 'hls.js'
import { MAX_CACHED_BYTES, MAX_FRAGMENT_CAPTURE_BYTES, MAX_QUEUE_PER_EPISODE } from './constants'
import { diskLoad, diskDelete } from './disk-cache'
import { cacheGet, cacheSet, cacheDelete } from './memory-cache'
import { adaptiveFragmentTimeout, recordNetworkFailure, updateNetworkDiagnosis } from './network'
import { rangeLength, segmentCacheKey } from './segment-key'
import { fireListeners, prefetchState, session, telemetry } from './store'
import { recordFetchDuration } from './telemetry'

export type TransferPurpose = 'playback' | 'prefetch' | 'helper' | 'preview' | 'warmup'
export class SegmentFailure extends Error {
  constructor(public code: number, message: string, public kind: 'network' | 'http' | 'content' | 'capacity' = 'network', public retryAfterMs = 0) { super(message) }
}
export interface SegmentFailureRecord {
  attempts: number; failures: number; nextRetry: number; at: number; position: number; firstAt: number; firstPosition: number; error: SegmentFailure
}
export interface TransferResult { buffer: ArrayBuffer; stats: LoaderStats; cached: boolean; streamed: boolean }
export interface SegmentLease { promise: Promise<TransferResult>; release: () => void }
interface Consumer { purpose: TransferPurpose; progress?: (stats: LoaderStats, data: ArrayBuffer) => void; streamed: boolean }
interface Flight {
  key: string; url: string; range: string | null; episodeKey: string; generation: number
  controller: AbortController; consumers: Set<Consumer>; chunks: Uint8Array[]; captured: number
  capture: boolean; stats: LoaderStats; promise: Promise<TransferResult>; settled: boolean
}
const flights = new Map<string, Flight>()
const failures = new Map<string, SegmentFailureRecord>()
const successes = new Map<string, { at: number; attempts: number; position: number }>()
const decoding = new Map<string, number>()
let retainedBytes = 0
const totals = { networkRequests: 0, completedTransfers: 0, failedTransfers: 0, cancelledTransfers: 0, interruptedBytes: 0, joinedTransfers: 0 }
export function transferSnapshot() { return { ...totals, ...transferCounts() } }
let changed: () => void = () => {}
export function onTransfersChanged(callback: () => void): void { changed = callback }
export function newLoaderStats(): LoaderStats {
  return { aborted: false, loaded: 0, retry: 0, total: 0, chunkCount: 0, bwEstimate: 0,
    loading: { start: 0, first: 0, end: 0 }, parsing: { start: 0, end: 0 }, buffering: { start: 0, first: 0, end: 0 } }
}
export function transferKey(url: string, range?: string | null): string { return segmentCacheKey(url, range) }
export function transferFailure(url: string, range?: string | null): SegmentFailureRecord | undefined { return failures.get(transferKey(url, range)) }
export function transferBusy(url: string, range?: string | null): boolean {
  const key = transferKey(url, range)
  if (flights.has(key)) return true
  const until = decoding.get(key)
  if (until && until > performance.now()) return true
  decoding.delete(key); return false
}
export function transferCounts() {
  const counts = { prefetch: 0, helper: 0, playback: 0, foreground: 0, preview: 0, warmup: 0, bytes: retainedBytes }
  for (const flight of flights.values()) {
    const purposes = new Set([...flight.consumers].map(c => c.purpose))
    for (const purpose of purposes) counts[purpose]++
    if (purposes.has('playback') && !purposes.has('prefetch') && !purposes.has('helper')) counts.foreground++
  }
  return counts
}
export function pruneTransferFailures(keys: Set<string>): void {
  for (const key of failures.keys()) if (!keys.has(key) && !flights.has(key)) failures.delete(key)
  for (const key of successes.keys()) if (!keys.has(key) && !flights.has(key)) successes.delete(key)
  for (const key of decoding.keys()) if (!keys.has(key) && !flights.has(key)) decoding.delete(key)
}
export function transferSucceededSince(url: string, range: string | null | undefined, since: number): boolean { return (successes.get(transferKey(url, range))?.at ?? -Infinity) >= since }
export function confirmSegmentDecoded(url: string, range?: string | null): void { const key = transferKey(url, range); failures.delete(key); decoding.delete(key); changed() }
export function rejectSegmentContent(url: string, range?: string | null): void {
  const key = transferKey(url, range), previous = failures.get(key), success = successes.get(key)
  decoding.delete(key)
  if (previous?.error.kind === 'content' && previous.at >= (success?.at ?? 0)) return
  cacheDelete(url, range); void diskDelete(url, range)
  const at = performance.now(), count = (previous?.failures ?? 0) + 1
  failures.set(key, { attempts: success?.attempts ?? previous?.attempts ?? 1, failures: count, at, position: session.playbackPosition,
    firstAt: previous?.firstAt ?? success?.at ?? at, firstPosition: previous?.firstPosition ?? success?.position ?? session.playbackPosition,
    nextRetry: at + Math.min(15_000, 750 * 2 ** Math.min(5, count - 1)), error: new SegmentFailure(0, 'media fragment could not be parsed', 'content') })
  changed(); fireListeners()
}
export function clearTransferFailures(): void { failures.clear(); successes.clear(); decoding.clear() }
export function resetSegmentFailure(url: string, range?: string | null): void {
  const row = transferFailure(url, range)
  if (row) row.nextRetry = 0
  changed()
}
export function cancelTransfers(): void {
  for (const flight of flights.values()) flight.controller.abort()
  // Keep ownership until each aborted reader has actually stopped.
  clearTransferFailures()
}
export function cancelSpeculation(keep: Set<string>): void {
  for (const flight of flights.values()) {
    if (keep.has(flight.key) || [...flight.consumers].some(c => c.purpose === 'playback' || c.purpose === 'preview')) continue
    flight.controller.abort()
  }
}

/** One transport per resource/range. Consumers detach independently; the last
 * consumer owns cancellation. Progress is copied before a worker can transfer it. */
export function acquireSegment(options: {
  url: string; range?: string | null; purpose: TransferPurpose; episodeKey?: string; duration?: number
  headers?: Record<string, string>; maxBytes?: number; timeoutMs?: number
  progress?: (stats: LoaderStats, data: ArrayBuffer) => void
}): SegmentLease {
  const key = transferKey(options.url, options.range)
  const consumer: Consumer = { purpose: options.purpose, progress: options.progress, streamed: false }
  let flight = flights.get(key)
  if (flight?.controller.signal.aborted || (flight && flight.generation !== prefetchState.cacheSession)) {
    // Await the old owner before retrying, rather than opening a duplicate GET.
    const old = flight
    let next: ReturnType<typeof acquireSegment> | undefined, released = false
    const promise: Promise<TransferResult> = old.promise.catch(() => undefined).then(() => {
      if (released) throw new DOMException('Cancelled', 'AbortError')
      next = acquireSegment(options)
      return next.promise
    })
    return { promise, release: () => { released = true; next?.release() } }
  }
  if (!flight) {
    flight = { key, url: options.url, range: options.range ?? null, episodeKey: options.episodeKey || session.currentEpKey,
      generation: prefetchState.cacheSession, controller: new AbortController(), consumers: new Set(), chunks: [], captured: 0,
      capture: true, stats: newLoaderStats(), settled: false, promise: undefined! }
    flights.set(key, flight)
    const owned = flight
    // Register ownership and the first consumer before disk or network awaits.
    flight.consumers.add(consumer)
    flight.promise = Promise.resolve().then(() => run(owned, options)).finally(() => {
      owned.settled = true
      retainedBytes -= owned.captured; owned.captured = 0; owned.chunks.length = 0
      if (flights.get(key) === owned) flights.delete(key)
      changed(); fireListeners()
    })
  } else {
    if (!flight.capture && consumer.progress) {
      return { promise: Promise.reject(new SegmentFailure(0, 'stream prefix no longer retained', 'capacity')), release: () => {} }
    }
    flight.consumers.add(consumer)
    if (options.purpose !== 'preview') totals.joinedTransfers++
    if (consumer.progress && flight.capture) for (const chunk of flight.chunks) {
      consumer.streamed = true
      consumer.progress(flight.stats, chunk.slice().buffer as ArrayBuffer)
    }
  }
  const owned = flight
  let released = false
  const promise = owned.promise.then(result => ({ ...result, streamed: consumer.streamed }))
  return { promise, release: () => {
    if (released) return
    released = true; owned.consumers.delete(consumer)
    if (!owned.settled && owned.consumers.size === 0) owned.controller.abort()
    changed()
  } }
}

async function run(flight: Flight, options: Parameters<typeof acquireSegment>[0]): Promise<TransferResult> {
  const signal = flight.controller.signal
  const current = () => !signal.aborted && flight.generation === prefetchState.cacheSession
  const check = () => { if (!current()) throw new DOMException('Cancelled', 'AbortError') }
  const purposes = () => [...flight.consumers].map(c => c.purpose)
  const observed = () => purposes().some(p => p !== 'preview')
  const playbackObservation = () => purposes().some(p => p === 'playback' || p === 'prefetch' || p === 'helper')
  const rejected = failures.get(flight.key)?.error.kind === 'content'
  const hit = rejected ? null : cacheGet(flight.url, flight.range)
  const stored = rejected ? null : hit?.buffer ?? await diskLoad(flight.url, flight.range)
  check()
  if (stored?.byteLength) {
    if (!hit) cacheSet(flight.url, stored, flight.episodeKey, flight.range)
    return { buffer: stored, stats: flight.stats, cached: true, streamed: false }
  }
  const failure = failures.get(flight.key)
  if (failure && failure.nextRetry > performance.now()) throw failure.error
  const attempts = (failure?.attempts ?? 0) + 1
  const stats = flight.stats
  stats.retry = attempts - 1
  stats.loading.start = performance.now()
  let countedAttempt = false, timer: ReturnType<typeof setTimeout> | undefined
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined
  let response: Response | undefined
  const clearDeadline = () => { if (timer !== undefined) clearTimeout(timer) }
  try {
    timer = setTimeout(() => flight.controller.abort(new DOMException('Segment timed out', 'TimeoutError')), Math.min(options.timeoutMs ?? adaptiveFragmentTimeout(), 10_000))
    const headers = { ...options.headers }
    if (flight.range) headers.Range = `bytes=${flight.range}`
    countedAttempt = true
    if (playbackObservation()) totals.networkRequests++
    response = await fetch(flight.url, { signal, headers, priority: options.purpose === 'playback' ? 'high' : 'low' } as RequestInit)
    check(); clearDeadline()
    timer = setTimeout(() => flight.controller.abort(new DOMException('Segment timed out', 'TimeoutError')), Math.max(1, (options.timeoutMs ?? adaptiveFragmentTimeout()) - (performance.now() - stats.loading.start)))
    stats.loading.first = performance.now()
    if (!response.ok) {
      const hint = response.headers.get('Retry-After') || ''
      const retryAfter = /^\d+$/.test(hint) ? Number(hint) * 1000 : Date.parse(hint) - Date.now()
      throw new SegmentFailure(response.status, 'media HTTP request failed', 'http', Number.isFinite(retryAfter) ? Math.max(0, Math.min(2_147_483_647, retryAfter)) : 0)
    }
    if (!flight.range && response.status === 206) {
      const whole = response.headers.get('Content-Range')?.match(/^bytes\s+0-(\d+)\/(\d+)$/i)
      if (!whole || Number(whole[1]) + 1 !== Number(whole[2])) throw new SegmentFailure(206, 'unexpected partial media response', 'content')
    }
    if (/text\/html|application\/(?:json|problem\+json)/i.test(response.headers.get('Content-Type') || '')) throw new SegmentFailure(response.status, 'media response is an error document', 'content')
    const encoded = response.headers.get('Content-Encoding')
    const declared = encoded && encoded !== 'identity' ? 0 : Number(response.headers.get('Content-Length'))
    const streaming = !flight.range && [...flight.consumers].some(c => !!c.progress)
    const limit = Math.min(options.maxBytes ?? MAX_FRAGMENT_CAPTURE_BYTES, MAX_FRAGMENT_CAPTURE_BYTES)
    if (declared > limit && !streaming) throw new SegmentFailure(0, 'media capture exceeds budget', 'capacity')
    const expectedRange = flight.range ? rangeLength(flight.range) : 0
    if (flight.range && response.status === 206) {
      const contentRange = response.headers.get('Content-Range')?.match(/^bytes\s+(\d+)-(\d+)\/(?:\d+|\*)$/i)
      if (!contentRange || `${contentRange[1]}-${contentRange[2]}` !== flight.range) throw new SegmentFailure(206, 'incorrect byte-range response', 'content')
    }
    reader = response.body?.getReader()
    if (!reader) throw new SegmentFailure(0, 'empty media response', 'content')
    while (true) {
      const chunk = await reader.read()
      check()
      if (chunk.done) break
      const data = chunk.value
      stats.loaded += data.byteLength; stats.chunkCount++
      if (observed()) telemetry.networkBytes += data.byteLength
      const canStream = !flight.range && [...flight.consumers].some(c => !!c.progress)
      if (flight.capture && (stats.loaded > limit || retainedBytes + data.byteLength > MAX_CACHED_BYTES)) {
        if (!canStream) throw new SegmentFailure(0, 'media capture exceeds budget', 'capacity')
        retainedBytes -= flight.captured; flight.captured = 0; flight.chunks.length = 0; flight.capture = false
      }
      if (flight.capture) { flight.chunks.push(data); flight.captured += data.byteLength; retainedBytes += data.byteLength }
      if (!flight.range) for (const consumer of flight.consumers) if (consumer.progress) {
        consumer.streamed = true
        consumer.progress(stats, data.slice().buffer as ArrayBuffer)
      }
    }
    if (!stats.loaded || (declared > 0 && stats.loaded !== declared)) throw new SegmentFailure(0, 'incomplete media response', 'content')
    stats.loading.end = performance.now(); stats.total = stats.loaded
    let buffer = new ArrayBuffer(flight.capture ? flight.captured : 0)
    if (flight.capture) {
      const view = new Uint8Array(buffer)
      let offset = 0
      for (const chunk of flight.chunks) { view.set(chunk, offset); offset += chunk.byteLength }
      if (flight.range) {
        const start = Number(flight.range.split('-')[0])
        if (response.status === 200 && buffer.byteLength >= start + expectedRange) buffer = buffer.slice(start, start + expectedRange)
        if (buffer.byteLength !== expectedRange) throw new SegmentFailure(response.status, 'incomplete byte-range response', 'content')
      }
      check()
      cacheSet(flight.url, buffer, flight.episodeKey, flight.range, stats.loading.end - stats.loading.start)
    }
    if (failure?.error.kind !== 'content') failures.delete(flight.key)
    if (!flight.capture && purposes().includes('playback')) decoding.set(flight.key, performance.now() + 10_000)
    if (playbackObservation()) {
      totals.completedTransfers++
      successes.delete(flight.key); successes.set(flight.key, { at: performance.now(), attempts, position: session.playbackPosition })
      while (successes.size > MAX_QUEUE_PER_EPISODE * 2) successes.delete(successes.keys().next().value!)
      recordFetchDuration(stats.loading.end - stats.loading.start, stats.loaded, options.duration, stats.loading.start); updateNetworkDiagnosis()
    }
    return { buffer, stats, cached: false, streamed: false }
  } catch (cause) {
    const timeout = signal.aborted && signal.reason?.name === 'TimeoutError'
    if (flight.generation !== prefetchState.cacheSession || (signal.aborted && !timeout)) {
      if (countedAttempt && playbackObservation()) { totals.cancelledTransfers++; totals.interruptedBytes += stats.loaded }
      throw new DOMException('Cancelled', 'AbortError')
    }
    const error = cause instanceof SegmentFailure ? cause : new SegmentFailure(0, timeout ? 'media request timed out' : 'media request failed')
    if (countedAttempt && playbackObservation()) {
      totals.failedTransfers++; totals.interruptedBytes += stats.loaded
      const count = (failure?.failures ?? 0) + 1
      failures.delete(flight.key)
      failures.set(flight.key, { attempts, failures: count, nextRetry: performance.now() + Math.max(error.retryAfterMs, Math.min(15_000, 750 * 2 ** Math.min(5, count - 1))), at: performance.now(), position: session.playbackPosition,
        firstAt: failure?.firstAt ?? performance.now(), firstPosition: failure?.firstPosition ?? session.playbackPosition, error })
      while (failures.size > MAX_QUEUE_PER_EPISODE * 2) failures.delete(failures.keys().next().value!)
      if (error.kind !== 'capacity') recordNetworkFailure()
    }
    throw error
  } finally {
    clearDeadline()
    await reader?.cancel().catch(() => {})
    reader?.releaseLock()
    if (!reader) await response?.body?.cancel().catch(() => {})
  }
}

if (import.meta.hot) import.meta.hot.dispose(() => { cancelTransfers(); changed = () => {} })
