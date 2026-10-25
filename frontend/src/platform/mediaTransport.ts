import { isIOSRuntime } from '../api/runtime'

let endpoint = ''

/** Native AVPlayer needs HTTP streaming; the listener is owned by Go's app
 * lifecycle. Cache only this document's capability, never persist or log it. */
export async function playbackTransportURL(proxyURL: string, signal: AbortSignal): Promise<string> {
  if (!isIOSRuntime() || !proxyURL.startsWith('/__cczj/hls?')) return proxyURL
  if (!endpoint) {
    const response = await fetch('/__cczj/mobile-media', { signal, cache: 'no-store' })
    if (!response.ok) throw new Error('Mobile media transport unavailable')
    const data: unknown = await response.json()
    const value = data && typeof data === 'object' ? (data as Record<string, unknown>).endpoint : undefined
    if (typeof value !== 'string' || !/^http:\/\/127\.0\.0\.1:\d+\/[a-f0-9]{64}$/.test(value)) {
      throw new Error('Invalid mobile media transport')
    }
    signal.throwIfAborted()
    endpoint = value
  }
  signal.throwIfAborted()
  return endpoint + proxyURL.replace('/__cczj/hls?', '/__cczj/ios-hls?')
}
