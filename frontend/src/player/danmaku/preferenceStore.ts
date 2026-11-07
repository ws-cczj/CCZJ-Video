import { computed, onScopeDispose, ref, shallowRef, watch } from 'vue'
import { acceptHMRUpdate, defineStore } from 'pinia'
import { CCZJ } from '../../constants/cczj'
import { GetSetting, SetSetting } from '../../api/app'
import { createSettingsQueue } from '../../api/settingsQueue'
import { decodeDanmakuPreferences, type DanmakuPreferences } from './preferences'

/** Display and cache-policy editors share the same current value and writer. */
export const useDanmakuPreferencesStore = defineStore('danmaku-preferences', () => {
  const preferences = ref(decodeDanmakuPreferences(null)), ready = ref(false)
  const loadError = shallowRef<unknown>(null), saveError = shallowRef<unknown>(null)
  const dirty = ref(false), pending = ref(0)
  const error = computed(() => saveError.value ?? loadError.value)
  const unsaved = computed(() => dirty.value || pending.value > 0)
  let loading: Promise<boolean> | undefined, request: ReturnType<typeof GetSetting> | undefined
  let timer: ReturnType<typeof setTimeout> | undefined, disposed = false
  const writes = createSettingsQueue((key, value) => SetSetting(key, value), (count, failure) => {
    pending.value = count; saveError.value = failure
  })
  function load(): Promise<boolean> {
    if (ready.value) return Promise.resolve(true)
    if (loading) return loading
    if (disposed) return Promise.resolve(false)
    request = GetSetting(CCZJ.Settings.danmakuPreferences)
    loading = request.then(raw => {
      if (disposed) return false
      preferences.value = decodeDanmakuPreferences(raw)
      loadError.value = null; ready.value = true
      return true
    }).catch(failure => {
      if (!disposed) loadError.value = failure
      return false
    }).finally(() => { loading = undefined; request = undefined })
    return loading
  }
  async function flush(): Promise<boolean> {
    clearTimeout(timer); timer = undefined
    if (!ready.value) return false
    let result: boolean
    if (dirty.value) {
      dirty.value = false
      result = await writes.set(CCZJ.Settings.danmakuPreferences, JSON.stringify(preferences.value))
    } else result = await writes.retry()
    // An edit queued while a previous flush was settling still needs its own drain.
    while (result && writes.pending()) result = await writes.retry()
    return result
  }
  const stopWatching = watch(preferences, () => {
    if (!ready.value || disposed) return
    dirty.value = true; clearTimeout(timer)
    timer = setTimeout(() => { timer = undefined; void flush() }, 350)
  }, { deep: true, flush: 'sync' })
  async function update(patch: Partial<DanmakuPreferences>): Promise<boolean> {
    if (disposed || !await load()) return false
    preferences.value = decodeDanmakuPreferences({ ...preferences.value, ...patch })
    return flush()
  }
  async function retry(): Promise<boolean> { return await load() && await flush() }
  function pageHidden(): void { if (document.hidden) void flush() }
  function pageLeaving(): void { void flush() }
  document.addEventListener('visibilitychange', pageHidden)
  window.addEventListener('pagehide', pageLeaving)
  function dispose(): void {
    if (disposed) return
    stopWatching(); void flush(); disposed = true
    document.removeEventListener('visibilitychange', pageHidden)
    window.removeEventListener('pagehide', pageLeaving)
    try { void Promise.resolve(request?.cancel?.()).catch(() => {}) } catch { /* Request may have settled. */ }
  }
  onScopeDispose(dispose)
  if (import.meta.hot) import.meta.hot.dispose(dispose)
  return { preferences, ready, error, unsaved, load, update, flush, retry }
})

if (import.meta.hot) import.meta.hot.accept(acceptHMRUpdate(useDanmakuPreferencesStore, import.meta.hot))
