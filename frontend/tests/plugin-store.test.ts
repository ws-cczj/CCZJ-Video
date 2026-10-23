import { beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePluginStore } from '../src/stores/plugins'
import { scriptTrust } from '../src/plugins/trust'

const api = vi.hoisted(() => ({
  GetPluginDirectory: vi.fn(async () => 'plugins'), ListPlugins: vi.fn(async () => []),
  RescanPlugins: vi.fn(async () => []), SetPluginEnabled: vi.fn(async () => {}),
  UninstallPlugin: vi.fn(async () => {}), ReadPluginFile: vi.fn(async () => ''), ReadPluginAsset: vi.fn(async () => ''),
}))
vi.mock('../src/api/app', () => ({ ...api, normalizeApiError: (error: Error) => ({ message: error.message }) }))
vi.mock('../src/locales', () => ({ default: { global: { locale: { value: 'en' } } }, tr: (key: string) => key }))
vi.mock('../src/plugins/trust', () => ({ scriptTrust: { requireTrust: vi.fn(async () => true), revoke: vi.fn(async () => {}) } }))
function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>(yes => { resolve = yes })
  return { promise, resolve }
}
beforeEach(() => { setActivePinia(createPinia()); Object.values(api).forEach(fn => fn.mockClear()) })

describe('plugin registry and file cache generations', () => {
  it('refreshes the registry after disk uninstall even when trust cleanup fails', async () => {
    const store = usePluginStore()
    store.packs = [{ id: 'removed', status: 'ready', kind: 'script' }] as any
    vi.mocked(scriptTrust.revoke).mockRejectedValueOnce(new Error('settings write failed'))
    const message = await store.uninstall('removed')
    expect(message).not.toBe('')
    expect(store.packs).toEqual([])
  })
  it('does not replace a newer rescan with a late cached registry response', async () => {
    const store = usePluginStore()
    const old = deferred<any[]>()
    api.ListPlugins.mockReturnValueOnce(old.promise)
    api.RescanPlugins.mockResolvedValueOnce([{ id: 'new', status: 'ready' }] as any)
    const first = store.refresh()
    await vi.waitFor(() => expect(api.ListPlugins).toHaveBeenCalled())
    await store.refresh(true)
    old.resolve([{ id: 'old', status: 'ready' }])
    await first
    expect(store.packs.map(pack => pack.id)).toEqual(['new'])
  })
  it('does not let a late text read refill the cache after a rescan', async () => {
    const store = usePluginStore()
    const old = deferred<string>()
    api.ReadPluginFile.mockReturnValueOnce(old.promise).mockResolvedValue('new source')
    const pending = store.readText('pack', 'main.js')
    await store.refresh(true)
    expect(await store.readText('pack', 'main.js')).toBe('new source')
    old.resolve('old source')
    await pending
    expect(await store.readText('pack', 'main.js')).toBe('new source')
  })
  it('does not keep every large image indefinitely in the asset cache', async () => {
    const store = usePluginStore()
    api.ReadPluginAsset.mockResolvedValue('x'.repeat(1024 * 1024))
    for (let i = 0; i < 20; i++) await store.readAsset('pack', `${i}.png`)
    const calls = api.ReadPluginAsset.mock.calls.length
    await store.readAsset('pack', '0.png')
    expect(api.ReadPluginAsset.mock.calls.length).toBe(calls + 1)
  })
})
