export interface Enhancement {
  init(video: HTMLVideoElement, wrapper?: HTMLElement): Promise<boolean>
  start(): void
  stop(): void
  destroy(): void
}

/** Keeps asynchronous initialization local to its own instance. A media/mode
 * switch destroys the pending instance and rejects every late completion. */
export function createEnhancementPipeline<T extends Enhancement>() {
  let generation = 0
  let active: T | null = null
  let pending: T | null = null

  function dispose(instance: T | null): void {
    if (!instance) return
    try { instance.stop() } finally { instance.destroy() }
  }

  function reset(): void {
    generation++
    const oldActive = active
    const oldPending = pending
    active = pending = null
    dispose(oldActive)
    if (oldPending !== oldActive) dispose(oldPending)
  }

  async function start(factory: () => Promise<T | null>, video: HTMLVideoElement, wrapper?: HTMLElement): Promise<T | null> {
    reset()
    const token = generation
    const instance = await factory()
    if (!instance) return null
    if (token !== generation) { dispose(instance); return null }
    pending = instance
    let ready = false
    try {
      ready = await instance.init(video, wrapper)
    } catch (error) {
      if (pending === instance) pending = null
      dispose(instance)
      if (token !== generation) return null
      throw error
    }
    if (pending === instance) pending = null
    // Destroy again after a stale init: an implementation may allocate resources
    // after reset() destroyed its earlier partial state.
    if (token !== generation || !ready) { dispose(instance); return null }
    active = instance
    instance.start()
    return instance
  }

  return { start, reset, current: () => active }
}
