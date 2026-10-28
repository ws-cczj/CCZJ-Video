import { onBeforeUnmount, onMounted } from 'vue'
import { isMobileRuntime } from '../api/runtime'
let mediaControlsOwner: symbol | undefined

/** Chromium owns audio focus. The host supplements it with visibility/noisy
 * events; only the same session may resume after a host interruption. */
export function usePlaybackInterruptions(deps: {
  video: () => HTMLVideoElement | null; current: () => () => boolean
  pip: () => boolean; play: () => void; cancel: () => void; save: () => void
}) {
  let wanted = true, blocked = false, foreground = true
  let suspended: {video: HTMLVideoElement; current: () => boolean} | undefined
  function cancelResume(): void { suspended = undefined }
  function intent(play: boolean): void { wanted = play; blocked = !play; cancelResume() }
  function reset(play = true): void { wanted = play; blocked = !play; cancelResume() }
  function pause(permanent: boolean): void {
    const video = deps.video()
    if (permanent) { wanted = false; cancelResume() }
    else if (!suspended && video && !video.paused && wanted) suspended = {video, current:deps.current()}
    blocked = true
    deps.cancel(); deps.save(); video?.pause()
  }
  function resume(): void {
    if (!foreground || (document.hidden && !deps.pip())) return
    const saved = suspended
    suspended = undefined
    if (!wanted) return
    blocked = false
    if (saved && saved.current() && deps.video() === saved.video && saved.video.paused && !saved.video.ended) deps.play()
  }
  function visibility(): void {
    if (document.hidden && !deps.pip()) pause(false)
    else resume()
  }
  function host(event: Event): void {
    const detail: unknown = (event as CustomEvent).detail
    if (detail === 'noisy') pause(true)
    else if (detail === 'background') { foreground = false; if (!deps.pip()) pause(false) }
    else if (detail === 'foreground') { foreground = true; resume() }
  }
  const enabled = isMobileRuntime()
  const owner=Symbol('playback-interruptions')
  onMounted(() => {
    if (!enabled) return
    mediaControlsOwner=owner
    document.addEventListener('visibilitychange', visibility)
    window.addEventListener('cczj:player-interruption', host)
    if(navigator.mediaSession){
      try{
        navigator.mediaSession.setActionHandler('pause',()=>{intent(false);deps.cancel();deps.save();deps.video()?.pause()})
        navigator.mediaSession.setActionHandler('play',()=>{
          if(!foreground&&!deps.pip())return
          intent(true);deps.play()
        })
      }catch{ /* Older WebViews may omit media-session actions. */ }
    }
  })
  onBeforeUnmount(() => {
    cancelResume()
    document.removeEventListener('visibilitychange', visibility)
    window.removeEventListener('cczj:player-interruption', host)
    if(mediaControlsOwner===owner){
      mediaControlsOwner=undefined
      try{navigator.mediaSession?.setActionHandler('pause',null);navigator.mediaSession?.setActionHandler('play',null)}catch{ /* Optional host capability. */ }
    }
  })
  return {intent, reset, cancelResume, allowed:() => !enabled || (mediaControlsOwner===owner && !blocked && (foreground || deps.pip()) && (!document.hidden || deps.pip()))}
}
