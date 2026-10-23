import { computed, nextTick, onBeforeUnmount, ref, shallowRef, watch } from 'vue'
import { GetSetting, SetSetting, normalizeApiError } from '../../api/app'
import { danmakuAPI, type DanmakuBindingValue, type DanmakuCandidate, type DanmakuEpisode, type DanmakuWindowValue, type DanmakuMatchAttempt } from '../../api/danmaku'
import { decodeDanmakuPreferences } from './preferences'
import { DANMAKU_SOURCE_SETTINGS_KEY, decodeSourceSettings, sourceEnabled } from './sources'

import { NETWORK_POLICY_KEY, decodeNetworkPolicy, networkRetryDelay } from '../../network/requestPolicy'
import { matchEpisode, normalizeWorkTitle } from './matching'

type Pending = { cancel?: () => unknown }
export function useDanmaku(deps: { key: () => string; current: () => number; title?: () => string; episode?: () => string; duration?: () => number; year?:()=>string; kind?:()=>string }) {
  const preferences = ref(decodeDanmakuPreferences(null))
  const sources = ref(decodeSourceSettings('')), sourcesReady = ref(false)
  const networkPolicy = ref(decodeNetworkPolicy(''))
  const binding = shallowRef<DanmakuBindingValue | null>(null)
  const window = shallowRef<DanmakuWindowValue | null>(null)
  const candidates = shallowRef<DanmakuCandidate[]>([])
  const episodes = shallowRef<DanmakuEpisode[]>([])
  const provider = ref('bilibili'), error = ref(''), discoveryBusy = ref(false), bindingBusy = ref(false), loading = ref(false), bindingReady = ref(false)
  const discoveryResult = ref<'idle' | 'searchEmpty' | 'episodesEmpty'>('idle')
  const windowError = ref('')
  const cooldownUntil = ref(0)
  const matching = ref(false), matchState = ref<'idle' | 'noMatch' | 'error'>('idle')
  const attempts=shallowRef<DanmakuMatchAttempt[]>([])
  const traceId=ref('')
  const matchIdentity = computed(() => JSON.stringify([
    deps.key(), sources.value.defaultProvider, normalizeWorkTitle(deps.title?.() || ''),
    deps.episode?.() || '', deps.year?.() || '', deps.kind?.() || '',
  ]))
  let autoAttempted = ''
  let generation = 0, windowGeneration = 0, discoveryGeneration = 0, disposed = false, retryAt = 0
  let windowRequest: Pending | null = null, lastRequested = -Infinity
  const pending = new Set<Pending>()
  const discoveries = new Set<Pending>()
  let saveTimer: ReturnType<typeof setTimeout> | undefined
  let saveTail: Promise<unknown> = Promise.resolve()
  const busy = computed(() => discoveryBusy.value || bindingBusy.value || matching.value)
  const preferencesReady = ref(false)
  let preferencesDirty = false
  const boundSourceEnabled = computed(() => !!binding.value && sourceEnabled(sources.value, binding.value.episode.provider))
  const status = computed(() => !preferences.value.enabled ? 'disabled' : matching.value ? 'matching' : windowError.value || matchState.value === 'error' ? 'error' : !preferencesReady.value || !sourcesReady.value || !bindingReady.value ? 'loading' : !binding.value ? !sources.value.defaultProvider ? 'sourceDisabled' : matchState.value === 'noMatch' ? 'noMatch' : 'unbound' : !boundSourceEnabled.value ? 'sourceDisabled' : loading.value ? 'loading' : !window.value ? 'waiting' : window.value.stale ? 'stale' : window.value.warnings.includes('segment_not_found') && !window.value.items.length ? 'missingSegment' : window.value.items.length ? 'ready' : 'empty')
  function cancel(request: Pending | null) { try { void Promise.resolve(request?.cancel?.()).catch(() => { }) } catch { } }
  async function own<T>(request: Promise<T> & Pending): Promise<T> { pending.add(request); try { return await request } finally { pending.delete(request) } }
  function message(value: unknown) { const normalized = normalizeApiError(value); if (normalized.code !== 'CANCELLED') error.value = normalized.message }
  function resetWindow() { windowGeneration++; cancel(windowRequest); windowRequest = null; window.value = null; loading.value = false; retryAt = 0; cooldownUntil.value = 0; lastRequested = -Infinity }
  async function fetchWindow(force = false) {
    if (disposed || matching.value || !preferencesReady.value || !sourcesReady.value || !preferences.value.enabled || !boundSourceEnabled.value || !binding.value || !bindingReady.value || discoveryBusy.value || bindingBusy.value || (Date.now() < retryAt && Math.abs(deps.current()*1000-lastRequested)<15000)) return
    const sourceTime = deps.current() * 1000 - binding.value.offsetMs
    if (sourceTime < -75000) return
    const current = window.value
    if (!force && current && sourceTime >= current.startMs && sourceTime < current.endMs - 15000) return
    if (windowRequest && Math.abs(deps.current() * 1000 - lastRequested) < 15000) return
    cancel(windowRequest); const token = ++windowGeneration, key = deps.key()
    const request = danmakuAPI.window(key, deps.current() * 1000, preferences.value.freshDays); windowRequest = request; lastRequested = deps.current() * 1000; loading.value = true
    try {
      const value = await own(request)
      if (disposed || token !== windowGeneration || key !== deps.key()) return
      if (value.provider !== binding.value?.episode.provider || value.episodeId !== binding.value?.episode.episodeId) return
      if (deps.title && value.warnings.some(warning => warning === 'pool_empty' || warning === 'segment_not_found')) {
        void autoMatch(true)
        return
      }
      window.value = value; windowError.value = ''; error.value = ''; cooldownUntil.value = 0; retryAt = 0
    } catch (value) { if (!disposed && token === windowGeneration) { const e = normalizeApiError(value); if (e.code !== 'CANCELLED') { windowError.value = e.message; message(value); retryAt = Date.now() + networkRetryDelay(networkPolicy.value.frequency,e.retryAfterSeconds); cooldownUntil.value = retryAt; if(deps.title)void autoMatch(true) } } }
    finally { if (token === windowGeneration) { windowRequest = null; loading.value = false } }
  }
  function savePreferences() {
    if (!preferencesReady.value || !preferencesDirty) return
    preferencesDirty = false
    const serialized = JSON.stringify(preferences.value)
    saveTail = saveTail.catch(() => { }).then(() => SetSetting('danmaku_preferences_v1', serialized)).catch(value => { preferencesDirty = true; if (!disposed) message(value) })
  }
  async function loadPreferences() { try { const value = await GetSetting('danmaku_preferences_v1'); if (!disposed) { preferences.value = decodeDanmakuPreferences(value); await nextTick(); preferencesReady.value = true } } catch (value) { if (!disposed) message(value) } }
  async function loadSources() { try { const [value, policy] = await Promise.all([GetSetting(DANMAKU_SOURCE_SETTINGS_KEY),GetSetting(NETWORK_POLICY_KEY)]); if (!disposed) { sources.value = decodeSourceSettings(value); networkPolicy.value = decodeNetworkPolicy(policy); if (!binding.value || !sourceEnabled(sources.value, provider.value)) provider.value = sources.value.defaultProvider; sourcesReady.value = true } } catch (value) { if (!disposed) message(value) } }
  void loadPreferences()
  void loadSources()
  watch(preferences, () => { if (!preferencesReady.value) return; preferencesDirty = true; clearTimeout(saveTimer); saveTimer = setTimeout(savePreferences, 350) }, { deep: true })
  async function loadBinding(key: string) {
    const token = ++generation; cancelDiscovery(); for (const request of pending) cancel(request)
    autoAttempted = ''; matchState.value = 'idle'
    attempts.value=[]
    traceId.value=''
    resetWindow(); binding.value = null; candidates.value = []; episodes.value = []; error.value = ''; windowError.value = ''; cooldownUntil.value = 0; bindingReady.value = false; discoveryBusy.value = false; bindingBusy.value = false
    if (!key) return
    // The backend owns multi-provider priority and durable failures. Do not
    // publish a prior binding before it has evaluated the current default.
    if(deps.title){bindingReady.value=true;return}
    try { const value = await own(danmakuAPI.binding(key)); if (!disposed && token === generation) {
      const accepted = value && (!deps.episode || matchEpisode([value.episode], deps.episode(), deps.duration?.() || 0)) ? value : null
      binding.value = accepted; bindingReady.value = true; provider.value = accepted && sourceEnabled(sources.value, accepted.episode.provider) ? accepted.episode.provider : sources.value.defaultProvider; void fetchWindow()
    } }
    catch (value) { if (!disposed && token === generation) { windowError.value = normalizeApiError(value).message; message(value) } }
  }
  watch(deps.key, loadBinding, { immediate: true })
  watch(() => Math.floor(deps.current()), () => void fetchWindow())
  watch(preferencesReady, ready => { if (ready) void fetchWindow() })
  watch(sourcesReady, ready => { if (ready) void fetchWindow() })
  watch(discoveryBusy, async active => { if (!active) { await nextTick(); void fetchWindow() } })
  watch(() => preferences.value.enabled, enabled => { resetWindow(); if (enabled) { autoAttempted = ''; if (!deps.title) void fetchWindow() } else cancelDiscovery() })
  watch(() => preferences.value.freshDays, () => { resetWindow(); void fetchWindow() })
  function cancelDiscovery() { discoveryGeneration++; for (const request of discoveries) cancel(request); discoveries.clear(); discoveryBusy.value = false; matching.value = false }
  function resetDiscovery() { cancelDiscovery(); candidates.value = []; episodes.value = []; discoveryResult.value = 'idle' }
  watch(provider, () => { if (!deps.title) resetDiscovery() })
  async function discover(kind: 'search' | 'episodes', query: string) {
    if (!sourcesReady.value || !sourceEnabled(sources.value, provider.value) || busy.value) return
    cancelDiscovery(); const token = discoveryGeneration, key = deps.key(), selected = provider.value; discoveryBusy.value = true; error.value = ''
    discoveryResult.value = 'idle'
    // Discovery is explicit user work. Suspend window reads instead of querying
    // a second provider alongside the current binding's content request.
    windowGeneration++; cancel(windowRequest); windowRequest = null; loading.value = false
    if (kind === 'search') { candidates.value = []; episodes.value = [] } else episodes.value = []
    try {
      if (kind === 'search') { const request = danmakuAPI.search(selected, query); discoveries.add(request); try { const values = await own(request); if (!disposed && token === discoveryGeneration && key === deps.key()) { candidates.value = values; if (!values.length) discoveryResult.value = 'searchEmpty' } } finally { discoveries.delete(request) } }
      else { const request = danmakuAPI.episodes(selected, query); discoveries.add(request); try { const values = await own(request); if (!disposed && token === discoveryGeneration && key === deps.key()) { episodes.value = values; if (!values.length) discoveryResult.value = 'episodesEmpty' } } finally { discoveries.delete(request) } }
    } catch (value) { if (!disposed && token === discoveryGeneration) message(value) }
    finally { if (token === discoveryGeneration) discoveryBusy.value = false }
  }
  async function bind(episode: DanmakuEpisode, offsetMs: number, automatic = false) {
    if ((!automatic && busy.value) || bindingBusy.value || !sourcesReady.value || !sourceEnabled(sources.value, episode.provider)) return false
    const key = deps.key(), token = generation, discoveryToken = discoveryGeneration; bindingBusy.value = true; error.value = ''
    windowGeneration++; cancel(windowRequest); windowRequest = null; loading.value = false
    const value: DanmakuBindingValue = { localKey: key, episode, offsetMs, confirmed: true }
    const request = danmakuAPI.bind(value)
    if (automatic) discoveries.add(request)
    try { await own(request); if (disposed || token !== generation || key !== deps.key() || (automatic && discoveryToken !== discoveryGeneration)) return false; binding.value = value; bindingReady.value = true; windowError.value = ''; resetWindow(); return true }
    catch (value) { if (!disposed && token === generation) message(value); return false }
    finally { discoveries.delete(request); if (token === generation) { bindingBusy.value = false; void fetchWindow() } }
  }
  async function autoMatch(force=false) {
    if (disposed || busy.value || !preferencesReady.value || !sourcesReady.value || !bindingReady.value || !preferences.value.enabled) return
    const title = deps.title?.().trim() || '', label = deps.episode?.() || '', selected = sources.value.defaultProvider
    if (!deps.key() || !title || !label || (deps.duration && (!Number.isFinite(deps.duration())||deps.duration() <= 0)) || !sourceEnabled(sources.value, selected)) return
    const identity = matchIdentity.value
    if (!force&&autoAttempted === identity) return
    autoAttempted = identity; cancelDiscovery()
    const token = discoveryGeneration, key = deps.key(); matching.value = true; error.value = ''; matchState.value = 'idle'
    const current = () => !disposed && key === deps.key() && token === discoveryGeneration && identity === matchIdentity.value
    try {
      windowGeneration++;cancel(windowRequest);windowRequest=null;loading.value=false
      const request=danmakuAPI.match({localKey:key,title,year:deps.year?.()||'',kind:deps.kind?.()||'',episodeLabel:label,durationMs:Math.round((deps.duration?.()||0)*1000),positionMs:Math.round(deps.current()*1000),freshDays:preferences.value.freshDays})
      discoveries.add(request)
      let result
      try{result=await own(request)}finally{discoveries.delete(request)}
      if(!current())return
      if(result.binding&&result.binding.localKey!==key)throw new Error('Mismatched video identity')
      binding.value=result.binding;window.value=result.window;attempts.value=result.attempts;windowError.value='';error.value='';retryAt=0;cooldownUntil.value=0
      traceId.value=result.traceId||''
      if(!result.binding)matchState.value='noMatch'
    } catch (value) {
      if (current() && normalizeApiError(value).code !== 'CANCELLED') {
        const failure = normalizeApiError(value)
        message(value); matchState.value = 'error'
        retryAt = Date.now() + networkRetryDelay(networkPolicy.value.frequency, failure.retryAfterSeconds); cooldownUntil.value = retryAt
      }
    } finally { if (current()) matching.value = false }
  }
  watch([matchIdentity, preferencesReady, sourcesReady, bindingReady, () => preferences.value.enabled, () => deps.duration?.()], () => {
    if (deps.title && autoAttempted && autoAttempted !== matchIdentity.value) {
      cancelDiscovery(); resetWindow(); binding.value = null; attempts.value = []; matchState.value = 'idle'
    }
    void autoMatch()
  })
  async function unbind() {
    if (busy.value) return
    const key = deps.key(), token = generation; bindingBusy.value = true
    try { await own(danmakuAPI.unbind(key)); if (!disposed && token === generation) { binding.value = null; resetWindow() } }
    catch (value) { if (!disposed && token === generation) message(value) } finally { if (token === generation) bindingBusy.value = false }
  }
  function retry() { if(Date.now()<retryAt)return; error.value = ''; windowError.value = ''; if (!preferencesReady.value) void loadPreferences(); if (!sourcesReady.value) void loadSources(); if (!bindingReady.value) void loadBinding(deps.key()); if (preferencesDirty) savePreferences(); if (!binding.value) { autoAttempted = ''; void autoMatch() } else void fetchWindow(true) }
  onBeforeUnmount(() => { disposed = true; generation++; cancelDiscovery(); resetWindow(); for (const request of pending) cancel(request); pending.clear(); clearTimeout(saveTimer); savePreferences(); window.value = null })
  return { preferences, preferencesReady, sources, sourcesReady, boundSourceEnabled, binding, window, candidates, episodes, provider, error, busy, loading, status, cooldownUntil, attempts, traceId, discoveryResult, bind, unbind, cancelDiscovery, search: (query: string) => discover('search', query), resolve: (ref: string) => discover('episodes', ref), retry }
}
