import { CCZJ } from '../constants/cczj'
import { computed, onScopeDispose, ref } from 'vue'
import { defineStore } from 'pinia'
import { GetApplicationModeStatus, SetSetting } from '../api/app'
import { normalizeApiError } from '../api/errors'
import { createRequestScope } from '../api/requestScope'
import { reloadApplication } from '../api/runtime'
import { normalizeApplicationMode, modeReloadURL, type ApplicationMode } from './definitions'
import { modeSessions } from './sessionRuntime'
import { prepareModeBackground } from '../platform/bootAppearance'

export const APPLICATION_MODE_KEY = CCZJ.Settings.applicationMode
type Phase = 'idle' | 'confirming' | 'switching' | 'recovering' | 'reloading' | 'failed'
type Issue = '' | 'finishing' | 'switchFailed' | 'reloadFailed' | 'loadFailed' | 'savePending'

export const useApplicationMode = defineStore('applicationMode', () => {
  // This is the mounted shell's mode. The backend can commit the next mode
  // before document navigation succeeds, so retain that target separately.
  const mode = ref<ApplicationMode>('video')
  const loaded = ref(false)
  const phase = ref<Phase>('idle')
  const issue = ref<Issue>('')
  const warmResume = ref(false)
  const target = ref<ApplicationMode>()
  const reloadTarget = ref<ApplicationMode>()
  const switching = computed(() => phase.value !== 'idle' && phase.value !== 'failed')
  const blocked = computed(() => phase.value !== 'idle' && phase.value !== 'confirming')
  let loading: Promise<void> | undefined
  const lifetime = new AbortController()
  const requests = createRequestScope()
  onScopeDispose(() => { lifetime.abort(); requests.cancel() })

  async function status() {
    if (lifetime.signal.aborted) throw new DOMException('Application disposed', 'AbortError')
    const value = await requests.run(GetApplicationModeStatus)
    if (lifetime.signal.aborted) throw new DOMException('Application disposed', 'AbortError')
    if (!value || !['video', 'novel', 'comic'].includes(value.mode) || !['ready', 'switching', 'draining', 'stopping'].includes(value.phase)) {
      throw new Error('Invalid application mode status')
    }
    return { mode: normalizeApplicationMode(value.mode), phase: value.phase }
  }

  function load(): Promise<void> {
    if (loaded.value) return Promise.resolve()
    if (!loading) loading = status().then(value => {
      if (lifetime.signal.aborted) return
      mode.value = value.mode
      warmResume.value = modeSessions.activate(value.mode, window.location.href).warm
      loaded.value = true
      issue.value = value.phase === 'ready' ? '' : 'finishing'
      phase.value = value.phase === 'ready' ? 'idle' : 'failed'
    }).catch(() => {
      if (lifetime.signal.aborted) return
      issue.value = 'loadFailed'
      phase.value = 'failed'
    }).finally(() => { loading = undefined })
    return loading
  }

  async function navigate(next: ApplicationMode): Promise<void> {
    if (lifetime.signal.aborted) return
    reloadTarget.value = next
    phase.value = 'reloading'
    issue.value = ''
    try {
      prepareModeBackground(next)
      await reloadApplication(modeReloadURL(window.location.href, next, modeSessions.resumeRoute(next), modeSessions.prepare(next)), lifetime.signal)
    } catch {
      if (lifetime.signal.aborted) return
      issue.value = 'reloadFailed'
      phase.value = 'failed'
    }
  }

  async function reconcile(error: unknown): Promise<void> {
    if (lifetime.signal.aborted) return
    issue.value = normalizeApiError(error).code === 'MODE_FINISHING' ? 'finishing' : 'switchFailed'
    try {
      const value = await status()
      // A lost acknowledgement must not turn a committed switch into a rollback.
      if (value.mode !== mode.value) { await navigate(value.mode); return }
      if (value.phase !== 'ready') issue.value = 'finishing'
    } catch { /* Retain recovery actions until the host can be reached again. */ }
    phase.value = 'failed'
  }

  async function apply(next: ApplicationMode): Promise<void> {
    phase.value = next === mode.value ? 'recovering' : 'switching'
    issue.value = ''
    try {
      const value = await status()
      if (value.mode !== mode.value) { await navigate(value.mode); return }
      if (next !== mode.value && value.phase === 'ready') {
        let saved = true
        if (mode.value === 'novel') saved = await (await import('../novel/readingState')).useNovelReading().flush()
        if (mode.value === 'comic') saved = await (await import('../comic/state')).useComicState().flush()
        if (!saved) { issue.value = 'savePending'; phase.value = 'failed'; return }
      }
      if (lifetime.signal.aborted) return
      await requests.run(() => SetSetting(APPLICATION_MODE_KEY, next))
    } catch (error) {
      await reconcile(error)
      return
    }
    await navigate(next)
  }

  function shellFailed(): void {
    reloadTarget.value = mode.value
    issue.value = 'reloadFailed'
    phase.value = 'failed'
  }

  async function switchMode(next: ApplicationMode, confirmSwitch: () => Promise<boolean>): Promise<void> {
    if (phase.value !== 'idle' || next === mode.value) return
    phase.value = 'confirming'
    try {
      if (!await confirmSwitch()) { phase.value = 'idle'; return }
      target.value = next
      await apply(next)
    } catch (error) { await reconcile(error) }
  }

  async function retry(): Promise<void> {
    if (switching.value) return
    if (!loaded.value) {
      phase.value = 'recovering'
      await load()
      if (loaded.value) await navigate(mode.value)
      return
    }
    if (reloadTarget.value) { await navigate(reloadTarget.value); return }
    await apply(target.value ?? mode.value)
  }

  async function recover(): Promise<void> {
    if (switching.value || reloadTarget.value) return
    target.value = undefined
    phase.value = 'recovering'
    try {
      const value = await status()
      if (value.mode !== mode.value) { await navigate(value.mode); return }
      if (value.phase === 'ready') {
        if (issue.value === 'finishing') { await navigate(mode.value); return }
        issue.value = ''
        phase.value = 'idle'
        return
      }
    } catch (error) { await reconcile(error); return }
    await apply(mode.value)
  }

  return { mode, loaded, switching, blocked, phase, issue, target, reloadTarget, warmResume, load, switchMode, retry, recover, shellFailed }
})
