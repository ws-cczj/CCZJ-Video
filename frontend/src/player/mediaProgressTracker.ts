interface ProgressTrackerDeps {
  getVideo: () => HTMLVideoElement | null
  progress: (video: HTMLVideoElement) => void
  metadata: (video: HTMLVideoElement) => void
  missing: () => void
}

/** Owns page progress listeners and a bounded search for the mounted player. */
export function createMediaProgressTracker(deps: ProgressTrackerDeps) {
  let disposed = false
  let timer: ReturnType<typeof setTimeout> | null = null
  let controller: AbortController | null = null
  let attempts = 0

  function reset(): void {
    if (timer !== null) clearTimeout(timer)
    timer = null
    controller?.abort()
    controller = null
    attempts = 0
  }

  function attempt(): void {
    timer = null
    if (disposed) return
    const video = deps.getVideo()
    if (!video) {
      if (++attempts >= 20) { deps.missing(); return }
      timer = setTimeout(attempt, 300)
      return
    }
    controller = new AbortController()
    const { signal } = controller
    video.addEventListener('timeupdate', () => deps.progress(video), { signal })
    video.addEventListener('loadedmetadata', () => deps.metadata(video), { signal })
  }

  return {
    bind() { if (disposed) return; reset(); attempt() },
    schedule(delay = 300) { if (disposed) return; reset(); timer = setTimeout(attempt, delay) },
    stop() { disposed = true; reset() },
  }
}
