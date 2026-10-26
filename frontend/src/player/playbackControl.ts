/** Browser autoplay fallback belongs only to a denied automatic play request.
 * A pause, media switch or user click must never silently mute the video. */
export function createPlaybackControl(deps: {
  getVideo: () => HTMLVideoElement | null
  onMuted: () => void
  onUnsupported: () => void
}) {
  let generation = 0

  function cancel(): void { generation++ }

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

  return { play, cancel }
}
