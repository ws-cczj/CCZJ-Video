/** User motion preferences share one persisted owner and one CSS application point. */
import { ref, watch, onScopeDispose } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../api/app'
import { usePluginStore } from './plugins'
import { useErrorStore } from './error'
import { tr } from '../locales'
import { DEFAULT_MOTION, normalizeMotionOptions, motionVariables, type MotionOptions } from '../motion/preferences'

export const MOTION_SETTING_KEY = 'ui_motion'
export const MOTION_OPTIONS_KEY = 'ui_motion_options'
export const MOTION_PACK_ID = 'motion-effects'
export const useMotionStore = defineStore('motion', () => {
  const pluginStore = usePluginStore()
  const enabled = ref(true)
  const loaded = ref(false)
  const packReady = ref(false)
  const options = ref<MotionOptions>({ ...DEFAULT_MOTION })
  const media = typeof window.matchMedia === 'function' ? window.matchMedia('(prefers-reduced-motion: reduce)') : undefined
  const reduced = ref(media?.matches || false)
  let savedOptions: MotionOptions = { ...DEFAULT_MOTION }
  let savedEnabled = true
  let loading: Promise<void> | undefined
  let writes = Promise.resolve()
  const revisions = { enabled: 0, options: 0 }
  function apply(): void {
    const root = document.documentElement
    const keys = ['pages', 'overlays', 'feedback'] as const
    if (enabled.value || !packReady.value) root.removeAttribute('data-cczj-motion')
    else root.setAttribute('data-cczj-motion', 'off')
    for (const key of keys) {
      if (packReady.value && !options.value[key]) root.setAttribute(`data-motion-${key}`, 'off')
      else root.removeAttribute(`data-motion-${key}`)
    }
    const variables = motionVariables(options.value)
    const cut = reduced.value || (!enabled.value && packReady.value)
    for (const [name, value] of Object.entries(variables)) {
      if (!packReady.value) { root.style.removeProperty(name); continue }
      const next = cut && name === '--cczj-motion-distance' ? '0px' : cut && name === '--cczj-motion-enter-scale' ? '1' : cut && ['--cczj-motion-fast', '--cczj-motion-normal', '--cczj-motion-slow', '--cczj-motion-emphasis', '--cczj-motion-carousel'].includes(name) ? '1ms'
        : cut && name === '--cczj-motion-stagger' ? '0ms' : value
      root.style.setProperty(name, next)
    }
  }
  watch(() => pluginStore.readyPacks.some(pack => pack.id === MOTION_PACK_ID), present => { packReady.value = present; apply() }, { immediate: true })
  const onReduced = () => { reduced.value = media?.matches || false; apply() }
  media?.addEventListener('change', onReduced)
  onScopeDispose(() => media?.removeEventListener('change', onReduced))
  function load(): Promise<void> {
    if (loaded.value) return Promise.resolve()
    if (loading) return loading
    loading = Promise.all([GetSetting(MOTION_SETTING_KEY).catch(() => ''), GetSetting(MOTION_OPTIONS_KEY).catch(() => '')]).then(([raw, config]) => {
      enabled.value = String(raw ?? '').trim() !== '0'
      try { options.value = normalizeMotionOptions(JSON.parse(config || '{}')) } catch { options.value = { ...DEFAULT_MOTION } }
      savedOptions = { ...options.value }; savedEnabled = enabled.value
      loaded.value = true
      apply()
      void pluginStore.ensureLoaded()
    }).finally(() => { loading = undefined })
    return loading
  }
  function persist(key: string, value: string, success: () => void, rollback: () => void): Promise<void> {
    const write = writes.then(async () => {
      try { await SetSetting(key, value); success() } catch (error) {
        rollback()
        useErrorStore().fromError(tr('motion.saveFailed'), error, 'Motion.settings')
      }
    })
    writes = write.catch(() => {})
    return write
  }
  function setEnabled(next: boolean): Promise<void> {
    if (enabled.value === next) return Promise.resolve()
    const revision = ++revisions.enabled
    enabled.value = next; apply()
    return persist(MOTION_SETTING_KEY, next ? '1' : '0', () => { savedEnabled = next }, () => { if (revision === revisions.enabled) { enabled.value = savedEnabled; apply() } })
  }
  function setOptions(next: Partial<MotionOptions>): Promise<void> {
    const revision = ++revisions.options
    options.value = normalizeMotionOptions({ ...options.value, ...next }); apply()
    const submitted = { ...options.value }
    return persist(MOTION_OPTIONS_KEY, JSON.stringify(submitted), () => { savedOptions = submitted }, () => { if (revision === revisions.options) { options.value = { ...savedOptions }; apply() } })
  }
  function reset(): Promise<void> { return setOptions({ ...DEFAULT_MOTION }) }
  function tokenMs(name: string): number {
    void options.value; void reduced.value; void enabled.value; void packReady.value
    const raw = getComputedStyle(document.documentElement).getPropertyValue(name).trim()
    const value = Number.parseFloat(raw)
    return Number.isFinite(value) && value > 0 ? raw.endsWith('ms') ? value : value * 1000 : 0
  }
  function dipMs(): number { return !enabled.value || !packReady.value || reduced.value ? 0 : Math.min(tokenMs('--cczj-motion-fast'), 300) }
  return { enabled, loaded, packReady, options, reduced, load, setEnabled, setOptions, reset, dipMs, tokenMs }
})
