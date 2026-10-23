import { ref } from 'vue'
import { defineStore } from 'pinia'
import { GetSetting, SetSetting } from '../api/app'
import { reloadApplication } from '../api/runtime'
import { normalizeApplicationMode, modeReloadURL, type ApplicationMode } from './definitions'
import { modeSessions } from './sessionRuntime'
import { prepareModeBackground } from '../platform/bootAppearance'

export const APPLICATION_MODE_KEY = 'application_mode'
export const useApplicationMode = defineStore('applicationMode', () => {
  const mode = ref<ApplicationMode>('video')
  const loaded = ref(false)
  const switching = ref(false)
  const warmResume = ref(false)
  let loading: Promise<void> | undefined
  function load(): Promise<void> {
    if (loaded.value) return Promise.resolve()
    if (!loading) loading = GetSetting(APPLICATION_MODE_KEY).then(raw => { mode.value = normalizeApplicationMode(raw); warmResume.value = modeSessions.activate(mode.value,window.location.href).warm }).catch(() => {}).finally(() => { loaded.value = true; loading = undefined })
    return loading
  }
  async function switchMode(next: ApplicationMode, confirmSwitch:()=>Promise<boolean>): Promise<void> {
    if (switching.value || next === mode.value) return
    switching.value = true
    try {
      if (!await confirmSwitch()) { switching.value=false; return }
      await SetSetting(APPLICATION_MODE_KEY, next)
      prepareModeBackground(next)
      // Do not replace the live shell before persistence succeeds. Full document
      // navigation replaces this document and its retained component state.
      reloadApplication(modeReloadURL(window.location.href, next,modeSessions.resumeRoute(next),modeSessions.prepare(next)))
    } catch (error) { switching.value = false; throw error }
  }
  return { mode, loaded, switching, warmResume, load, switchMode }
})
