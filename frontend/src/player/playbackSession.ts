/** One media load owns its callbacks, listeners and delayed work. Replacing it
 * invalidates pending promises before releasing resources. */
export function createPlaybackSession() {
  let generation = 0
  let controller = new AbortController()
  const timers = new Set<ReturnType<typeof setTimeout>>()

  function reset(): number {
    generation++
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

  return { reset, capture, later }
}
