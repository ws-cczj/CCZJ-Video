import { afterEach, describe, expect, it, vi } from 'vitest'
import { clearM3u8TextCache, fetchAndParseM3u8, getM3u8FromCache, setM3u8Cache } from '../src/utils/ts-cache/m3u8'

vi.mock('../src/utils/ts-cache/ad-filter', () => ({ stripAdFromM3u8Text: (text: string) => text }))
afterEach(() => { clearM3u8TextCache(); vi.unstubAllGlobals() })
describe('playlist input memory bounds', () => {
  it('rejects a direct media response without reading the entire video as text', async () => {
    const response = new Response('not a playlist', { headers: { 'Content-Type': 'video/mp4', 'Content-Length': '1000000000' } })
    const text = vi.spyOn(response, 'text')
    vi.stubGlobal('fetch', vi.fn(async () => response))
    await expect(fetchAndParseM3u8('https://media/movie.mp4')).rejects.toThrow()
    expect(text).not.toHaveBeenCalled()
    expect(getM3u8FromCache('https://media/movie.mp4')).toBeNull()
  })
  it('rejects generic binary input at its first chunk and cancels the response', async () => {
    const cancel = vi.fn()
    let pulls = 0
    const body = new ReadableStream<Uint8Array>({
      pull(controller) {
        if (++pulls === 1) controller.enqueue(new Uint8Array([0, 1, 2, 3]))
        else if (pulls === 2) controller.enqueue(new Uint8Array(1024))
        else controller.close()
      }, cancel,
    })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(body)))
    await expect(fetchAndParseM3u8('https://media/short-link')).rejects.toThrow()
    expect(cancel).toHaveBeenCalledOnce()
  })
  it('rejects a declared oversized playlist before decoding it', async () => {
    const response = new Response('#EXTM3U', { headers: { 'Content-Type': 'application/vnd.apple.mpegurl', 'Content-Length': String(9 * 1024 * 1024) } })
    const text = vi.spyOn(response, 'text')
    vi.stubGlobal('fetch', vi.fn(async () => response))
    await expect(fetchAndParseM3u8('https://media/huge.m3u8')).rejects.toThrow()
    expect(text).not.toHaveBeenCalled()
  })
  it('bounds total playlist cache text instead of only the number of entries', () => {
    const text = '#EXTM3U\n#' + 'x'.repeat(1024 * 1024)
    for (let i = 0; i < 16; i++) setM3u8Cache(`https://media/${i}.m3u8`, text)
    const retainedBytes = Array.from({ length: 16 }, (_, i) => (getM3u8FromCache(`https://media/${i}.m3u8`)?.length ?? 0) * 2).reduce((a, b) => a + b, 0)
    expect(retainedBytes).toBeLessThanOrEqual(8 * 1024 * 1024)
  })
  it('accepts a real playlist split across response chunks', async () => {
    const chunks = ['\uFEFF#EXT', 'M3U\n#EXTINF:6,\nsegment.ts\n#EXT-X-ENDLIST']
    const response = new Response(new ReadableStream({ start(controller) { chunks.forEach(chunk => controller.enqueue(new TextEncoder().encode(chunk))); controller.close() } }))
    vi.stubGlobal('fetch', vi.fn(async () => response))
    const result = await fetchAndParseM3u8('https://media/playlist.m3u8')
    expect(result.urls).toEqual(['https://media/segment.ts'])
  })
  it('rejects an oversized playlist stream even without Content-Length', async () => {
    const cancel = vi.fn()
    let chunks = 0
    const body = new ReadableStream<Uint8Array>({ pull(controller) {
      if (++chunks === 1) controller.enqueue(new TextEncoder().encode('#EXTM3U\n#'))
      else controller.enqueue(new Uint8Array(1024 * 1024).fill(120))
    }, cancel })
    vi.stubGlobal('fetch', vi.fn(async () => new Response(body)))
    await expect(fetchAndParseM3u8('https://media/huge-chunked.m3u8')).rejects.toThrow('exceeds')
    expect(cancel).toHaveBeenCalledOnce()
  })
  it('forwards cancellation to a playlist fetch waiting for response headers', async () => {
    const abort = new AbortController()
    const fetch = vi.fn((_url, options) => new Promise<Response>((_resolve, reject) => {
      options.signal.addEventListener('abort', () => reject(new Error('aborted')), { once: true })
    }))
    vi.stubGlobal('fetch', fetch)
    const pending = fetchAndParseM3u8('https://media/pending.m3u8', abort.signal)
    abort.abort()
    await expect(pending).rejects.toThrow('aborted')
    expect(fetch.mock.calls[0][1].signal).toBe(abort.signal)
  })
  it('resolves rewritten proxy paths against the page origin without doubling the proxy directory', async () => {
    vi.stubGlobal('location', { href: 'http://wails.localhost/index.html' })
    vi.stubGlobal('fetch', vi.fn(async () => new Response('#EXTM3U\n#EXTINF:6,\n/__cczj/hls?u=segment\n')))
    const result = await fetchAndParseM3u8('/__cczj/hls?u=playlist')
    expect(result.urls).toEqual(['http://wails.localhost/__cczj/hls?u=segment'])
  })
  it('keeps the playlist path for a query-only segment URI', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('#EXTM3U\n#EXTINF:6,\n?segment=1\n')))
    const result = await fetchAndParseM3u8('https://media/stream.php?playlist=1')
    expect(result.urls).toEqual(['https://media/stream.php?segment=1'])
  })
})
