// Keep a bounded set through a normal tab visit, not just the leave animation.
// 750ms discarded Home's visible posters while the adjacent tab was opening,
// so returning immediately showed transparent slots before image load events.
// Hidden-window/unmount release still bypasses this grace period.
const pending = new Map<() => void, number>()
let timer: number | null = null
const MAX_RETAINED_IMAGES = 64
const RETIRE_AFTER_MS = 15000

function scheduleRelease(): void {
  const next = pending.values().next().value
  if (next === undefined || timer !== null) return
  timer = window.setTimeout(() => {
    timer = null
    const now = performance.now()
    for (const [release, deadline] of pending) {
      if (deadline > now) break
      pending.delete(release)
      release()
    }
    scheduleRelease()
  }, Math.max(0, Math.ceil(next - performance.now())))
}

export function deferImageRelease(release: () => void): () => void {
  pending.set(release, performance.now() + RETIRE_AFTER_MS)
  while(pending.size>MAX_RETAINED_IMAGES){
    const oldest=pending.keys().next().value
    if(!oldest)break
    pending.delete(oldest);oldest()
  }
  scheduleRelease()
  return () => {
    pending.delete(release)
    if (!pending.size && timer !== null) { window.clearTimeout(timer); timer = null }
  }
}

if (import.meta.hot) import.meta.hot.dispose(() => {
  if (timer !== null) window.clearTimeout(timer)
  timer = null
  pending.clear()
})
