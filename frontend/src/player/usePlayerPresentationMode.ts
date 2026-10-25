import { nextTick, onMounted, onBeforeUnmount, ref, type Ref } from 'vue'
import type { PlayerPlatform } from './platform'
import { ApiFailure } from '../api/errors'

let presentationOwner: symbol | undefined

/** Session ownership and late-result guards are shared; host operations vary
 * behind the platform adapter and do not create media or HLS resources. */
export function usePlayerPresentationMode(deps: {
  platform: PlayerPlatform; wrapperRef: Ref<HTMLDivElement | undefined>; video: () => HTMLVideoElement | null
  fullscreenRoot?: () => HTMLElement | undefined
  visible: () => void; failure: (operation:string,error:unknown) => void; unavailable: (capability:'fullscreen'|'pip') => void
}) {
  const { platform } = deps
  const owner = Symbol('player-presentation')
  presentationOwner = owner
  let disposed = false, fullscreenGeneration = 0, pipGeneration = 0
  let stopElementPip: (() => void) | undefined
  const isFullscreen = ref(false), isPiP = ref(false)
  const owns = () => !disposed && presentationOwner === owner
  const fullscreenRoot = () => deps.fullscreenRoot?.() ?? deps.wrapperRef.value
  function reflectFullscreen(value: boolean): void {
    isFullscreen.value = value
    if (value) document.body.setAttribute('data-player-fullscreen', '1')
    else document.body.removeAttribute('data-player-fullscreen')
  }
  async function toggleFullscreen(): Promise<void> {
    if (!owns()) return
    const token = ++fullscreenGeneration
    const current = () => owns() && token === fullscreenGeneration
    const root = fullscreenRoot()
    try {
      if (isFullscreen.value) {
        await platform.fullscreen.exit(root, current)
        if (!current()) return
        reflectFullscreen(false)
      } else {
        const entered = await platform.fullscreen.enter(root, current)
        if (!current()) {
          await platform.fullscreen.release(root, () => presentationOwner === undefined)
          return
        }
        if (!entered) { deps.unavailable('fullscreen'); return }
        reflectFullscreen(true)
      }
      deps.visible()
    } catch (error) { if (current()) deps.failure('fullscreen',error) }
  }
  function onFsChange(): void {
    if (owns()) reflectFullscreen(platform.fullscreen.browserActive(fullscreenRoot()))
  }
  function onNativePip(event: Event): void {
    if (!owns()) return
    isPiP.value = Boolean((event as CustomEvent).detail)
    if (!isPiP.value) document.body.removeAttribute('data-player-pip')
  }
  function onNativePipError(event: Event): void {
    if (!owns()) return
    isPiP.value = false
    document.body.removeAttribute('data-player-pip')
    const code = (event as CustomEvent).detail === 'permission' ? 'PERMISSION_DENIED' : 'UNAVAILABLE'
    deps.failure('pip',new ApiFailure({code,message:'Native picture-in-picture request was rejected'},undefined))
  }
  async function togglePiP(): Promise<void> {
    if (!owns()) return
    const token = ++pipGeneration
    const current = () => owns() && token === pipGeneration
    const video = deps.video()
    if (!video || video.readyState < 1) return
    try {
      if (platform.pip.active(video)) {
        await platform.pip.exit(video)
        if (current()) isPiP.value = false
        return
      }
      if (!platform.pip.supported(video)) { deps.unavailable('pip'); return }
      if (platform.pip.kind === 'device') {
        if (isFullscreen.value) await toggleFullscreen()
        if (!current()) return
        document.body.setAttribute('data-player-pip', '1')
        await nextTick()
        if (!current()) return
      }
      const entered = await platform.pip.enter(video)
      if (!current()) { await platform.pip.exit(video); return }
      if (!entered) { document.body.removeAttribute('data-player-pip'); deps.unavailable('pip'); return }
      if (platform.pip.kind === 'element') isPiP.value = true
    } catch (error) {
      if (!current()) return
      isPiP.value = false
      document.body.removeAttribute('data-player-pip')
      deps.failure('pip',error)
    }
  }
  onMounted(() => {
    if (platform.fullscreen.kind === 'element') {
      document.addEventListener('fullscreenchange', onFsChange)
      document.addEventListener('webkitfullscreenchange', onFsChange)
    }
    if (platform.pip.kind === 'device') {
      window.addEventListener('cczj:player-pip', onNativePip)
      window.addEventListener('cczj:player-pip-error', onNativePipError)
    } else {
      void nextTick().then(() => {
        const video = deps.video()
        if (!owns() || !video) return
        const changed = () => { if (owns()) isPiP.value = platform.pip.active(video) }
        const events = ['enterpictureinpicture', 'leavepictureinpicture', 'webkitpresentationmodechanged']
        for (const event of events) video.addEventListener(event, changed)
        stopElementPip = () => { for (const event of events) video.removeEventListener(event, changed) }
      })
    }
  })
  onBeforeUnmount(() => {
    disposed = true; fullscreenGeneration++; pipGeneration++
    stopElementPip?.()
    document.removeEventListener('fullscreenchange', onFsChange)
    document.removeEventListener('webkitfullscreenchange', onFsChange)
    window.removeEventListener('cczj:player-pip', onNativePip)
    window.removeEventListener('cczj:player-pip-error', onNativePipError)
    if (presentationOwner !== owner) return
    presentationOwner = undefined
    void platform.pip.exit(deps.video()).catch(() => {})
    void platform.fullscreen.release(fullscreenRoot(), () => presentationOwner === undefined).catch(() => {})
    document.body.removeAttribute('data-player-fullscreen')
    document.body.removeAttribute('data-player-pip')
  })
  return { isFullscreen, isPiP, toggleFullscreen, togglePiP }
}
