import { ref } from 'vue'
import type { createPlaybackSession } from './playbackSession'

/** Browser autoplay fallback belongs only to a denied automatic play request.
 * A pause, media switch or user click must never silently mute the video. */
export function createPlaybackControl(deps: {
  getVideo: () => HTMLVideoElement | null
  onMuted: () => void
  onUnsupported: () => void
  autoplay?: {
    session: ReturnType<typeof createPlaybackSession>
    ready: () => boolean
    enabled: () => boolean
    state: (state: 'requested' | 'blocked') => void
  }
}) {
  let generation = 0
  let autoGeneration = 0, retries = 0, resumeRequested = false
  const starting = ref(false)

  function cancel(): void {
    generation++
    autoGeneration++
    starting.value = false
    resumeRequested = false
  }
  function reset(): void { cancel(); retries = 0 }
  function requestResume(): void { resumeRequested = true }

  async function play(auto: boolean): Promise<'playing' | 'interrupted' | 'blocked'> {
    const video = deps.getVideo()
    if (!video) return 'interrupted'
    const token = ++generation
    try {
      await video.play()
      return token === generation && deps.getVideo() === video ? 'playing' : 'interrupted'
    } catch (error) {
      if (token !== generation || deps.getVideo() !== video) return 'interrupted'
      const name = (error as { name?: string })?.name
      if (name === 'AbortError') return 'interrupted'
      if (name === 'NotSupportedError') {
        deps.onUnsupported()
      } else if (auto && name === 'NotAllowedError') {
        video.muted = true
        deps.onMuted()
        try {
          await video.play()
          return token === generation && deps.getVideo() === video ? 'playing' : 'interrupted'
        } catch (error) {
          if (token !== generation || deps.getVideo() !== video || (error as {name?:string})?.name === 'AbortError') return 'interrupted'
        }
      }
      return 'blocked'
    }
  }

  function tryAutoplay(video: HTMLVideoElement): void {
    const automatic = deps.autoplay
    if (!automatic || !automatic.ready() || starting.value || (!automatic.enabled() && !resumeRequested) || video.hasAttribute('data-autoplay-done')) return
    video.setAttribute('data-autoplay-done', '1')
    if (!video.paused) return
    const session = automatic.session.capture()
    const attempt = ++autoGeneration
    const auto = !resumeRequested
    resumeRequested = false
    starting.value = true
    automatic.state('requested')
    void play(auto).then(outcome => {
      if (!session.current() || attempt !== autoGeneration) return
      starting.value = false
      if (outcome === 'interrupted') {
        video.removeAttribute('data-autoplay-done')
        // Readiness events can precede an interrupted play promise. Give the
        // current media one deferred retry, preserving a later pause or cast.
        if (retries++ < 1) automatic.session.later(() => {
          if (attempt === autoGeneration && video.paused && !video.seeking && video.readyState >= 2) tryAutoplay(video)
        }, 50)
      }
      if (video.paused) automatic.state('blocked')
    })
  }

  return { play, cancel, reset, starting, requestResume, tryAutoplay }
}
