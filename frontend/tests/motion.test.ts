import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { DEFAULT_MOTION, motionVariables, normalizeMotionOptions } from '../src/motion/preferences'
import { useMotionStore } from '../src/stores/motion'
const api = vi.hoisted(() => ({ GetSetting: vi.fn(async (_key: string) => ''), SetSetting: vi.fn(async (_key: string, _value: string) => {}), failure: vi.fn() }))
vi.mock('../src/api/app', () => ({ GetSetting: api.GetSetting, SetSetting: api.SetSetting }))
vi.mock('../src/stores/plugins', () => ({ usePluginStore: () => ({ readyPacks: [{ id: 'motion-effects' }], ensureLoaded: async () => {} }) }))
vi.mock('../src/stores/error', () => ({ useErrorStore: () => ({ fromError: api.failure }) }))
vi.mock('../src/locales', () => ({ tr: (key: string) => key }))
let store: ReturnType<typeof useMotionStore> | undefined
let variables: Map<string, string>
const media = { matches: false, addEventListener: vi.fn(), removeEventListener: vi.fn() }
beforeEach(() => {
  vi.clearAllMocks()
  api.GetSetting.mockResolvedValue(''); api.SetSetting.mockResolvedValue(undefined)
  variables = new Map(); media.matches = false
  setActivePinia(createPinia())
  vi.stubGlobal('window', { matchMedia: () => media })
  vi.stubGlobal('document', { documentElement: { style: { setProperty: (key: string, value: string) => variables.set(key, value), removeProperty: (key: string) => variables.delete(key) }, setAttribute: vi.fn(), removeAttribute: vi.fn() } })
  vi.stubGlobal('getComputedStyle', () => ({ getPropertyValue: (key: string) => variables.get(key) || '' }))
})
afterEach(() => { store?.$dispose(); store = undefined; vi.unstubAllGlobals() })
describe('motion preferences and persistence', () => {
  it('bounds corrupted settings and rejects string numbers or unknown switches', () => {
    expect(normalizeMotionOptions({ speed: 9999, intensity: -100, pages: 'false', style: 'unknown' })).toMatchObject({ speed: 160, intensity: 0, pages: true, style: 'balanced' })
    expect(normalizeMotionOptions({ speed: NaN, intensity: Infinity })).toEqual(DEFAULT_MOTION)
    expect(normalizeMotionOptions(['bad'])).toEqual(DEFAULT_MOTION)
  })
  it('keeps fade-only motion at zero intensity and changes real durations with speed', () => {
    const zero = motionVariables({ ...DEFAULT_MOTION, intensity: 0 })
    expect(zero['--cczj-motion-distance']).toBe('0px')
    expect(zero['--cczj-motion-enter-scale']).toBe('1')
    expect(motionVariables({ ...DEFAULT_MOTION, speed: 160 })['--cczj-motion-normal']).toBe('138ms')
  })
  it('deduplicates startup reads without accumulating system preference listeners', async () => {
    store = useMotionStore()
    await Promise.all([store.load(), store.load(), store.load()])
    expect(api.GetSetting).toHaveBeenCalledTimes(2)
    expect(api.SetSetting).not.toHaveBeenCalled()
    expect(media.addEventListener).toHaveBeenCalledTimes(1)
    store.$dispose(); store = undefined
    expect(media.removeEventListener).toHaveBeenCalledTimes(1)
  })
  it('serializes rapid edits so a slow older write cannot replace the latest choice', async () => {
    let release!: () => void
    api.SetSetting.mockImplementationOnce(() => new Promise<void>(resolve => { release = resolve }))
    store = useMotionStore(); await store.load()
    const first = store.setOptions({ speed: 60 }), second = store.setOptions({ speed: 140 })
    await vi.waitFor(() => expect(api.SetSetting).toHaveBeenCalledTimes(1))
    expect(store.options.speed).toBe(140)
    release(); await Promise.all([first, second])
    expect(JSON.parse(api.SetSetting.mock.calls[1][1] as string).speed).toBe(140)
  })
  it('rolls back failed writes and respects system reduced motion with inline CSS tokens', async () => {
    store = useMotionStore(); await store.load()
    api.SetSetting.mockRejectedValueOnce(new Error('disk unavailable'))
    await store.setOptions({ speed: 160 })
    expect(store.options.speed).toBe(100)
    expect(api.failure).toHaveBeenCalledOnce()
    media.matches = true
    media.addEventListener.mock.calls[0][1]()
    expect(store.reduced).toBe(true)
    expect(store.tokenMs('--cczj-motion-normal')).toBe(1)
    expect(store.dipMs()).toBe(0)
  })
  it('restores the last durable preference when two consecutive rapid writes fail', async () => {
    store = useMotionStore(); await store.load()
    api.SetSetting.mockRejectedValueOnce(new Error('first write failed')).mockRejectedValueOnce(new Error('second write failed'))
    await Promise.all([store.setOptions({ speed: 60 }), store.setOptions({ speed: 140 })])
    expect(store.options.speed).toBe(100)
  })
})
