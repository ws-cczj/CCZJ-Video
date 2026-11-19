/** In-memory handoff for a verified identical episode, independent of history. */
export interface PlaybackCheckpoint {
  position: number
  rate: number
  paused: boolean
  muted: boolean
  volume: number
}
/** One media load owns its callbacks, listeners and delayed work. Replacing it
 * invalidates pending promises before releasing resources. */
export function createPlaybackSession() {
  let generation = 0
  let controller = new AbortController()
  const timers = new Set<ReturnType<typeof setTimeout>>()
  let frame: { video: HTMLVideoElement; id: number } | undefined

  function reset(): number {
    generation++
    if (frame) frame.video.cancelVideoFrameCallback?.(frame.id)
    frame = undefined
    controller.abort()
    controller = new AbortController()
    for (const timer of timers) clearTimeout(timer)
    timers.clear()
    return generation
  }

  function capture() {
    const token = generation
    const signal = controller.signal
    return {
      signal,
      current: () => token === generation && !signal.aborted,
      guard: <Args extends unknown[]>(callback: (...args: Args) => void) =>
        (...args: Args) => { if (token === generation && !signal.aborted) callback(...args) },
    }
  }

  function later(callback: () => void, delay: number): void {
    const session = capture()
    const timer = setTimeout(() => {
      timers.delete(timer)
      if (session.current()) callback()
    }, delay)
    timers.add(timer)
  }

  function presentFrame(video: HTMLVideoElement, callback: (metadata: VideoFrameCallbackMetadata) => void): void {
    if (frame || typeof video.requestVideoFrameCallback !== 'function') return
    const session = capture()
    frame = { video, id: video.requestVideoFrameCallback((_now, metadata) => {
      if (!session.current()) return
      frame = undefined
      callback(metadata)
    }) }
  }

  return { reset, capture, later, presentFrame }
}
