import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../src/api/app', () => ({ ProxyImage: vi.fn() }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))

beforeEach(() => {
  vi.resetModules()
  vi.useFakeTimers()
  vi.stubGlobal('window', { setTimeout, clearTimeout, addEventListener: vi.fn() })
  vi.stubGlobal('localStorage', { getItem: () => null, setItem: vi.fn() })
})
afterEach(() => { vi.clearAllTimers(); vi.useRealTimers(); vi.unstubAllGlobals() })

describe('image proxy retained bytes', () => {
  it('evicts by retained bytes even below the existing 80-image count limit', async () => {
    const { ProxyImage } = await import('../src/api/app')
    vi.mocked(ProxyImage).mockImplementation(async url => `data:image/jpeg;base64,${url}${'a'.repeat(2 * 1024 * 1024)}`)
    const { getProxiedImageUrl, imageProxyStats } = await import('../src/utils')
    for (let i = 0; i < 20; i++) await getProxiedImageUrl(`https://posters.example/${i}`)
    await getProxiedImageUrl('https://posters.example/0')
    expect(ProxyImage).toHaveBeenCalledTimes(21)
    const stats = imageProxyStats() as unknown as { bytes: number; limitBytes: number; entries: number }
    expect(stats.bytes).toBeGreaterThan(0)
    expect(stats.bytes).toBeLessThanOrEqual(8 * 1024 * 1024)
    expect(stats.entries).toBeLessThan(20)
  })
  it('returns oversized images without retaining them or evicting a small cached image', async () => {
    const { ProxyImage } = await import('../src/api/app')
    vi.mocked(ProxyImage).mockImplementation(async url => url.endsWith('huge') ? `data:image/jpeg;base64,${'x'.repeat(5 * 1024 * 1024)}` : 'data:image/jpeg;base64,small')
    const { getProxiedImageUrl, imageProxyStats } = await import('../src/utils')
    await getProxiedImageUrl('https://posters.example/small')
    const before = imageProxyStats().bytes
    expect(await getProxiedImageUrl('https://posters.example/huge')).toHaveLength(5 * 1024 * 1024 + 23)
    expect(imageProxyStats().bytes).toBe(before)
    await getProxiedImageUrl('https://posters.example/small')
    expect(ProxyImage).toHaveBeenCalledTimes(2)
  })
  it('deduplicates concurrent proxy requests and counts their retained bytes once', async () => {
    const { ProxyImage } = await import('../src/api/app')
    vi.mocked(ProxyImage).mockResolvedValue('data:image/png;base64,abc')
    const { getProxiedImageUrl, imageProxyStats } = await import('../src/utils')
    const url = 'https://posters.example/same'
    await Promise.all([getProxiedImageUrl(url), getProxiedImageUrl(url)])
    expect(ProxyImage).toHaveBeenCalledOnce()
    expect(imageProxyStats().bytes).toBe((url.length + 'data:image/png;base64,abc'.length) * 2)
  })
  it('bounds failed URL cooldowns instead of retaining every failed poster', async () => {
    const { ProxyImage } = await import('../src/api/app')
    vi.mocked(ProxyImage).mockResolvedValue('')
    const { getProxiedImageUrl, imageProxyStats } = await import('../src/utils')
    for (let i = 0; i < 300; i++) await getProxiedImageUrl(`https://posters.example/failure/${i}`)
    expect(imageProxyStats().failureEntries).toBe(256)
    await getProxiedImageUrl('https://posters.example/failure/299')
    expect(ProxyImage).toHaveBeenCalledTimes(300)
  })
})
