// Keep a bounded set through a normal tab visit, not just the leave animation.
// 750ms discarded Home's visible posters while the adjacent tab was opening,
// so returning immediately showed transparent slots before image load events.
// Hidden-window/unmount release still bypasses this grace period.
const pending = new Set<() => void>()
let timer: number | null = null
const MAX_RETAINED_IMAGES = 64
const RETIRE_AFTER_MS = 15000

export function deferImageRelease(release: () => void): () => void {
  pending.add(release)
  while(pending.size>MAX_RETAINED_IMAGES){
    const oldest=pending.values().next().value
    if(!oldest)break
    pending.delete(oldest);oldest()
  }
  if (timer === null) timer = window.setTimeout(() => {
    timer = null
    const callbacks = [...pending]
    pending.clear()
    callbacks.forEach(callback => callback())
  }, RETIRE_AFTER_MS)
  return () => {
    pending.delete(release)
    if (!pending.size && timer !== null) { window.clearTimeout(timer); timer = null }
  }
}
