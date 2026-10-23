/** Browser autoplay fallback belongs only to a denied automatic play request.
 * A pause, media switch or user click must never silently mute the video. */
export function createPlaybackControl(deps: {
  getVideo: () => HTMLVideoElement | null
  onMuted: () => void
  onUnsupported: () => void
}) {
  let generation = 0

  function cancel(): void { generation++ }

  async function play(auto: boolean): Promise<void> {
    const video = deps.getVideo()
    if (!video) return
    const token = ++generation
    try {
      await video.play()
    } catch (error) {
      if (token !== generation || deps.getVideo() !== video) return
      const name = (error as { name?: string })?.name
      if (name === 'NotSupportedError') {
        deps.onUnsupported()
      } else if (auto && name === 'NotAllowedError') {
        video.muted = true
        deps.onMuted()
        try { await video.play() } catch { /* The browser may still require a gesture. */ }
      }
    }
  }

  return { play, cancel }
}
